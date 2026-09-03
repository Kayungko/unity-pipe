package cli

import (
	"bytes"
	"flag"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// reorderForFlags is unexported; these are direct unit tests in the internal
// package (as opposed to app_test.go's cli_test black-box e2e tests) to
// cover branches the e2e tests never exercise: the "--" terminator, a
// name=value token whose name is unknown to the shared flag set, and a
// known value-taking/bool flag with nothing left to consume after it.

func newSharedFS() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("project-path", "", "")
	fs.Bool("json", false, "")
	return fs
}

func TestReorderForFlagsDoubleDashTerminator(t *testing.T) {
	fs := newSharedFS()
	got := reorderForFlags(fs, []string{"--project-path", "X", "--", "--unknown", "positional"})
	want := []string{"--project-path", "X", "--unknown", "positional"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A name=value token whose name the shared flag set does not recognize (a
// subcommand-only flag, e.g. pkg-install's --source=x.tgz) must stay
// positional so the subcommand's own parser sees it untouched.
func TestReorderForFlagsUnknownEqualsFormStaysPositional(t *testing.T) {
	fs := newSharedFS()
	got := reorderForFlags(fs, []string{"--source=x.tgz"})
	want := []string{"--source=x.tgz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A known equals-form shared flag is hoisted to the front, ahead of any
// positional/subcommand-only arguments.
func TestReorderForFlagsKnownEqualsFormHoisted(t *testing.T) {
	fs := newSharedFS()
	got := reorderForFlags(fs, []string{"positional", "--project-path=X"})
	want := []string{"--project-path=X", "positional"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A known VALUE-taking flag with nothing left to consume (it's the very
// last token) must still be hoisted bare, without eating a value that isn't
// there — flag.Parse itself reports the "missing value" error downstream.
func TestReorderForFlagsValueFlagAtEndOfArgs(t *testing.T) {
	fs := newSharedFS()
	got := reorderForFlags(fs, []string{"positional1", "--project-path"})
	want := []string{"--project-path", "positional1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A known BOOL flag is hoisted bare even when a following token exists —
// bool flags never consume the next token as their value.
func TestReorderForFlagsBoolFlagNeverConsumesNextToken(t *testing.T) {
	fs := newSharedFS()
	got := reorderForFlags(fs, []string{"--json", "positional"})
	want := []string{"--json", "positional"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// render's human-readable (non --json) success path pretty-prints the
// payload to stdout; the e2e tests in app_test.go all use --json, leaving
// this branch (and writeJSON's own error path) untested. captureStdout is a
// package-local helper distinct from app_test.go's black-box runCLI, since
// render is unexported and only reachable from within package cli.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stdout = old
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}

func TestRenderHumanModePrettyPrintsPayload(t *testing.T) {
	opts := &options.Options{JSONOut: false}
	out := captureStdout(t, func() {
		code := render(nil, opts, map[string]any{"hello": "world"})
		if code != protocol.ExitOK {
			t.Fatalf("code = %d, want ExitOK", code)
		}
	})
	if !strings.Contains(out, `"hello": "world"`) {
		t.Fatalf("expected pretty-printed JSON on stdout, got %q", out)
	}
}

func TestRenderHumanModeNoPayloadPrintsNothing(t *testing.T) {
	opts := &options.Options{JSONOut: false}
	out := captureStdout(t, func() {
		code := render(nil, opts)
		if code != protocol.ExitOK {
			t.Fatalf("code = %d, want ExitOK", code)
		}
	})
	if out != "" {
		t.Fatalf("expected no stdout output for a nil payload, got %q", out)
	}
}

// TestRenderHumanModeErrorGoesToStderr locks the non-JSON error path: the
// error text (not a JSON envelope) goes to stderr, and the exit code is the
// UpipeError's own code.
func TestRenderHumanModeErrorGoesToStderr(t *testing.T) {
	opts := &options.Options{JSONOut: false}
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := render(protocol.ErrNoInstance("no editor"), opts)
	os.Stderr = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if code != protocol.ExitNoInstance {
		t.Fatalf("code = %d, want ExitNoInstance", code)
	}
	if !strings.Contains(string(out), "no editor") {
		t.Fatalf("expected error message on stderr, got %q", out)
	}
}

// TestRenderHumanModeMarshalFailureReportsExitInternal locks the render()
// branch where json.MarshalIndent itself fails on a payload the caller
// handed it (e.g. a value containing a channel, which JSON cannot encode) —
// distinct from writeJSON's own encode-failure path (which os.Exit()s and so
// cannot be driven from a normal test).
func TestRenderHumanModeMarshalFailureReportsExitInternal(t *testing.T) {
	opts := &options.Options{JSONOut: false}
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	code := render(nil, opts, map[string]any{"bad": make(chan int)})
	os.Stderr = old
	_ = w.Close()
	out, _ := io.ReadAll(r)
	if code != protocol.ExitInternal {
		t.Fatalf("code = %d, want ExitInternal", code)
	}
	if !strings.Contains(string(out), "render") {
		t.Fatalf("expected a render-failure message on stderr, got %q", out)
	}
}
