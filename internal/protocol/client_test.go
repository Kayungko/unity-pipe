package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

// recordingTransport captures every request so tests can assert on the wire
// format (body fields, round-trip deadline) while scripting responses.
type recordingTransport struct {
	fakeTransport
	reqs []Request
}

func (r *recordingTransport) Do(req Request) (Response, error) {
	r.reqs = append(r.reqs, req)
	return r.fakeTransport.Do(req)
}

func okStep() fakeStep {
	return fakeStep{resp: Response{StatusCode: 200, Body: []byte(`{"success":true,"command":"x","result":null}`)}}
}

// The server consumes the request-body timeout as milliseconds
// (CommandExecutionRequest.Timeout → dispatcherTimeoutMs). The client must
// therefore send milliseconds, not seconds.
func TestExecSendsTimeoutInMillis(t *testing.T) {
	rt := &recordingTransport{fakeTransport: fakeTransport{steps: []fakeStep{okStep()}}}
	_, err := newTestClient(rt).Exec("x", ExecOptions{Timeout: 90 * time.Second})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if len(rt.reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(rt.reqs))
	}
	var body struct {
		Timeout *int64 `json:"timeout"`
	}
	if err := json.Unmarshal(rt.reqs[0].Body, &body); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if body.Timeout == nil {
		t.Fatal("timeout field missing from request body")
	}
	if *body.Timeout != 90000 {
		t.Fatalf("body timeout = %d, want 90000 (90s in milliseconds)", *body.Timeout)
	}
}

func TestExecOmitsTimeoutFieldWhenUnset(t *testing.T) {
	rt := &recordingTransport{fakeTransport: fakeTransport{steps: []fakeStep{okStep()}}}
	_, err := newTestClient(rt).Exec("x", ExecOptions{})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	if !json.Valid(rt.reqs[0].Body) {
		t.Fatalf("unexpected body: %s", rt.reqs[0].Body)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rt.reqs[0].Body, &raw)
	if _, present := raw["timeout"]; present {
		t.Fatalf("timeout must be omitted when unset, body: %s", rt.reqs[0].Body)
	}
}

// Without an explicit timeout the round-trip deadline must be the 10-minute
// default (+30s margin), not the transport's 120s default.
func TestExecAppliesDefaultRoundTripTimeout(t *testing.T) {
	rt := &recordingTransport{fakeTransport: fakeTransport{steps: []fakeStep{okStep()}}}
	_, err := newTestClient(rt).Exec("x", ExecOptions{})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	want := DefaultRoundTripTimeout + 30*time.Second
	if got := rt.reqs[0].Timeout; got != want {
		t.Fatalf("round-trip deadline = %v, want %v (10m default + 30s margin)", got, want)
	}

	// Explicit timeout keeps the same structure: value + 30s margin.
	rt2 := &recordingTransport{fakeTransport: fakeTransport{steps: []fakeStep{okStep()}}}
	_, _ = newTestClient(rt2).Exec("x", ExecOptions{Timeout: 2 * time.Minute})
	if got, want := rt2.reqs[0].Timeout, 2*time.Minute+30*time.Second; got != want {
		t.Fatalf("explicit round-trip = %v, want %v", got, want)
	}
}

// A server that stays busy (503) through the whole ladder must surface
// ErrBusy (exit 13) on exhaustion, not ErrConnectionLost (exit 11).
func TestReloadTolerantExhaustionPropagatesBusy(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{
		{resp: Response{StatusCode: 503, Body: []byte(`{"error":"Busy","errorDetails":"editor settling"}`)}},
		{resp: Response{StatusCode: 503, Body: []byte(`{"error":"Busy","errorDetails":"editor settling"}`)}},
		{resp: Response{StatusCode: 503, Body: []byte(`{"error":"Busy","errorDetails":"editor settling"}`)}},
	}}
	_, err := newTestClient(ft).ExecReloadTolerant("x", ExecOptions{}, 3)
	ue := AsUpipeError(err)
	if ue.Code != ExitBusy {
		t.Fatalf("want ExitBusy (13) after busy-exhausted ladder, got %+v", ue)
	}
	if !ue.Retryable {
		t.Fatalf("exhausted-busy must stay retryable: %+v", ue)
	}
	if ft.calls != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", ft.calls)
	}
}

// Connection-class exhaustion keeps the ErrConnectionLost contract.
func TestReloadTolerantExhaustionKeepsConnectionLost(t *testing.T) {
	ft := &fakeTransport{} // connection refused forever
	_, err := newTestClient(ft).ExecReloadTolerant("x", ExecOptions{}, 3)
	ue := AsUpipeError(err)
	if ue.Code != ExitConnectionLost {
		t.Fatalf("want ExitConnectionLost (11), got %+v", ue)
	}
}

// ErrBusy must carry the server's 503/429 response envelope in Data.
func TestErrBusyCarriesServerEnvelope(t *testing.T) {
	serverBody := `{"error":"Busy","errorDetails":"job queue full","activeJobs":100}`
	ft := &fakeTransport{steps: []fakeStep{
		{resp: Response{StatusCode: 503, Body: []byte(serverBody)}},
	}}
	_, err := newTestClient(ft).Exec("x", ExecOptions{})
	ue := AsUpipeError(err)
	if ue.Code != ExitBusy {
		t.Fatalf("want ExitBusy, got %+v", ue)
	}
	raw, ok := ue.Data.(json.RawMessage)
	if !ok || len(raw) == 0 {
		t.Fatalf("ErrBusy.Data must be a non-empty json.RawMessage, got %#v", ue.Data)
	}
	if string(raw) != serverBody {
		t.Fatalf("ErrBusy.Data = %s, want the server 503 body verbatim", raw)
	}
}
