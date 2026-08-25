package protocol

import (
	"encoding/json"
	"fmt"
)

// Exit codes are a stable contract for agents: branch on the code, never
// parse stderr text.
const (
	ExitOK             = 0  // success
	ExitUsage          = 2  // command-line usage error (argparse convention)
	ExitNoInstance     = 10 // no running editor instance discovered
	ExitConnectionLost = 11 // unreachable after the domain-reload retry ladder
	ExitAuth           = 12 // 401/403 — token invalid, editor restart likely
	ExitBusy           = 13 // 503 settling / 429 job queue full (retryable)
	ExitCommandFailed  = 20 // server reported ok=false (incl. failing tests)
	ExitTimeout        = 21 // client-side or server-side timeout
	ExitJobCancelled   = 22 // job was cancelled before completion
	ExitProtocol       = 30 // endpoint missing/unknown — package version mismatch
	ExitInternal       = 50 // unexpected internal error
)

// UpipeError is the single error type crossing command boundaries. It carries
// the exit code, a short machine-readable kind, and optional structured data
// (e.g. the compile error list of a failed recompile).
type UpipeError struct {
	Code      int    `json:"code"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Data      any    `json:"data,omitempty"`
}

func (e *UpipeError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func newErr(code int, kind, msg string) *UpipeError {
	return &UpipeError{Code: code, Kind: kind, Message: msg}
}

// ErrNoInstance reports that no live pipeline instance was found.
func ErrNoInstance(msg string) *UpipeError {
	return newErr(ExitNoInstance, "NoInstance", msg)
}

// ErrConnectionLost reports the retry ladder was exhausted.
func ErrConnectionLost(msg string) *UpipeError {
	return &UpipeError{Code: ExitConnectionLost, Kind: "ConnectionLost", Message: msg, Retryable: true}
}

// ErrAuth reports an authentication failure against the pipeline server.
func ErrAuth(msg string) *UpipeError {
	return newErr(ExitAuth, "Auth", msg)
}

// ErrBusy reports a retryable busy/settling state.
func ErrBusy(msg string, data any) *UpipeError {
	return &UpipeError{Code: ExitBusy, Kind: "Busy", Message: msg, Retryable: true, Data: data}
}

// ErrCommandFailed reports a command that executed but failed server-side.
func ErrCommandFailed(resp *ExecResponse) *UpipeError {
	e := &UpipeError{
		Code:    ExitCommandFailed,
		Kind:    "CommandFailed",
		Message: resp.ErrorMessage(),
	}
	if len(resp.Result) > 0 {
		e.Data = json.RawMessage(resp.Result)
	}
	if resp.Retryable != nil && *resp.Retryable {
		e.Retryable = true
	}
	return e
}

// ErrTimeout reports a timeout.
func ErrTimeout(msg string) *UpipeError {
	return &UpipeError{Code: ExitTimeout, Kind: "Timeout", Message: msg, Retryable: true}
}

// ErrJobCancelled reports a cancelled job.
func ErrJobCancelled(msg string) *UpipeError {
	return newErr(ExitJobCancelled, "JobCancelled", msg)
}

// ErrProtocol reports an endpoint/protocol mismatch — usually a package
// version too old or too new for this client.
func ErrProtocol(msg string) *UpipeError {
	return newErr(ExitProtocol, "Protocol", msg)
}

// AsUpipeError extracts an *UpipeError or wraps an arbitrary error as
// ExitInternal.
func AsUpipeError(err error) *UpipeError {
	if err == nil {
		return nil
	}
	if ue, ok := err.(*UpipeError); ok {
		return ue
	}
	return &UpipeError{Code: ExitInternal, Kind: "Internal", Message: err.Error()}
}
