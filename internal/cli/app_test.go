package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/cli"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// mockEditor implements enough of the pipeline HTTP API to exercise the CLI
// end-to-end: bearer auth, status, editor_status, commands and exec.
type mockEditor struct {
	srv     *httptest.Server
	project string
	token   string
	// execHandler lets individual tests override /api/exec behavior.
	execHandler func(command string, params map[string]any, job bool) (int, string)
}

func newMockEditor(t *testing.T) *mockEditor {
	t.Helper()
	m := &mockEditor{token: "test-token"}
	dir := t.TempDir()
	m.project = filepath.Join(dir, "FakeProj")
	if err := os.MkdirAll(filepath.Join(m.project, "Library", "Pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		m.authed(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running","version":"mock"}`))
	})
	mux.HandleFunc("/api/editor_status", func(w http.ResponseWriter, r *http.Request) {
		m.authed(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"command":"editor_status","result":{"state":"ready"}}`))
	})
	mux.HandleFunc("/api/commands", func(w http.ResponseWriter, r *http.Request) {
		m.authed(w, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commands":[{"name":"editor_status","description":"state","tags":["core"],"package":"Unity.Pipeline"}],"total":1,"count":1}`))
	})
	m.execHandler = func(command string, params map[string]any, job bool) (int, string) {
		if command == "" {
			return 400, `{"error":"Bad Request","errorDetails":"command required"}`
		}
		result, _ := json.Marshal(map[string]any{"echo": command, "params": params, "job": job})
		return 200, `{"success":true,"command":"` + command + `","result":` + string(result) + `}`
	}
	mux.HandleFunc("/api/exec", func(w http.ResponseWriter, r *http.Request) {
		m.authed(w, r)
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Command    string         `json:"command"`
			Parameters map[string]any `json:"parameters"`
			Job        bool           `json:"job"`
		}
		_ = json.Unmarshal(body, &req)
		status, payload := m.execHandler(req.Command, req.Parameters, req.Job)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	})

	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)

	// Write the port file pointing at the mock server.
	portJSON := `{"pid": 1, "port": ` + portOf(m.srv.URL) + `, "projectPath": "` + strings.ReplaceAll(m.project, `\`, `\\`) +
		`", "projectName": "FakeProj", "unityVersion": "2022.3.23f1", "mode": "editor", "evalToken": "` + m.token + `"}`
	if err := os.WriteFile(filepath.Join(m.project, "Library", "Pipeline", ".unity-pipeline-port"), []byte(portJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *mockEditor) authed(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+m.token {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}
}

func portOf(url string) string {
	return url[strings.LastIndex(url, ":")+1:]
}

// runCLI invokes cli.Main capturing stdout; returns exit code + decoded envelope.
func runCLI(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	code := cli.Main(args)
	os.Stdout = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	env := map[string]any{}
	if len(out) > 0 {
		if err := json.Unmarshal(out, &env); err != nil {
			t.Fatalf("stdout is not a JSON envelope: %q (%v)", string(out), err)
		}
	}
	return code, env
}

func TestStatusEndToEnd(t *testing.T) {
	m := newMockEditor(t)
	code, env := runCLI(t, "--project-path", m.project, "--json", "status")
	if code != 0 {
		t.Fatalf("exit=%d env=%v", code, env)
	}
	if env["ok"] != true {
		t.Fatalf("ok missing: %v", env)
	}
	data := env["data"].(map[string]any)
	ins := data["instance"].(map[string]any)
	if ins["port"].(float64) != 7802 && ins["port"].(float64) <= 0 {
		t.Fatalf("bad instance: %v", ins)
	}
	if _, has := ins["evalToken"]; has {
		t.Fatal("evalToken must never appear in output")
	}
}

func TestExecEndToEnd(t *testing.T) {
	m := newMockEditor(t)
	code, env := runCLI(t, "--project-path", m.project, "--json", "exec", "set_autotick", "--set", "enable=true", "--set", "fps=60")
	if code != 0 {
		t.Fatalf("exit=%d env=%v", code, env)
	}
	data := env["data"].(map[string]any)
	res := data["result"].(map[string]any)
	if res["echo"] != "set_autotick" {
		t.Fatalf("bad echo: %v", res)
	}
	params := res["params"].(map[string]any)
	if params["enable"] != true || params["fps"].(float64) != 60 {
		t.Fatalf("type coercion failed: %v", params)
	}
}

func TestCommandsEndToEnd(t *testing.T) {
	m := newMockEditor(t)
	code, env := runCLI(t, "--project-path", m.project, "--json", "commands")
	if code != 0 {
		t.Fatalf("exit=%d env=%v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["total"].(float64) != 1 {
		t.Fatalf("bad commands payload: %v", data)
	}
}

func TestNoInstanceExitCode(t *testing.T) {
	empty := t.TempDir()
	code, env := runCLI(t, "--project-path", empty, "--json", "status")
	if code != 10 {
		t.Fatalf("want exit 10 (NoInstance), got %d env=%v", code, env)
	}
	if env["ok"] != false {
		t.Fatal("expected ok=false envelope")
	}
}

func TestUnknownCommandExitCode(t *testing.T) {
	code, _ := runCLI(t, "definitely-not-a-command")
	if code != 2 {
		t.Fatalf("want exit 2 (usage), got %d", code)
	}
}

func TestRecompilePollingEndToEnd(t *testing.T) {
	m := newMockEditor(t)
	// Stateful recompile behavior: trigger → compiling → completed.
	polls := 0
	m.execHandler = func(command string, params map[string]any, job bool) (int, string) {
		switch command {
		case "recompile":
			return 200, `{"success":true,"command":"recompile","result":{"status":"triggered"}}`
		case "recompile_status":
			polls++
			if polls < 2 {
				return 200, `{"success":true,"command":"recompile_status","result":{"status":"compiling"}}`
			}
			return 200, `{"success":true,"command":"recompile_status","result":{"status":"completed","failed":false,"errors":[]}}`
		default:
			return 400, `{"error":"unknown"}`
		}
	}
	code, env := runCLI(t, "--project-path", m.project, "--json", "recompile")
	if code != 0 {
		t.Fatalf("exit=%d env=%v", code, env)
	}
	data := env["data"].(map[string]any)
	if data["status"] != "completed" || polls != 2 {
		t.Fatalf("want status=completed after 2 polls, got %v polls=%d", data, polls)
	}
}

func TestTestCommandPassAndFail(t *testing.T) {
	m := newMockEditor(t)
	// Async test run whose terminal body is the raw status-file JSON —
	// crucially WITHOUT a top-level "success" field (real server behavior;
	// regression for the false-failure bug).
	m.execHandler = func(command string, params map[string]any, job bool) (int, string) {
		switch command {
		case "run_tests":
			return 200, `{"success":true,"command":"run_tests","result":{"status":"running"}}`
		case "test_status":
			return 200, `{"success":true,"command":"test_status","result":{"status":"completed","summary":{"total":1,"passed":1,"failed":0},"results":[{"FullName":"T.Passes","Status":"Passed"}]}}`
		default:
			return 400, `{"error":"unknown"}`
		}
	}
	code, env := runCLI(t, "--project-path", m.project, "--json", "test", "--mode", "editor", "--filter", "T")
	if code != 0 {
		t.Fatalf("passing run must exit 0, got %d env=%v", code, env)
	}

	// Same shape, one failure → exit 20 with structured data.
	m.execHandler = func(command string, params map[string]any, job bool) (int, string) {
		switch command {
		case "run_tests":
			return 200, `{"success":true,"command":"run_tests","result":{"status":"running"}}`
		case "test_status":
			return 200, `{"success":true,"command":"test_status","result":{"status":"completed","summary":{"total":1,"passed":0,"failed":1},"results":[{"FullName":"T.Fails","Status":"Failed","Message":"boom"}]}}`
		default:
			return 400, `{"error":"unknown"}`
		}
	}
	code, env = runCLI(t, "--project-path", m.project, "--json", "test", "--mode", "editor", "--filter", "T")
	if code != 20 {
		t.Fatalf("failing run must exit 20, got %d", code)
	}
	e := env["error"].(map[string]any)
	if e["kind"] != "CommandFailed" {
		t.Fatalf("kind mismatch: %v", e)
	}
	data := e["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if summary["failed"].(float64) != 1 {
		t.Fatalf("summary must carry failure count: %v", summary)
	}
}

// Regression: shared flags AFTER the subcommand (the documented shape
// `upipe status --project-path X`) must reach the runtime before project
// resolution — this shape silently failed with NoInstance before v0.1.6.
func TestStatusProjectPathAfterSubcommand(t *testing.T) {
	m := newMockEditor(t)
	code, env := runCLI(t, "--json", "status", "--project-path", m.project)
	if code != 0 {
		t.Fatalf("exit %d: %v", code, env)
	}
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("no data envelope: %v", env)
	}
	if _, ok := data["instance"]; !ok {
		t.Fatal("no instance in status output — project-path after subcommand was ignored")
	}
}

// Same regression for value flags with = form after the subcommand.
func TestProjectPathEqualsFormAfterSubcommand(t *testing.T) {
	m := newMockEditor(t)
	code, env := runCLI(t, "--json", "status", "--project-path="+m.project)
	if code != 0 {
		t.Fatalf("exit %d: %v", code, env)
	}
	data, _ := env["data"].(map[string]any)
	if _, ok := data["instance"]; !ok {
		t.Fatalf("instance missing: %v", env)
	}
}

// captureStderrCLI runs cli.Main capturing stderr; returns exit code + output.
func captureStderrCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := cli.Main(args)
	os.Stderr = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	_ = r.Close()
	return code, string(out)
}

// Top-level --help must show the command table, not only shared flag defaults.
func TestTopLevelHelpShowsCommandTable(t *testing.T) {
	code, out := captureStderrCLI(t, "--help")
	if code != 2 {
		t.Fatalf("want exit 2 (usage), got %d", code)
	}
	for _, want := range []string{
		"Usage:\n  upipe [global flags] <command> [flags] [args]",
		"Commands:",
		"eval-file",   // registry entry
		"pkg-install", // registry entry
		"-project-path",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("top-level help missing %q:\n%s", want, out)
		}
	}
}

