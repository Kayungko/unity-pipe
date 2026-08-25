package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// captureTransport records the last request and replays a fixed response.
type captureTransport struct {
	last protocol.Request
	resp protocol.Response
}

func (c *captureTransport) Do(req protocol.Request) (protocol.Response, error) {
	c.last = req
	return c.resp, nil
}
func (c *captureTransport) Close() error { return nil }

// newCaptureRuntime wires a Runtime whose client talks to captureTransport —
// the commands layer must stay transport-agnostic, so this is the only seam
// tests need to observe the exact wire body of POST /api/exec.
func newCaptureRuntime(ft *captureTransport) *Runtime {
	return &Runtime{
		Opts: &options.Options{},
		Client: &protocol.PipelineClient{
			Transport: ft,
			Source: func() (*protocol.InstanceDescriptor, error) {
				return &protocol.InstanceDescriptor{Port: 7802, EvalToken: "t"}, nil
			},
			Sleep: protocol.NopSleep,
		},
	}
}

func execBody(t *testing.T, ft *captureTransport) (command string, params map[string]any, topLevelTimeout *int64) {
	t.Helper()
	if ft.last.Method != "POST" || ft.last.Path != "/api/exec" {
		t.Fatalf("expected POST /api/exec, got %s %s", ft.last.Method, ft.last.Path)
	}
	var body struct {
		Command    string         `json:"command"`
		Parameters map[string]any `json:"parameters"`
		Timeout    *int64         `json:"timeout"`
	}
	if err := json.Unmarshal(ft.last.Body, &body); err != nil {
		t.Fatalf("unparseable exec body %s: %v", ft.last.Body, err)
	}
	return body.Command, body.Parameters, body.Timeout
}

func okExecTransport() *captureTransport {
	return &captureTransport{resp: protocol.Response{StatusCode: 200, Body: []byte(`{"success":true,"result":{}}`)}}
}

func TestEvalTimeoutSentAsNumberParameter(t *testing.T) {
	ft := okExecTransport()
	if _, err := EvalCommand().Run(newCaptureRuntime(ft), []string{"--eval-timeout", "60000", "return 2 + 2;"}); err != nil {
		t.Fatalf("eval: %v", err)
	}
	cmd, params, topTimeout := execBody(t, ft)
	if cmd != "eval" {
		t.Fatalf("command = %q, want eval", cmd)
	}
	if params["code"] != "return 2 + 2;" {
		t.Fatalf("code = %v", params["code"])
	}
	// The command-level timeout must land in parameters as a JSON NUMBER
	// (float64 after decoding) of raw milliseconds — a string "60000" or a
	// seconds value would be silently wrong on the server.
	v, ok := params["timeout"].(float64)
	if !ok {
		t.Fatalf("parameters.timeout = %#v, want JSON number", params["timeout"])
	}
	if v != 60000 {
		t.Fatalf("parameters.timeout = %v, want 60000 (milliseconds)", v)
	}
	if topTimeout != nil {
		t.Fatalf("body-level timeout = %v, want unset (shared --timeout not given)", *topTimeout)
	}
}

func TestEvalTimeoutOmittedByDefault(t *testing.T) {
	ft := okExecTransport()
	if _, err := EvalCommand().Run(newCaptureRuntime(ft), []string{"return 1;"}); err != nil {
		t.Fatalf("eval: %v", err)
	}
	_, params, _ := execBody(t, ft)
	if _, ok := params["timeout"]; ok {
		t.Fatalf("parameters.timeout = %v, want absent (server default 5000ms)", params["timeout"])
	}
}

func TestEvalFileTimeoutSent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scratch.cs")
	if err := os.WriteFile(path, []byte("return 3;"), 0o644); err != nil {
		t.Fatal(err)
	}
	ft := okExecTransport()
	if _, err := EvalFileCommand().Run(newCaptureRuntime(ft), []string{"--eval-timeout", "120000", path}); err != nil {
		t.Fatalf("eval-file: %v", err)
	}
	cmd, params, _ := execBody(t, ft)
	if cmd != "eval_file" {
		t.Fatalf("command = %q, want eval_file", cmd)
	}
	if params["file"] != path {
		t.Fatalf("file = %v", params["file"])
	}
	if v, ok := params["timeout"].(float64); !ok || v != 120000 {
		t.Fatalf("parameters.timeout = %#v, want number 120000", params["timeout"])
	}
}

func TestEvalTimeoutRangeChecked(t *testing.T) {
	for _, bad := range []string{"-1", "86400001"} {
		ft := okExecTransport()
		_, err := EvalCommand().Run(newCaptureRuntime(ft), []string{"--eval-timeout", bad, "return 1;"})
		if err == nil {
			t.Fatalf("--eval-timeout %s accepted", bad)
		}
		if ue := protocol.AsUpipeError(err); ue.Code != protocol.ExitUsage {
			t.Fatalf("--eval-timeout %s: code = %d, want ExitUsage", bad, ue.Code)
		}
	}
}

func TestEvalTimeoutIndependentOfSharedTimeout(t *testing.T) {
	// The shared --timeout (Go duration) must keep its own channel: the
	// body-level timeout hint (milliseconds) + round-trip budget — never
	// clobber the millisecond eval *parameter*.
	ft := okExecTransport()
	if _, err := EvalCommand().Run(newCaptureRuntime(ft), []string{"--timeout", "90s", "--eval-timeout", "60000", "return 1;"}); err != nil {
		t.Fatalf("eval: %v", err)
	}
	_, params, topTimeout := execBody(t, ft)
	if v, ok := params["timeout"].(float64); !ok || v != 60000 {
		t.Fatalf("parameters.timeout = %#v, want number 60000", params["timeout"])
	}
	if topTimeout == nil || *topTimeout != 90000 {
		t.Fatalf("body-level timeout = %v, want 90000 (ms from --timeout 90s)", topTimeout)
	}
}

func TestEvalFlagDocsMentionFlag(t *testing.T) {
	// Guards the usage contract: both synopses must advertise --eval-timeout
	// so the flag is discoverable in `upipe help`.
	for _, sc := range []SubCommand{EvalCommand(), EvalFileCommand()} {
		if !strings.Contains(sc.Synopsis, "--eval-timeout") {
			t.Errorf("%s: synopsis does not mention --eval-timeout: %q", sc.Name, sc.Synopsis)
		}
	}
}
