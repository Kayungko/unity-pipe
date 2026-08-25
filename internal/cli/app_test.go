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

	"github.com/Kayungko/unity-pipe/internal/cli"
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
