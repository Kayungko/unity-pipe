package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// scriptedTransport replays job snapshots in order (GET /api/job).
type scriptedTransport struct {
	steps []protocol.Response
	calls int
}

func (s *scriptedTransport) Do(req protocol.Request) (protocol.Response, error) {
	i := s.calls
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	s.calls++
	return s.steps[i], nil
}
func (s *scriptedTransport) Close() error { return nil }

func newJobRuntime(st *scriptedTransport, timeout time.Duration) *Runtime {
	return &Runtime{
		Opts: &options.Options{Timeout: timeout},
		Client: &protocol.PipelineClient{
			Transport: st,
			Source: func() (*protocol.InstanceDescriptor, error) {
				return &protocol.InstanceDescriptor{Port: 7802, EvalToken: "t"}, nil
			},
			Sleep: protocol.NopSleep,
		},
	}
}

func jobBody(state string) protocol.Response {
	return protocol.Response{StatusCode: 200, Body: []byte(`{"jobId":"j1","state":"` + state + `"}`)}
}

func TestJobWaitsUntilTerminal(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{jobBody("running"), jobBody("completed")}}
	got, err := JobCommand().Run(newJobRuntime(st, 0), []string{"j1"})
	if err != nil {
		t.Fatalf("job: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(mustMarshal(got), &body); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if body["state"] != "completed" {
		t.Fatalf("state = %v, want completed", body["state"])
	}
}

func TestJobNoWaitReturnsSnapshot(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{jobBody("running")}}
	got, err := JobCommand().Run(newJobRuntime(st, 0), []string{"--no-wait", "j1"})
	if err != nil {
		t.Fatalf("job --no-wait: %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("polls = %d, want a single snapshot", st.calls)
	}
	var body map[string]any
	_ = json.Unmarshal(mustMarshal(got), &body)
	if body["state"] != "running" {
		t.Fatalf("state = %v, want running", body["state"])
	}
}

func TestJobSynopsisDocumentsTimeout(t *testing.T) {
	// The 10min default poll budget silently truncates long builds/bakes;
	// the synopsis must tell users to raise it with --timeout.
	syn := JobCommand().Synopsis
	if !strings.Contains(syn, "--timeout") || !strings.Contains(syn, "10m") {
		t.Fatalf("job synopsis must document --timeout and the 10m default: %q", syn)
	}
}
