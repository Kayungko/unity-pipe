package commands

import (
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// scriptedExecTransport replays /api/exec responses in order, regardless of
// which command was requested — enough to drive pollCommand/RecompileCommand
// through trigger -> N status polls -> terminal.
type scriptedExecTransport struct {
	steps []protocol.Response
	calls int
}

func (s *scriptedExecTransport) Do(req protocol.Request) (protocol.Response, error) {
	i := s.calls
	if i >= len(s.steps) {
		i = len(s.steps) - 1
	}
	s.calls++
	return s.steps[i], nil
}
func (s *scriptedExecTransport) Close() error { return nil }

func execResp(body string) protocol.Response {
	return protocol.Response{StatusCode: 200, Body: []byte(body)}
}

func newRecompileRuntime(st *scriptedExecTransport) *Runtime {
	return &Runtime{
		Opts: &options.Options{Timeout: time.Second},
		Client: &protocol.PipelineClient{
			Transport: st,
			Source: func() (*protocol.InstanceDescriptor, error) {
				return &protocol.InstanceDescriptor{Port: 7802, EvalToken: "t"}, nil
			},
			Sleep: protocol.NopSleep,
		},
	}
}

// TestRecompileWaitsForTerminalStatus drives pollCommand (0% covered) via
// RecompileCommand: trigger, then poll recompile_status through a
// non-terminal state to "completed".
func TestRecompileWaitsForTerminalStatus(t *testing.T) {
	st := &scriptedExecTransport{steps: []protocol.Response{
		execResp(`{"success":true,"command":"recompile","result":{"status":"triggered"}}`),
		execResp(`{"success":true,"command":"recompile_status","result":"{\"status\":\"compiling\"}"}`),
		execResp(`{"success":true,"command":"recompile_status","result":"{\"status\":\"completed\"}"}`),
	}}
	got, err := RecompileCommand().Run(newRecompileRuntime(st), nil)
	if err != nil {
		t.Fatalf("recompile: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected a map result, got %T", got)
	}
	if m["status"] != "completed" {
		t.Fatalf("status = %v, want completed", m["status"])
	}
	if st.calls != 3 {
		t.Fatalf("expected 3 calls (trigger + 2 polls), got %d", st.calls)
	}
}

// TestRecompileNoWaitSkipsPolling locks that --no-wait returns right after
// the trigger, without calling pollCommand at all.
func TestRecompileNoWaitSkipsPolling(t *testing.T) {
	st := &scriptedExecTransport{steps: []protocol.Response{
		execResp(`{"success":true,"command":"recompile","result":{"status":"triggered"}}`),
	}}
	_, err := RecompileCommand().Run(newRecompileRuntime(st), []string{"--no-wait"})
	if err != nil {
		t.Fatalf("recompile --no-wait: %v", err)
	}
	if st.calls != 1 {
		t.Fatalf("expected exactly 1 call (trigger only), got %d", st.calls)
	}
}

// TestRecompileFailedStatusIsCommandFailed locks pollCommand's TerminalFail
// wiring: a "failed" recompile_status must surface as ExitCommandFailed, not
// hang or succeed.
func TestRecompileFailedStatusIsCommandFailed(t *testing.T) {
	st := &scriptedExecTransport{steps: []protocol.Response{
		execResp(`{"success":true,"command":"recompile","result":{"status":"triggered"}}`),
		execResp(`{"success":true,"command":"recompile_status","result":"{\"status\":\"failed\"}"}`),
	}}
	_, err := RecompileCommand().Run(newRecompileRuntime(st), nil)
	ue := protocol.AsUpipeError(err)
	if ue.Code != protocol.ExitCommandFailed {
		t.Fatalf("failed status -> exit %d (%s), want ExitCommandFailed", ue.Code, ue.Kind)
	}
}
