package protocol

import (
	"errors"
	"testing"
	"time"
)

func TestBackoffLadder(t *testing.T) {
	want := []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}
	for i, w := range want {
		if got := Backoff(i); got != w {
			t.Fatalf("Backoff(%d) = %v, want %v", i, got, w)
		}
	}
	if Backoff(99) != 30*time.Second {
		t.Fatal("attempts beyond the ladder must keep the last delay")
	}
	if Backoff(-1) != 200*time.Millisecond {
		t.Fatal("negative attempts clamp to the first delay")
	}
}

// TestParseLadder locks the pure-parsing contract for UPIPE_RELOAD_LADDER
// overrides (env reading itself lives in the cli layer; this package must
// stay IO-free).
func TestParseLadder(t *testing.T) {
	got, err := ParseLadder("200ms,500ms,1s,2m")
	if err != nil {
		t.Fatalf("ParseLadder: %v", err)
	}
	want := []time.Duration{200 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Minute}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %v want %v", i, got[i], want[i])
		}
	}

	// Whitespace around entries is tolerated.
	got2, err := ParseLadder(" 1s , 2s ")
	if err != nil || len(got2) != 2 {
		t.Fatalf("whitespace-tolerant parse failed: %v %v", got2, err)
	}
}

func TestParseLadderRejectsInvalidEntries(t *testing.T) {
	cases := []string{
		"",           // single empty entry
		"1s,,2s",     // empty entry mid-list
		"1s,notadur", // unparseable
		"0s",         // non-positive
		"-1s",        // negative
	}
	for _, c := range cases {
		if _, err := ParseLadder(c); err == nil {
			t.Errorf("ParseLadder(%q) should have failed", c)
		}
	}
}

// TestSetReloadLadderResetsOnEmpty locks the fallback contract: an empty/nil
// override must restore DefaultReloadLadder, not leave the ladder empty
// (which would make Backoff panic on index -1).
func TestSetReloadLadderResetsOnEmpty(t *testing.T) {
	orig := append([]time.Duration(nil), ReloadLadder...)
	defer SetReloadLadder(orig)

	custom := []time.Duration{1 * time.Second, 2 * time.Second}
	SetReloadLadder(custom)
	if len(ReloadLadder) != 2 || ReloadLadder[0] != time.Second {
		t.Fatalf("custom ladder not applied: %v", ReloadLadder)
	}

	SetReloadLadder(nil)
	if len(ReloadLadder) != len(DefaultReloadLadder) {
		t.Fatalf("nil override should reset to DefaultReloadLadder, got %v", ReloadLadder)
	}
	for i := range DefaultReloadLadder {
		if ReloadLadder[i] != DefaultReloadLadder[i] {
			t.Fatalf("reset ladder mismatch at %d: %v vs default %v", i, ReloadLadder, DefaultReloadLadder)
		}
	}

	SetReloadLadder(custom)
	SetReloadLadder([]time.Duration{})
	if len(ReloadLadder) != len(DefaultReloadLadder) {
		t.Fatalf("empty-slice override should also reset to default, got %v", ReloadLadder)
	}
}

// TestSetReloadLadderCopiesInput guards against callers later mutating the
// slice they passed in and silently changing the active ladder.
func TestSetReloadLadderCopiesInput(t *testing.T) {
	orig := append([]time.Duration(nil), ReloadLadder...)
	defer SetReloadLadder(orig)

	custom := []time.Duration{1 * time.Second, 2 * time.Second}
	SetReloadLadder(custom)
	custom[0] = 99 * time.Second
	if ReloadLadder[0] == 99*time.Second {
		t.Fatal("SetReloadLadder must copy its input, not alias it")
	}
}

// fakeTransport scripts its responses/errors in order.
type fakeTransport struct {
	steps []fakeStep
	calls int
}
type fakeStep struct {
	resp Response
	err  error
}

