package commands

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func TestExecCommandOutcome(t *testing.T) {
	cases := []struct {
		name, command, result, errorContains string
	}{
		{"script compile error", "run_script", `{"success":false,"error":"Compilation failed","diagnostics":[{"id":"CS0126"}]}`, "Compilation failed"},
		{"script runtime error", "run_script", `{"success":false,"error":"Runtime Error","errorDetails":"Probe.cs:13"}`, "Runtime Error"},
		{"script succeeds", "run_script", `{"success":true,"result":{"success":false}}`, ""},
		{"batch rollback", "batch", `{"reverted":true,"results":[{"success":false,"error":"component missing"},{"skipped":true}]}`, "component missing"},
		{"batch rollback budget", "batch", `{"reverted":true,"error":"batch time budget exceeded"}`, "time budget"},
		{"batch dry run", "batch", `{"dryRun":true,"valid":false,"reverted":false}`, ""},
		{"batch continues", "batch", `{"transactional":false,"reverted":false,"results":[{"success":false,"error":"bad op"}]}`, ""},
		{"job submission", "batch", `{"jobId":"j1","state":"queued"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &scriptedExecTransport{steps: []protocol.Response{
				execResp(`{"success":true,"result":` + tc.result + `}`),
			}}
			_, err := ExecCommand().Run(newRecompileRuntime(st), []string{tc.command})
			if tc.errorContains == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			got := protocol.AsUpipeError(err)
			if got == nil || got.Code != protocol.ExitCommandFailed || !strings.Contains(got.Message, tc.errorContains) {
				t.Fatalf("got %v, want exit 20 containing %q", err, tc.errorContains)
			}
			if data, ok := got.Data.(json.RawMessage); !ok || string(data) != tc.result {
				t.Fatalf("error.data = %v, want original result %s", got.Data, tc.result)
			}
			if st.calls != 1 {
				t.Fatalf("failed command was attempted %d times, want 1", st.calls)
			}
		})
	}
}

func TestJobCompletedWithBatchRollback(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{
		execResp(`{"jobId":"j1","command":"batch","state":"completed","result":{"reverted":true,"error":"time budget exceeded"}}`),
	}}
	_, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"j1"})
	got := protocol.AsUpipeError(err)
	if got == nil || got.Code != protocol.ExitCommandFailed || got.Message != "time budget exceeded" {
		t.Fatalf("got %v, want exit 20 with batch error", err)
	}
	if _, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"--no-wait", "j1"}); err != nil {
		t.Fatalf("job --no-wait should return the snapshot: %v", err)
	}
}

func TestJobCompletedWithScriptFailure(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{
		execResp(`{"jobId":"j1","command":"run_script","state":"completed","result":{"success":false,"error":"Compilation failed","diagnostics":[{"id":"CS0126"}]}}`),
	}}
	_, err := JobCommand().Run(newJobRuntime(st, time.Second), []string{"j1"})
	got := protocol.AsUpipeError(err)
	if got == nil || got.Code != protocol.ExitCommandFailed {
		t.Fatalf("got %v, want exit 20", err)
	}
	if data, ok := got.Data.(json.RawMessage); !ok || !strings.Contains(string(data), "CS0126") {
		t.Fatalf("error.data = %v, want original diagnostic", got.Data)
	}
}

func TestTestCommandRejectsDirtyAdditiveSceneBeforeSubmitting(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{
		execResp(`{"success":true,"result":{"scenes":[{"name":"Main","isActive":true,"isDirty":false},{"name":"Additive","path":"Assets/Additive.unity","isDirty":true}]}}`),
	}}
	_, err := TestCommand().Run(newJobRuntime(st, time.Second), []string{"--mode", "editor", "--no-wait"})
	got := protocol.AsUpipeError(err)
	if got == nil || got.Code != protocol.ExitCommandFailed || !strings.Contains(got.Message, "Assets/Additive.unity") {
		t.Fatalf("got %v, want exit 20 identifying the dirty scene", err)
	}
	if st.calls != 1 || !strings.Contains(string(st.last.Body), `"command":"list_open_scenes"`) {
		t.Fatalf("got %d requests, last %s, want only list_open_scenes", st.calls, st.last.Body)
	}
}

func TestTestCommandRequireMatch(t *testing.T) {
	cases := []struct {
		name, status string
		strict       bool
		want         int
	}{
		{"legacy empty result", `{"status":"completed","summary":{"total":0,"failed":0}}`, false, 0},
		{"strict empty result", `{"status":"completed","summary":{"total":0,"failed":0}}`, true, 20},
		{"strict no tests", `{"status":"no_tests"}`, true, 20},
		{"strict matched", `{"status":"completed","summary":{"total":1,"passed":1,"failed":0}}`, true, 0},
		{"matched failure", `{"status":"completed","summary":{"total":1,"failed":1},"results":[{"FullName":"Probe","Message":"assertion","StackTrace":"Probe.cs:12"}]}`, true, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := &scriptedExecTransport{steps: []protocol.Response{
				execResp(`{"success":true,"result":{"scenes":[{"name":"Clean","isDirty":false}]}}`),
				execResp(`{"success":true,"result":{"success":true,"status":"running"}}`),
				execResp(`{"success":true,"result":` + tc.status + `}`),
			}}
			args := []string{"--mode", "editor", "--filter", "Probe"}
			if tc.strict {
				args = append(args, "--require-match")
			}
			_, err := TestCommand().Run(newRecompileRuntime(st), args)
			if tc.want == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			got := protocol.AsUpipeError(err)
			if got == nil || got.Code != tc.want {
				t.Fatalf("got %v, want exit %d", err, tc.want)
			}
			if data, ok := got.Data.(json.RawMessage); !ok || string(data) != tc.status {
				t.Fatalf("error.data = %v, want original status %s", got.Data, tc.status)
			}
		})
	}
}

func TestTestCommandRequireMatchRejectsNoWait(t *testing.T) {
	st := &scriptedTransport{}
	_, err := TestCommand().Run(newJobRuntime(st, time.Second), []string{"--require-match", "--no-wait"})
	got := protocol.AsUpipeError(err)
	if got == nil || got.Code != protocol.ExitUsage || st.calls != 0 {
		t.Fatalf("got %v after %d requests, want usage error before calling Unity", err, st.calls)
	}
}
