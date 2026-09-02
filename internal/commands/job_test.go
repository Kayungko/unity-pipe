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
	last  protocol.Request
}

func (s *scriptedTransport) Do(req protocol.Request) (protocol.Response, error) {
	s.last = req
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

func TestJobCanceledAmericanSpellingExits22(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{jobBody("running"), jobBody("canceled")}}
	_, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"j1"})
	ue := protocol.AsUpipeError(err)
	if ue.Code != protocol.ExitJobCancelled {
		t.Fatalf("canceled → exit %d (%s), want 22 JobCancelled", ue.Code, ue.Kind)
	}
	if st.calls > 5 {
		t.Fatalf("polls = %d, canceled should terminate quickly (not timeout)", st.calls)
	}
}

func TestJobCancelledBritishSpellingAlsoExits22(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{jobBody("cancelled")}}
	_, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"j1"})
	if protocol.AsUpipeError(err).Code != protocol.ExitJobCancelled {
		t.Fatalf("cancelled (British) → %v, want ExitJobCancelled", err)
	}
}

func TestJobFailedStillExits20(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{jobBody("failed")}}
	_, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"j1"})
	ue := protocol.AsUpipeError(err)
	if ue.Code != protocol.ExitCommandFailed {
		t.Fatalf("failed → exit %d, want 20 CommandFailed", ue.Code)
	}
}

func TestJobCancelSendsIdNotJobId(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{
		{StatusCode: 200, Body: []byte(`{"jobId":"j1","state":"canceled"}`)},
	}}
	if _, err := JobCancelCommand().Run(newJobRuntime(st, 0), []string{"j1"}); err != nil {
		t.Fatalf("job-cancel: %v", err)
	}
	if st.last.Method != "POST" || st.last.Path != "/api/job/cancel" {
		t.Fatalf("got %s %s, want POST /api/job/cancel", st.last.Method, st.last.Path)
	}
	var body map[string]string
	if err := json.Unmarshal(st.last.Body, &body); err != nil {
		t.Fatalf("body %s: %v", st.last.Body, err)
	}
	if body["id"] != "j1" {
		t.Fatalf("id = %q, want j1 (server reads parsed[\"id\"])", body["id"])
	}
}