// Subcommand --help must print that command's real flag list WITHOUT a
// running editor instance (help is read before instance resolution).
func TestSubCommandHelpWithoutInstance(t *testing.T) {
	code, out := captureStderrCLI(t, "eval", "--help")
	if code != 2 {
		t.Fatalf("want exit 2 (usage), got %d; output:\n%s", code, out)
	}
	for _, want := range []string{
		"Usage: upipe eval [flags] [args]",
		"-eval-timeout",
		"-project-path",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("eval --help missing %q:\n%s", want, out)
		}
	}
}

// Any successful command registers its project in ~/.upipe/projects.txt
// (best-effort) so `upipe instances` can discover it — deduplicated on repeat.
func TestSuccessfulCommandRegistersProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	m := newMockEditor(t)
	for i := 0; i < 2; i++ { // second run must not duplicate the line
		code, env := runCLI(t, "--project-path", m.project, "--json", "status")
		if code != 0 {
			t.Fatalf("run %d: exit=%d env=%v", i, code, env)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, ".upipe", "projects.txt"))
	if err != nil {
		t.Fatalf("project not registered after a successful command: %v", err)
	}
	if got := strings.Count(string(data), m.project); got != 1 {
		t.Fatalf("want exactly one registry line for the project, got %d:\n%s", got, data)
	}
}

