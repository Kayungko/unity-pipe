package commands

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/options"
)

func TestRegisterProjectDedupes(t *testing.T) {
	dir := t.TempDir()
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(dir, "projects.txt") }
	defer func() { knownProjectsFile = orig }()

	RegisterProject(`E:\proj\A`)
	RegisterProject(`E:\proj\A`) // exact dup
	RegisterProject(`e:\proj\a`) // case dup (Windows paths are case-insensitive)
	RegisterProject(`E:\proj\B`)

	got := readKnownProjects()
	if len(got) != 2 {
		t.Fatalf("expected 2 unique projects, got %v", got)
	}
	// Append-only: original line preserved verbatim.
	data, _ := os.ReadFile(knownProjectsFile())
	if !strings.Contains(string(data), `E:\proj\A`) || !strings.Contains(string(data), `E:\proj\B`) {
		t.Fatalf("registry content wrong: %q", data)
	}
	// Blank lines are skipped; any non-blank line counts as a path (the
	// reader cannot know which paths are "real").
	_ = os.WriteFile(knownProjectsFile(), []byte("\n  \nE:\\proj\\A\nsome-other-path\n"), 0o644)
	if got := readKnownProjects(); len(got) != 2 {
		t.Fatalf("reader should skip blank lines only, got %v", got)
	}
}

func TestRegisterProjectNoHome(t *testing.T) {
	// A failing home dir must not panic — registration is best-effort.
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(t.TempDir(), "no", "such", "dir", "x.txt") }
	defer func() { knownProjectsFile = orig }()
	RegisterProject(`E:\proj\C`) // mkdir fails under a file path? dir exists actually
	// Regardless of outcome, no panic is the contract.
}

// TestDeadProjectReasonMissingPortFile locks the "never installed / moved /
// deleted" classification: a root whose Library/Pipeline port file cannot be
// read is dead, regardless of the reason (ENOENT, permission, etc).
func TestDeadProjectReasonMissingPortFile(t *testing.T) {
	root := t.TempDir() // no Library/Pipeline/.unity-pipeline-port at all
	reason := deadProjectReason(root, time.Now())
	if reason == "" {
		t.Fatal("a project with no port file must be classified dead")
	}
	if !strings.Contains(reason, "unreadable") {
		t.Errorf("unexpected reason: %q", reason)
	}
}

// TestDeadProjectReasonCorruptPortFile locks the "corrupt descriptor"
// classification distinctly from "missing file".
func TestDeadProjectReasonCorruptPortFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Library", "Pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	portFile := filepath.Join(root, "Library", "Pipeline", ".unity-pipeline-port")
	if err := os.WriteFile(portFile, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason := deadProjectReason(root, time.Now())
	if !strings.Contains(reason, "corrupt") {
		t.Errorf("want a corrupt-port-file reason, got %q", reason)
	}
}

// TestDeadProjectReasonUnreachableAndStaleIsDead / ...FreshHeartbeatIsLive
// lock the "definitely dead" bar shared with the default instances listing:
// unreachable AND stale is dead; a fresh heartbeat (even if currently
// unreachable, e.g. a flaky probe) is treated as live.
func TestDeadProjectReasonUnreachableAndStaleIsDead(t *testing.T) {
	root := t.TempDir()
	writeFakePortFile(t, root, 1) // port 1 is never a live pipeline server
	old := time.Now().Add(-time.Hour)
	reason := deadProjectReason(root, old.Add(2*time.Hour)) // "now" far past heartbeat
	if reason == "" {
		t.Fatal("unreachable + stale heartbeat must be classified dead")
	}
}

func TestDeadProjectReasonFreshHeartbeatIsLive(t *testing.T) {
	root := t.TempDir()
	writeFakePortFile(t, root, 1) // unreachable port, but...
	reason := deadProjectReason(root, time.Now())
	if reason != "" {
		t.Fatalf("a fresh heartbeat must be treated as live even if unreachable, got reason %q", reason)
	}
}

// writeFakePortFile writes a syntactically valid port file with a recent
// heartbeat pointing at the given (likely closed) port.
func writeFakePortFile(t *testing.T, root string, port int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "Library", "Pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	portFile := filepath.Join(root, "Library", "Pipeline", ".unity-pipeline-port")
	body := `{"pid":1,"port":` + strconv.Itoa(port) + `,"projectPath":"` + filepath.ToSlash(root) +
		`","projectName":"Fake","unityVersion":"2022.3.23f1","mode":"editor","lastHeartbeat":"` +
		time.Now().Format(time.RFC3339) + `","evalToken":"t"}`
	if err := os.WriteFile(portFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPruneKnownProjectsRemovesDeadKeepsLive locks the end-to-end prune
// behavior: dead entries are removed from the on-disk registry and reported;
// live entries survive untouched.
func TestPruneKnownProjectsRemovesDeadKeepsLive(t *testing.T) {
	dir := t.TempDir()
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(dir, "projects.txt") }
	defer func() { knownProjectsFile = orig }()

	deadRoot := t.TempDir() // no port file — dead
	liveRoot := t.TempDir()
	writeFakePortFile(t, liveRoot, 1) // fresh heartbeat — live even if unreachable

	if err := writeKnownProjects([]string{deadRoot, liveRoot}); err != nil {
		t.Fatal(err)
	}

	removed, err := pruneKnownProjects(time.Now())
	if err != nil {
		t.Fatalf("pruneKnownProjects: %v", err)
	}
	if len(removed) != 1 || removed[0]["project"] != deadRoot {
		t.Fatalf("expected exactly deadRoot removed, got %v", removed)
	}

	remaining := readKnownProjects()
	if len(remaining) != 1 || !strings.EqualFold(remaining[0], liveRoot) {
		t.Fatalf("registry after prune should contain only liveRoot, got %v", remaining)
	}
}

// TestPruneKnownProjectsNoopWhenAllLive locks that a no-op prune (nothing
// dead) does not rewrite the registry file at all — avoids spurious mtime
// churn / lock contention on a file nothing needed to change.
func TestPruneKnownProjectsNoopWhenAllLive(t *testing.T) {
	dir := t.TempDir()
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(dir, "projects.txt") }
	defer func() { knownProjectsFile = orig }()

	liveRoot := t.TempDir()
	writeFakePortFile(t, liveRoot, 1)
	if err := writeKnownProjects([]string{liveRoot}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(knownProjectsFile())

	removed, err := pruneKnownProjects(time.Now())
	if err != nil {
		t.Fatalf("pruneKnownProjects: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removals, got %v", removed)
	}
	after, _ := os.Stat(knownProjectsFile())
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("registry file was rewritten even though nothing was pruned")
	}
}

// TestInstancesCommandPruneFlagEndToEnd drives the InstancesCommand.Run body
// with --prune and checks the report shape plus the on-disk effect.
func TestInstancesCommandPruneFlagEndToEnd(t *testing.T) {
	dir := t.TempDir()
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(dir, "projects.txt") }
	defer func() { knownProjectsFile = orig }()

	deadRoot := t.TempDir()
	if err := writeKnownProjects([]string{deadRoot}); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Opts: &options.Options{}}
	cmd := InstancesCommand()
	res, err := cmd.Run(rt, []string{"--prune"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected a map result, got %T", res)
	}
	prunedCount, ok := m["prunedCount"].(int)
	if !ok || prunedCount != 1 {
		t.Fatalf("expected prunedCount=1, got %v", m["prunedCount"])
	}
	if remaining := readKnownProjects(); len(remaining) != 0 {
		t.Fatalf("registry should be empty after pruning the only (dead) entry, got %v", remaining)
	}
}
