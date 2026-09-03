package protocol

import (
	"strings"
	"testing"
	"time"
)

// These tests exercise the thin GET-endpoint wrappers around getJSON/do that
// carried 0% coverage: Status, EditorStatus, ListCommands, ListCommandsFull,
// GetJob, Progress, TestStatus. They reuse fakeTransport/newTestClient from
// retry_test.go (same package, same file-group convention in this repo).

func TestClientStatus(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200, Body: []byte(`{"status":"running","version":"1"}`)}}}}
	raw, err := newTestClient(ft).Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !strings.Contains(string(raw), `"running"`) {
		t.Fatalf("unexpected body: %s", raw)
	}
}

func TestClientEditorStatus(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"success":true,"command":"editor_status","result":{"state":"ready"}}`)}}}}
	r, err := newTestClient(ft).EditorStatus()
	if err != nil {
		t.Fatalf("EditorStatus: %v", err)
	}
	if !r.Success || string(r.Result) != `{"state":"ready"}` {
		t.Fatalf("unexpected response: %+v", r)
	}
}

func TestClientListCommands(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"commands":[{"name":"eval"}],"total":1,"count":1}`)}}}}
	r, err := newTestClient(ft).ListCommands()
	if err != nil {
		t.Fatalf("ListCommands: %v", err)
	}
	if r.Total != 1 || len(r.Commands) != 1 || r.Commands[0].Name != "eval" {
		t.Fatalf("unexpected response: %+v", r)
	}
}

func TestClientListCommandsFull(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"commands":[{"name":"eval","parameters":{}}]}`)}}}}
	raw, err := newTestClient(ft).ListCommandsFull()
	if err != nil {
		t.Fatalf("ListCommandsFull: %v", err)
	}
	if !strings.Contains(string(raw), `"parameters"`) {
		t.Fatalf("unexpected body: %s", raw)
	}
}

func TestClientGetJob(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200,
		Body: []byte(`{"jobId":"j1","state":"running"}`)}}}}
	r, err := newTestClient(ft).GetJob("j1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if r.JobId != "j1" || r.State != "running" {
		t.Fatalf("unexpected response: %+v", r)
	}
}

func TestClientProgress(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200, Body: []byte(`{"active":true}`)}}}}
	raw, err := newTestClient(ft).Progress()
	if err != nil {
		t.Fatalf("Progress: %v", err)
	}
	if !strings.Contains(string(raw), `"active":true`) {
		t.Fatalf("unexpected body: %s", raw)
	}
}

func TestClientTestStatus(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200, Body: []byte(`{"status":"running"}`)}}}}
	raw, err := newTestClient(ft).TestStatus()
	if err != nil {
		t.Fatalf("TestStatus: %v", err)
	}
	if !strings.Contains(string(raw), `"running"`) {
		t.Fatalf("unexpected body: %s", raw)
	}
}

// TestClientGetJSONPropagatesTransportErrors locks that the getJSON helper
// (used by EditorStatus/ListCommands/GetJob) surfaces do()'s error mapping
// (e.g. auth) rather than swallowing it.
func TestClientGetJSONPropagatesTransportErrors(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 401, Body: []byte(`{}`)}}}}
	_, err := newTestClient(ft).EditorStatus()
	if AsUpipeError(err).Code != ExitAuth {
		t.Fatalf("expected auth error, got %v", err)
	}
}

// TestClientGetJSONUnparseableBody locks the "unparseable response" error
// path distinctly from a transport-level failure.
func TestClientGetJSONUnparseableBody(t *testing.T) {
	ft := &fakeTransport{steps: []fakeStep{{resp: Response{StatusCode: 200, Body: []byte(`not json`)}}}}
	_, err := newTestClient(ft).EditorStatus()
	ue := AsUpipeError(err)
	if ue.Code != ExitProtocol {
		t.Fatalf("expected ExitProtocol for unparseable body, got %+v", ue)
	}
}

// TestClientSleepDefaultsToRealTimeSleepWhenUnset locks the nil-Sleep
// fallback branch in (*PipelineClient).sleep: when Sleep is not injected
// (the production default, as opposed to every other test in this package
// which injects NopSleep), it must fall back to the real time.Sleep rather
// than panic on a nil func call. A tiny duration keeps the test fast while
// still exercising the real code path.
func TestClientSleepDefaultsToRealTimeSleepWhenUnset(t *testing.T) {
	c := &PipelineClient{} // Sleep left nil on purpose
	start := time.Now()
	c.sleep(5 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Fatalf("sleep(5ms) returned after only %v — nil Sleep should fall back to a real wait", elapsed)
	}
}