// TestSharedFlagPrepassSilentForSubcommandFlags locks the fix for the
// "flag provided but not defined: -source" stderr noise: the shared-flag
// pre-parse deliberately discards errors (the subcommand's own flag pass is
// the authority), so it must not print usage for flags it doesn't know.
// A subcommand flag in either dash form (Go's flag treats -x/--x equally)
// must leave stderr clean while still reaching the command.
func TestSharedFlagPrepassSilentForSubcommandFlags(t *testing.T) {
	m := newMockEditor(t)

	for _, dash := range []string{"-", "--"} {
		code, stderr := captureStderrCLI(t, "--project-path", m.project, "--json",
			"exec", "set_autotick", dash+"set", "enable=true")
		if code != 0 {
			t.Fatalf("dash %q: exit=%d stderr=%s", dash, code, stderr)
		}
		if strings.Contains(stderr, "flag provided but not defined") {
			t.Errorf("dash %q: shared pre-pass leaked usage noise to stderr:\n%s", dash, stderr)
		}
		if strings.Contains(stderr, "Usage of upipe") {
			t.Errorf("dash %q: usage banner leaked by the pre-pass:\n%s", dash, stderr)
		}
	}
}

// TestVersionCommandIgnoresProjectPathNoise pins the pre-pass silencing for a
// NoRuntime command with a subcommand flag after it — same leak path.
func TestVersionCommandIgnoresProjectPathNoise(t *testing.T) {
	code, stderr := captureStderrCLI(t, "--json", "version")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "flag provided but not defined") {
		t.Errorf("stderr noise leaked:\n%s", stderr)
	}
}

// TestReloadLadderEnvOverride locks the UPIPE_RELOAD_LADDER wiring: a valid
// override is applied to protocol.ReloadLadder before any command runs.
func TestReloadLadderEnvOverride(t *testing.T) {
	t.Setenv("UPIPE_RELOAD_LADDER", "1ms,2ms")
	orig := append([]time.Duration(nil), protocol.ReloadLadder...)
	defer protocol.SetReloadLadder(orig)

	code, _ := runCLI(t, "--json", "version")
	if code != 0 {
		t.Fatalf("version should succeed regardless of ladder override, got %d", code)
	}
	if len(protocol.ReloadLadder) != 2 || protocol.ReloadLadder[0] != time.Millisecond {
		t.Fatalf("UPIPE_RELOAD_LADDER override not applied: %v", protocol.ReloadLadder)
	}
}

// TestReloadLadderEnvOverrideRejectsGarbage locks the fail-fast contract: a
// malformed UPIPE_RELOAD_LADDER must exit 2 (usage) before any command runs,
// not silently fall back to the default ladder.
func TestReloadLadderEnvOverrideRejectsGarbage(t *testing.T) {
	t.Setenv("UPIPE_RELOAD_LADDER", "not-a-duration")
	orig := append([]time.Duration(nil), protocol.ReloadLadder...)
	defer protocol.SetReloadLadder(orig)

	code, stderr := captureStderrCLI(t, "version")
	if code != 2 {
		t.Fatalf("want exit 2 (usage) for a malformed ladder, got %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "UPIPE_RELOAD_LADDER") {
		t.Errorf("error should name the offending variable, got: %s", stderr)
	}
}