func (f *fakeTransport) Do(req Request) (Response, error) {
	if f.calls >= len(f.steps) {
		f.calls++
		return Response{}, errors.New("connection refused (script exhausted)")
	}
	s := f.steps[f.calls]
	f.calls++
	return s.resp, s.err
}
func (f *fakeTransport) Close() error { return nil }

func fixedDescriptor() *InstanceDescriptor { return &InstanceDescriptor{Port: 7802, EvalToken: "t"} }

func newTestClient(ft Transport) *PipelineClient {
	return &PipelineClient{
		Transport: ft,
		Source:    func() (*InstanceDescriptor, error) { return fixedDescriptor(), nil },
		Sleep:     NopSleep,
	}
}

func TestExecMapsHTTPStatuses(t *testing.T) {
	cases := []struct {
		status int
		body   string
		code   int
	}{
		{401, `{}`, ExitAuth},
		{403, `{}`, ExitAuth},
		{503, `{"error":"Busy","errorDetails":"editor settling"}`, ExitBusy},
		{429, `{}`, ExitBusy},
		{404, `{}`, ExitProtocol},
		{400, `{"error":"Bad Request","errorDetails":"nope"}`, ExitCommandFailed},
	}
	for _, tc := range cases {
		ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: tc.status, Body: []byte(tc.body)}}}}
		_, err := newTestClient(ft).Exec("x", ExecOptions{})
		if err == nil {
			t.Fatalf("status %d: expected error", tc.status)
		}
		ue := AsUpipeError(err)
		if ue.Code != tc.code {
			t.Fatalf("status %d: exit %d, want %d (%v)", tc.status, ue.Code, tc.code, ue)
		}
	}
}

func TestExecSuccessAndFailureEnvelopes(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"success":true,"command":"eval","result":4}`)}}}}
	r, err := newTestClient(ft).Exec("eval", ExecOptions{})
	if err != nil || string(r.Result) != "4" {
		t.Fatalf("success path broken: %v %+v", err, r)
	}

	ft = &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"success":false,"command":"recompile","error":"Compile Error","errorDetails":"CS1009"}`)}}}}
	_, err = newTestClient(ft).Exec("recompile", ExecOptions{})
	ue := AsUpipeError(err)
	if ue.Code != ExitCommandFailed || ue.Message != "CS1009" {
		t.Fatalf("failure envelope not mapped: %+v", ue)
	}
}

func TestReloadTolerantRetriesConnectionErrors(t *testing.T) {
	// First call: connection refused (domain reload in flight).
	// Second call: 503 settling. Third call: success.
	ft := &fakeTransport{steps: []fakeStep{
		{err: errors.New("connection refused")},
		{resp: Response{StatusCode: 503, Body: []byte(`{"error":"Busy","errorDetails":"settling"}`)}},
		{resp: Response{StatusCode: 200, Body: []byte(`{"success":true,"command":"x","result":null}`)}},
	}}
	_, err := newTestClient(ft).ExecReloadTolerant("x", ExecOptions{}, 5)
	if err != nil {
		t.Fatalf("expected recovery, got %v", err)
	}
	if ft.calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", ft.calls)
	}
}

func TestReloadTolerantExhaustsLadder(t *testing.T) {
	ft := &fakeTransport{} // always connection refused
	_, err := newTestClient(ft).ExecReloadTolerant("x", ExecOptions{}, 3)
	ue := AsUpipeError(err)
	if ue.Code != ExitConnectionLost {
		t.Fatalf("want ConnectionLost, got %+v", ue)
	}
	if ft.calls != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", ft.calls)
	}
}

func TestReloadTolerantFailsFastOnSemanticError(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{
		{resp: Response{StatusCode: 401, Body: []byte(`{}`)}},
	}}
	_, err := newTestClient(ft).ExecReloadTolerant("x", ExecOptions{}, 5)
	ue := AsUpipeError(err)
	if ue.Code != ExitAuth || ft.calls != 1 {
		t.Fatalf("auth must fail fast: %+v calls=%d", ue, ft.calls)
	}
}
