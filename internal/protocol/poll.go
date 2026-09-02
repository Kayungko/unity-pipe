package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// PollConfig drives the shared polling state machine used by recompile, test,
// audit and job waits.
type PollConfig struct {
	TerminalOK     []string      // statuses that end the wait successfully
	TerminalFail   []string      // statuses that end the wait as a failure (exit 20)
	TerminalCancel []string      // statuses that end as a cancelled job (exit 22)
	Interval       time.Duration // between polls (default 500ms)
	Deadline       time.Duration // overall budget (default 10min)
	MaxAttempts    int           // hard cap even under the deadline (0 = unlimited)
}

// PollResult is the terminal observation.
type PollResult struct {
	Status   string
	Body     json.RawMessage
	Attempts int
}

// PollStep is one observation: the current status plus the raw body.
type PollStep struct {
	Status string
	Body   json.RawMessage
}

// defaultDeadline mirrors options.DefaultTimeout.
const defaultPollDeadline = 10 * time.Minute

// Poll runs fn until it reports a terminal status, the deadline expires or a
// hard error (non-retryable, e.g. auth) surfaces. Connectivity hiccups are
// retried — polling typically spans domain reloads, which drop connections.
func Poll(fn func() (PollStep, error), cfg PollConfig, sleep SleepFunc, now func() time.Time) (*PollResult, error) {
	if cfg.Interval <= 0 {
		cfg.Interval = 500 * time.Millisecond
	}
	if cfg.Deadline <= 0 {
		cfg.Deadline = defaultPollDeadline
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if now == nil {
		now = time.Now
	}
	start := now()
	attempts := 0
	for {
		attempts++
		step, err := fn()
		if err != nil {
			ue := AsUpipeError(err)
			if !reloadRetryable(ue.Code) {
				return nil, ue
			}
			// Connectivity blip (likely a domain reload) — keep polling.
		} else {
			for _, s := range cfg.TerminalOK {
				if step.Status == s {
					return &PollResult{Status: step.Status, Body: step.Body, Attempts: attempts}, nil
				}
			}
			for _, s := range cfg.TerminalFail {
				if step.Status == s {
					return nil, &UpipeError{
						Code:    ExitCommandFailed,
						Kind:    "CommandFailed",
						Message: fmt.Sprintf("terminal status %q", step.Status),
						Data:    json.RawMessage(step.Body),
					}
				}
			}
			for _, s := range cfg.TerminalCancel {
				if step.Status == s {
					return nil, ErrJobCancelled(fmt.Sprintf("job cancelled (status %q)", step.Status))
				}
			}
		}
		if cfg.MaxAttempts > 0 && attempts >= cfg.MaxAttempts {
			return nil, ErrTimeout(fmt.Sprintf("gave up after %d polls (status %q)", attempts, step.Status))
		}
		if now().Sub(start) >= cfg.Deadline {
			return nil, ErrTimeout(fmt.Sprintf("deadline %s exceeded (last status %q)", cfg.Deadline, step.Status))
		}
		sleep(cfg.Interval)
	}
}

// ExtractStatus pulls a "status" string out of an arbitrary JSON body.
func ExtractStatus(body json.RawMessage) string {
	body = UnwrapJSONString(body)
	var probe struct {
		Status string `json:"status"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	if probe.Status != "" {
		return probe.Status
	}
	return probe.State
}

// UnwrapJSONString decodes one level of double-encoded JSON: several pipeline
// commands (e.g. recompile_status) return a raw JSON string, which the server
// then serializes as a JSON string literal — `"{\"status\":…}"`. This makes
// the inner document addressable.
func UnwrapJSONString(body json.RawMessage) json.RawMessage {
	var s string
	if err := json.Unmarshal(body, &s); err != nil {
		return body
	}
	trimmed := json.RawMessage(s)
	// Only treat it as double-encoded if the inner text is itself JSON.
	var probe any
	if err := json.Unmarshal(trimmed, &probe); err == nil {
		return trimmed
	}
	return body
}
