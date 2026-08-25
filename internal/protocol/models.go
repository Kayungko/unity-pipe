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

// JobResponse is the shape of GET /api/job?id=… (job state envelope).
type JobResponse struct {
	JobId    string          `json:"jobId"`
	State    string          `json:"state"` // queued | running | completed | failed | cancelled
	Progress json.RawMessage `json:"progress"`
	Result   *ExecResponse   `json:"result"`
	Error    string          `json:"error"`
}
