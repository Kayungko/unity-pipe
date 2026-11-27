package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DescriptorSource re-reads the port file on every call. It must never cache:
// a domain reload rewrites port and eval token.
type DescriptorSource func() (*InstanceDescriptor, error)

// PipelineClient wraps the pipeline HTTP endpoints. It is transport-agnostic;
// all requests carry a freshly fetched descriptor so a mid-session domain
// reload is followed transparently.
type PipelineClient struct {
	Transport Transport
	Source    DescriptorSource
	Sleep     SleepFunc // injectable; defaults to time.Sleep
}

func (c *PipelineClient) sleep(d time.Duration) {
	if c.Sleep == nil {
		time.Sleep(d)
		return
	}
	c.Sleep(d)
}

// do performs one round-trip and maps transport/HTTP failures onto UpipeError.
// Transport-level failures come back as ExitConnectionLost (retryable) so
// callers can distinguish connectivity from semantics.
func (c *PipelineClient) do(method, path string, body []byte, timeout time.Duration) (Response, error) {
	return c.exchange(Request{Method: method, Path: path, Body: body, Timeout: timeout}, "")
}

func (c *PipelineClient) exchange(req Request, operation string) (Response, error) {
	d, err := c.Source()
	if err != nil {
		return Response{}, ErrNoInstance(err.Error())
	}
	req.HostPort = d.HostPort()
	req.AuthToken = d.EvalToken
	resp, err := c.Transport.Do(req)
	if err != nil {
		var notSent *RequestNotSent
		if req.ReportTimeout && errors.Is(err, context.DeadlineExceeded) {
			timeout := ErrTimeout("binary command request deadline exceeded")
			timeout.Retryable = false
			if operation != "" && !errors.As(err, &notSent) {
				unknown := unknownOutcome(ExitConnectionLost, operation, "request deadline exceeded")
				timeout.Message, timeout.Data = unknown.Message, unknown.Data
			}
			return Response{}, timeout
		}
		if operation != "" && !errors.As(err, &notSent) {
			return Response{}, unknownOutcome(ExitConnectionLost, operation, err.Error())
		}
		return Response{}, ErrConnectionLost(err.Error())
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp, ErrAuth("pipeline server rejected the token (editor restart refreshes it)")
	case resp.StatusCode == http.StatusNotFound:
		return resp, ErrProtocol(fmt.Sprintf("endpoint %s not found — package/server version mismatch", req.Path))
	case resp.StatusCode >= 400:
		// Best-effort decode of the server envelope for a richer message.
		var env ExecResponse
		_ = json.Unmarshal(resp.Body, &env)
		if env.ErrorCode == ReportedCommandFailure {
			return resp, ErrCommandFailed(&env)
		}
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			msg := firstNonEmpty(env.ErrorDetails, env.Message, "server busy (settling or job queue full)")
			var data any // server envelope body, when present
			if len(resp.Body) > 0 {
				data = json.RawMessage(resp.Body)
			}
			return resp, ErrBusy(msg, data)
		}
		msg := firstNonEmpty(env.ErrorDetails, env.Error, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, req.Path))
		return resp, &UpipeError{Code: ExitCommandFailed, Kind: "CommandFailed", Message: msg,
			Data: json.RawMessage(resp.Body)}
	}
	return resp, nil
}

