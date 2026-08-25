package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// captureStderr swaps os.Stderr for the duration of fn and returns what was
// written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stderr = old
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func commandsBody() []byte {
	return []byte(`{"commands":[
		{"name":"prefab_census","description":"count prefabs by folder","tags":["assets"],"package":"Unity.Pipeline"},
		{"name":"editor_status","description":"editor state","tags":["core"],"package":"Unity.Pipeline"},
		{"name":"run_tests","description":"run PREFactors","tags":["test"],"package":"Unity.Pipeline"}
	],"total":3,"count":3}`)
}

// --query must filter the compact index client-side: case-insensitive
// substring match on name/description/tags, Count updated, Total untouched.
func TestCommandsQueryFiltersCompact(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{{StatusCode: 200, Body: commandsBody()}}}
	got, err := CommandsCommand().Run(newJobRuntime(st, 0), []string{"--query", "PREF"})
	if err != nil {
		t.Fatalf("commands --query: %v", err)
	}
	list, ok := got.(*protocol.CommandsResponse)
	if !ok {
		t.Fatalf("want *CommandsResponse, got %T", got)
	}
	if len(list.Commands) != 2 || list.Count != 2 {
		t.Fatalf("want 2 matches (prefab_census, run_tests), got %d: %+v", list.Count, list.Commands)
	}
	if list.Commands[0].Name != "prefab_census" || list.Commands[1].Name != "run_tests" {
		t.Fatalf("wrong survivors: %+v", list.Commands)
	}
	if list.Total != 3 {
		t.Fatalf("Total must keep the server-reported value, got %d", list.Total)
	}
}

// No match yields an empty list, not an error (an agent filters to probe).
func TestCommandsQueryNoMatch(t *testing.T) {
	st := &scriptedTransport{steps: []protocol.Response{{StatusCode: 200, Body: commandsBody()}}}
	got, err := CommandsCommand().Run(newJobRuntime(st, 0), []string{"--query", "zzz-nothing"})
	if err != nil {
		t.Fatalf("commands --query: %v", err)
	}
	list := got.(*protocol.CommandsResponse)
	if len(list.Commands) != 0 || list.Count != 0 {
		t.Fatalf("want empty result, got %+v", list.Commands)
	}
}

// --full --query must filter the raw index while preserving unknown
// per-command fields (parameter schemas etc.) byte-for-byte.
func TestCommandsQueryFiltersFull(t *testing.T) {
	raw := []byte(`{"commands":[
		{"name":"prefab_census","description":"count prefabs","schema":{"params":[{"name":"root"}]}},
		{"name":"editor_status","description":"state","schema":{"params":[]}}
	],"total":2,"count":2}`)
	out, err := filterFullCommands(raw, "prefab")
	if err != nil {
		t.Fatalf("filterFullCommands: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"prefab_census"`) || strings.Contains(s, `"editor_status"`) {
		t.Fatalf("wrong filter result: %s", s)
	}
	if !strings.Contains(s, `"schema"`) {
		t.Fatalf("full-mode filtering must preserve schema fields: %s", s)
	}
	if !strings.Contains(s, `"count":1`) {
		t.Fatalf("count must reflect the filtered length: %s", s)
	}
}

// Subcommand --help must print real usage (synopsis + registered flags) and
// return an exit-2 usage error instead of a bare one-liner.
func TestSubCommandHelpPrintsUsage(t *testing.T) {
	rt := newJobRuntime(&scriptedTransport{}, 0)
	var runErr error
	out := captureStderr(t, func() {
		_, runErr = CommandsCommand().Run(rt, []string{"--help"})
	})
	if runErr == nil {
		t.Fatal("--help must surface as a usage error")
	}
	if code := protocol.AsUpipeError(runErr).Code; code != protocol.ExitUsage {
		t.Fatalf("want exit code 2 (ExitUsage), got %d", code)
	}
	for _, want := range []string{
		"Usage: upipe commands [flags] [args]",
		"list the commands exposed by the connected editor instance",
		"-query",
		"-full",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage output missing %q:\n%s", want, out)
		}
	}
}

// Sub-subcommand flag sets (e.g. "plugin install") resolve their parent's
// registry entry for the synopsis line.
func TestLookupSubCommandParentFallback(t *testing.T) {
	sc, ok := lookupSubCommand("plugin install")
	if !ok || sc.Name != "plugin" {
		t.Fatalf("want parent `plugin` synopsis fallback, got %+v ok=%v", sc, ok)
	}
	if _, ok := lookupSubCommand("no-such-command"); ok {
		t.Fatal("unknown flag-set name must not resolve")
	}
}
