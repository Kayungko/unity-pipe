// Package protocol contains the pure-logic model of the Unity Pipeline HTTP
// protocol: data types, endpoint wrappers, discovery, retry and polling
// policies. It performs no I/O of its own — everything that touches the
// network goes through the Transport interface, everything that touches the
// filesystem is injected by callers. This is the deliberate translation
// boundary should the client ever be re-implemented in another language.
package protocol

import (
	"encoding/json"
	"time"
)

// InstanceDescriptor mirrors the port file written by the editor at
// <project>/Library/Pipeline/.unity-pipeline-port
// (Runtime/Models/InstanceDescriptor.cs). The file is rewritten on every
// heartbeat and after every domain reload — always re-read before use.
type InstanceDescriptor struct {
	Pid           int       `json:"pid"`
	Port          int       `json:"port"`
	ProjectPath   string    `json:"projectPath"`
	ProjectName   string    `json:"projectName"`
	UnityVersion  string    `json:"unityVersion"`
	Mode          string    `json:"mode"` // "editor" | "batchmode"
	StartedAt     time.Time `json:"startedAt"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	EvalToken     string    `json:"evalToken"`
}

// CommandSpec is one entry of GET /api/commands?detail=compact.
type CommandSpec struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Package     string   `json:"package"`
}

// CommandsResponse is the envelope of GET /api/commands (flat mode).
type CommandsResponse struct {
	Commands []CommandSpec `json:"commands"`
	Total    int           `json:"total"`
	Count    int           `json:"count"`
}

// ExecResponse is the envelope of POST /api/exec, GET /api/job and the
// editor_status endpoint (BaseResponse + CommandExecutionResponse).
// Unknown fields are tolerated so newer server versions keep working.
type ExecResponse struct {
	Success         bool            `json:"success"`
	Command         string          `json:"command"`
	Result          json.RawMessage `json:"result"`
	Message         string          `json:"message"`
	Error           string          `json:"error"`
	ErrorDetails    string          `json:"errorDetails"`
	ExecutedAt      time.Time       `json:"executedAt"`
	ExecutionTimeMs *int64          `json:"executionTimeMs"`
	Status          string          `json:"status,omitempty"`
	Retryable       *bool           `json:"retryable,omitempty"`
}

// ErrorMessage returns the most specific human-readable error text.
func (r *ExecResponse) ErrorMessage() string {
	if r.ErrorDetails != "" {
		return r.ErrorDetails
	}
	if r.Error != "" {
		return r.Error
	}
	if r.Message != "" {
		return r.Message
	}
	return "command failed"
}

// CommandError interprets the outcome of a finished command. Some 0.6
// commands report failure inside a successful dispatch envelope.
func (r *ExecResponse) CommandError(command string) error {
	if !r.Success {
		return ErrCommandFailed(r)
	}
	var message string
	switch command {
	case "run_script", "run_tests":
		var result struct {
			Success *bool  `json:"success"`
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(r.Result, &result) != nil || result.Success == nil || *result.Success {
			return nil
		}
		message = (&ExecResponse{Error: result.Error, Message: result.Message}).ErrorMessage()
	case "wait":
		var result struct {
			Status    string `json:"status"`
			Condition string `json:"condition"`
		}
		if json.Unmarshal(r.Result, &result) != nil {
			return nil
		}
		switch result.Status {
		case "transitioning":
			return ErrBusy("Editor is changing Play Mode", r.Result)
		case "timed_out":
			failure := ErrTimeout("timed out waiting for " + result.Condition)
			failure.Data = r.Result
			return failure
		}
		return nil
	case "batch":
		var result struct {
			Reverted bool   `json:"reverted"`
			Error    string `json:"error"`
			Results  []struct {
				Error string `json:"error"`
			} `json:"results"`
		}
		if json.Unmarshal(r.Result, &result) != nil || !result.Reverted {
			return nil
		}
		message = result.Error
		if message == "" {
			message = "transactional batch rolled back"
			for _, operation := range result.Results {
				if operation.Error != "" {
					message += ": " + operation.Error
					break
				}
			}
		}
	default:
		return nil
	}
	return ErrCommandFailed(&ExecResponse{Error: message, Result: r.Result})
}

// JobResponse is the shape of GET /api/job?id=… (job state envelope).
type JobResponse struct {
	JobId    string          `json:"jobId"`
	Command  string          `json:"command,omitempty"`
	State    string          `json:"state"` // queued | running | completed | failed | canceled (US; CLI also accepts cancelled)
	Progress json.RawMessage `json:"progress"`
	Result   json.RawMessage `json:"result"` // detached jobs retain the raw command result
	Error    string          `json:"error"`
}