func replayableCommand(command string, detached bool) bool {
	if detached {
		return false
	}
	// These built-ins have fixed, read-only behavior on supported package
	// versions. Unknown commands and eval may write even if their names suggest
	// otherwise. A detached request always creates server state.
	switch command {
	case "editor_status", "get_console_logs", "list_open_scenes", "wait",
		"audit_status", "build_status", "memory_snapshot_status", "package_status",
		"player_memory_snapshot_status", "profiler_record_status", "recompile_status",
		"switch_build_target_status", "test_status", "wait_status":
		return true
	default:
		return false
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// getJSON performs a GET and decodes the JSON body into out.
func (c *PipelineClient) getJSON(path string, out any, timeout time.Duration) error {
	resp, err := c.do(http.MethodGet, path, nil, timeout)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return ErrProtocol(fmt.Sprintf("unparseable response from %s: %v", path, err))
	}
	return nil
}

// Status returns GET /api/status as a raw JSON object (shape varies by
// package version; kept generic on purpose).
func (c *PipelineClient) Status() (json.RawMessage, error) {
	resp, err := c.do(http.MethodGet, "/api/status", nil, 0)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// EditorStatus returns GET /api/editor_status (command envelope of editor_status).
func (c *PipelineClient) EditorStatus() (*ExecResponse, error) {
	var r ExecResponse
	if err := c.getJSON("/api/editor_status", &r, 0); err != nil {
		return nil, err
	}
	return &r, nil
}

// Dialog returns the off-main-thread modal dialog snapshot.
func (c *PipelineClient) Dialog(timeout time.Duration) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.getJSON("/api/dialog", &raw, timeout); err != nil {
		return nil, err
	}
	return raw, nil
}

// ListCommands returns the compact command index from GET /api/commands.
func (c *PipelineClient) ListCommands() (*CommandsResponse, error) {
	var r CommandsResponse
	if err := c.getJSON("/api/commands?detail=compact", &r, 0); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListCommandsFull returns the full command index including parameter schemas.
func (c *PipelineClient) ListCommandsFull() (json.RawMessage, error) {
	resp, err := c.do(http.MethodGet, "/api/commands", nil, 0)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// CommandCatalog returns the server's grouped or tag-filtered command index.
func (c *PipelineClient) CommandCatalog(groupBy, tag, query string, full bool) (json.RawMessage, error) {
	detail := "compact"
	if full {
		detail = "full"
	}
	args := url.Values{"detail": {detail}, "group_by": {groupBy}}
	if tag != "" {
		args.Set("tag", tag)
	}
	if query != "" {
		args.Set("query", query)
	}
	var raw json.RawMessage
	if err := c.getJSON("/api/commands?"+args.Encode(), &raw, 0); err != nil {
		return nil, err
	}
	var body struct {
		Commands []json.RawMessage `json:"commands"`
		Groups   []json.RawMessage `json:"groups"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || (groupBy == "flat" && body.Commands == nil) ||
		(groupBy != "flat" && body.Groups == nil) {
		return nil, ErrProtocol("server did not return the requested command catalog")
	}
	return raw, nil
}

// DefaultRoundTripTimeout is the client-side round-trip ceiling for a single
// /api/exec call when no explicit timeout was given. It mirrors
// options.DefaultTimeout (10 minutes); it is restated here because the
// protocol package must not depend on the CLI options package. Without it the
// transport default (120s) would silently clip long evals.
const DefaultRoundTripTimeout = 10 * time.Minute

// ExecOptions tunes one POST /api/exec call.
type ExecOptions struct {
	Parameters map[string]any
	Timeout    time.Duration // server-side command timeout hint (sent as ms); 0 = DefaultRoundTripTimeout (10m)
	Job        bool          // submit as a detached job, returns a jobId handle
	Stdin      *[]byte       // nil omits stdin; an empty slice is an explicit input
	BinaryIO   bool          // use the exact transport deadline for binary exec
}

// execRequestBody is the wire format of POST /api/exec.
type execRequestBody struct {
	Command    string         `json:"command"`
	Parameters map[string]any `json:"parameters"`
	Timeout    *int64         `json:"timeout,omitempty"`
	Job        bool           `json:"job,omitempty"`
	Stdin      *[]byte        `json:"stdinBase64,omitempty"`
	Verbose    bool           `json:"verbose,omitempty"`
}

// Exec runs a command and returns its envelope. A busy/settling server
// returns ErrBusy (retryable); a failed command returns ErrCommandFailed.
func (c *PipelineClient) Exec(command string, opts ExecOptions) (*ExecResponse, error) {
	body := execRequestBody{
		Command:    command,
		Parameters: opts.Parameters,
		Job:        opts.Job,
		Stdin:      opts.Stdin,
		// Official 0.6 lean envelope (verbose default false) drops command /
		// executedAt / executionTimeMs. Keep the full envelope until upipe
		// consumers stop reading those fields.
		Verbose: true,
	}
	// Always send a body timeout. The server default is 60s
	// (CommandExecutionRequest.Timeout ?? 60000); omitting the field used to
	// let a 10-minute client round-trip wait on a command the dispatcher had
	// already killed. Unset → DefaultRoundTripTimeout (10m).
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultRoundTripTimeout
	}
	ms := timeout.Milliseconds()
	if opts.BinaryIO {
		ms = max(ms, 1)
	}
	body.Timeout = &ms
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, &UpipeError{Code: ExitInternal, Kind: "Internal", Message: err.Error()}
	}
	// The user-facing --timeout drives BOTH the server-side command timeout
	// (request body, in milliseconds — the server consumes the field as
	// milliseconds; see CommandExecutionRequest.Timeout) and the client
	// round-trip deadline — otherwise a slow eval is clipped at the
	// transport default (120s) and misreported as a connection error.
	// A small margin is added so the server can deliver its own timeout
	// envelope before the client gives up.
	roundTrip := timeout + 30*time.Second
	if opts.BinaryIO {
		roundTrip = timeout
	} else if command == "wait" && !opts.Job {
		// A domain reload can orphan a pending response. Retry this read-only
		// request promptly once its condition deadline has elapsed.
		budget := 10 * time.Second
		if ms, err := strconv.ParseInt(fmt.Sprint(opts.Parameters["timeout_ms"]), 10, 64); err == nil && ms >= 1 && ms <= 600000 {
			budget = time.Duration(ms) * time.Millisecond
		}
		roundTrip = min(timeout, budget+2*time.Second)
	}
	operation := command
	if !opts.BinaryIO && replayableCommand(command, opts.Job) {
		operation = ""
	}
	resp, err := c.exchange(Request{
		Method: http.MethodPost, Path: "/api/exec", Body: raw, Timeout: roundTrip, ReportTimeout: opts.BinaryIO,
	}, operation)
	if err != nil {
		return nil, err
	}
	var r ExecResponse
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		if operation != "" {
			return nil, unknownOutcome(ExitProtocol, operation, "unparseable exec response")
		}
		return nil, ErrProtocol(fmt.Sprintf("unparseable exec response: %v", err))
	}
	return &r, r.CommandError(command)
}

// GetJob polls GET /api/job?id=….
func (c *PipelineClient) GetJob(jobID string) (*JobResponse, error) {
	return c.getJob(url.Values{"id": {jobID}}, 0)
}

// GetJobWithLogs requests a job snapshot and logs newer than the cursor.
func (c *PipelineClient) GetJobWithLogs(jobID string, after int64, timeout time.Duration) (*JobResponse, error) {
	return c.getJob(url.Values{"id": {jobID}, "log_after": {strconv.FormatInt(after, 10)}}, timeout)
}

func (c *PipelineClient) getJob(query url.Values, timeout time.Duration) (*JobResponse, error) {
	var r JobResponse
	if err := c.getJSON("/api/job?"+query.Encode(), &r, timeout); err != nil {
		return nil, err
	}
	return &r, nil
}

// CancelJob requests cancellation via POST /api/job/cancel.
// The server (HandleJobCancelRequest) reads JSON field "id"; "jobId" is
// echoed for older notes / mis-copied clients and is ignored.
func (c *PipelineClient) CancelJob(jobID string) (*JobResponse, error) {
	raw, _ := json.Marshal(map[string]string{"id": jobID, "jobId": jobID})
	resp, err := c.exchange(Request{Method: http.MethodPost, Path: "/api/job/cancel", Body: raw}, "job-cancel")
	if err != nil {
		return nil, err
	}
	var r JobResponse
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return nil, ErrProtocol(fmt.Sprintf("unparseable job-cancel response: %v", err))
	}
	return &r, nil
}

// Progress returns GET /api/progress ({"active": bool, "progress": …}).
func (c *PipelineClient) Progress() (json.RawMessage, error) {
	resp, err := c.do(http.MethodGet, "/api/progress", nil, 0)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// TestStatus returns GET /api/test-status (async test runner state).
func (c *PipelineClient) TestStatus() (json.RawMessage, error) {
	resp, err := c.do(http.MethodGet, "/api/test-status", nil, 0)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Body), nil
}

// reloadRetryable reports which error codes are worth retrying through a
// domain reload: connectivity loss (port/token rewrite window) and busy
// settling right after editor startup.
func reloadRetryable(err *UpipeError) bool {
	if err.Code == ExitNoInstance {
		return true
	}
	return err.Retryable && (err.Code == ExitConnectionLost || err.Code == ExitBusy)
}

// ExecReloadTolerant wraps Exec with the domain-reload retry ladder: on a
// connection error (editor mid-reload, port/token being rewritten) or a busy
// settling state it waits per the ladder and retries, re-reading the
// descriptor every attempt. Semantic errors (auth, command failure) are
// returned immediately.
func (c *PipelineClient) ExecReloadTolerant(command string, opts ExecOptions, maxAttempts int) (*ExecResponse, error) {
	if maxAttempts <= 0 {
		maxAttempts = len(ReloadLadder)
	}
	var lastErr *UpipeError
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			c.sleep(Backoff(attempt - 1))
		}
		r, err := c.Exec(command, opts)
		if err == nil {
			return r, nil
		}
		ue := AsUpipeError(err)
		if !reloadRetryable(ue) {
			return nil, ue
		}
		if ue.Code == ExitBusy && blockedByDialog(ue.Data) {
			return nil, ue
		}
		lastErr = ue
	}
	if lastErr == nil {
		lastErr = ErrConnectionLost("retry ladder exhausted")
	}
	// Propagate the last error's class: a server that stayed busy through the
	// whole ladder must surface ErrBusy (exit 13, retryable later), not be
	// misreported as a connectivity loss (exit 11).
	if lastErr.Code == ExitBusy {
		return nil, ErrBusy(fmt.Sprintf("editor still busy after %d attempts: %v", maxAttempts, lastErr), lastErr.Data)
	}
	return nil, ErrConnectionLost(fmt.Sprintf("editor unreachable after %d attempts: %v", maxAttempts, lastErr))
}

func blockedByDialog(data any) bool {
	raw, ok := data.(json.RawMessage)
	if !ok {
		return false
	}
	var response struct {
		BusyReason string `json:"busyReason"`
	}
	return json.Unmarshal(raw, &response) == nil && response.BusyReason == "blocked_by_dialog"
}
