package protocol

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	d, err := c.Source()
	if err != nil {
		return Response{}, ErrNoInstance(err.Error())
	}
	req := Request{
		Method:    method,
		Path:      path,
		Body:      body,
		HostPort:  d.HostPort(),
		AuthToken: d.EvalToken,
		Timeout:   timeout,
	}
	resp, err := c.Transport.Do(req)
	if err != nil {
		return Response{}, ErrConnectionLost(err.Error())
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp, ErrAuth("pipeline server rejected the token (editor restart refreshes it)")
	case resp.StatusCode == http.StatusNotFound:
		return resp, ErrProtocol(fmt.Sprintf("endpoint %s not found — package/server version mismatch", path))
	case resp.StatusCode >= 400:
		// Best-effort decode of the server envelope for a richer message.
		var env struct {
			Error        string `json:"error"`
			ErrorDetails string `json:"errorDetails"`
			Message      string `json:"message"`
		}
		_ = json.Unmarshal(resp.Body, &env)
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
			msg := firstNonEmpty(env.ErrorDetails, env.Message, "server busy (settling or job queue full)")
			return resp, ErrBusy(msg, nil)
		}
		msg := firstNonEmpty(env.ErrorDetails, env.Error, fmt.Sprintf("HTTP %d from %s", resp.StatusCode, path))
		return resp, &UpipeError{Code: ExitCommandFailed, Kind: "CommandFailed", Message: msg,
			Data: json.RawMessage(resp.Body)}
	}
	return resp, nil
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

// ExecOptions tunes one POST /api/exec call.
type ExecOptions struct {
	Parameters map[string]any
	Timeout    time.Duration // server-side command timeout hint; 0 = omit
	Job        bool          // submit as a detached job, returns a jobId handle
}

// execRequestBody is the wire format of POST /api/exec.
type execRequestBody struct {
	Command    string         `json:"command"`
	Parameters map[string]any `json:"parameters"`
	Timeout    *int64         `json:"timeout,omitempty"`
	Job        bool           `json:"job,omitempty"`
}

// Exec runs a command and returns its envelope. A busy/settling server
// returns ErrBusy (retryable); a failed command returns ErrCommandFailed.
func (c *PipelineClient) Exec(command string, opts ExecOptions) (*ExecResponse, error) {
	body := execRequestBody{
		Command:    command,
		Parameters: opts.Parameters,
		Job:        opts.Job,
	}
	if opts.Timeout > 0 {
		secs := int64(opts.Timeout.Seconds())
		body.Timeout = &secs
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, &UpipeError{Code: ExitInternal, Kind: "Internal", Message: err.Error()}
	}
	resp, err := c.do(http.MethodPost, "/api/exec", raw, 0)
	if err != nil {
		return nil, err
	}
	var r ExecResponse
	if err := json.Unmarshal(resp.Body, &r); err != nil {
		return nil, ErrProtocol(fmt.Sprintf("unparseable exec response: %v", err))
	}
	if !r.Success {
		return &r, ErrCommandFailed(&r)
	}
	return &r, nil
}

// GetJob polls GET /api/job?id=….
func (c *PipelineClient) GetJob(jobID string) (*JobResponse, error) {
	var r JobResponse
	if err := c.getJSON("/api/job?id="+url.QueryEscape(jobID), &r, 0); err != nil {
		return nil, err
	}
	return &r, nil
}

// CancelJob requests cancellation via POST /api/job/cancel.
func (c *PipelineClient) CancelJob(jobID string) (*ExecResponse, error) {
	raw, _ := json.Marshal(map[string]string{"jobId": jobID})
	resp, err := c.do(http.MethodPost, "/api/job/cancel", raw, 0)
	if err != nil {
		return nil, err
	}
	var r ExecResponse
	_ = json.Unmarshal(resp.Body, &r)
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
func reloadRetryable(code int) bool {
	return code == ExitNoInstance || code == ExitConnectionLost || code == ExitBusy
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
		if !reloadRetryable(ue.Code) {
			return nil, ue
		}
		lastErr = ue
	}
	if lastErr == nil {
		lastErr = ErrConnectionLost("retry ladder exhausted")
	}
	return nil, ErrConnectionLost(fmt.Sprintf("editor unreachable after %d attempts: %v", maxAttempts, lastErr))
}
