package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// runPkgInstall drives the pkg-install command body with a minimal Runtime
// bound to a sandbox project, returning (stdout-json-ish result, error).
func runPkgInstall(t *testing.T, project string, args ...string) (any, error) {
	t.Helper()
	rt := &Runtime{
		Opts:        &options.Options{},
		ProjectPath: project,
	}
	cmd := PkgInstallCommand()
	return cmd.Run(rt, args)
}

func TestPkgInstall_DryRun_ReportsPlanWithoutWriting(t *testing.T) {
	project := t.TempDir()
	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"9.9.9"}`,
		"package/Runtime/x.cs": "content",
	})

	res, err := runPkgInstall(t, project, "--source", tgz, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run should succeed: %v", err)
	}
	plan, ok := res.(*pkgPlan)
	if !ok {
		t.Fatalf("dry-run should return the plan, got %T", res)
	}
	if len(plan.Added) != 2 {
		t.Fatalf("plan should classify 2 added files, got %v", plan.Added)
	}
	// Nothing written.
	if _, err := os.Stat(filepath.Join(project, "Packages", "com.unity.pipeline", "package.json")); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write anything to the target")
	}
}

func TestPkgInstall_ReadOnlyTarget_RefusesWithGuidance(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only permission tests are meaningless as root")
	}
	project := t.TempDir()
	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"9.9.9"}`,
	})

	// Pre-existing install with a read-only file, like a P4-managed workspace
	// keeps unchanged files.
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(target, "package.json")
	if err := os.WriteFile(existing, []byte(`{"version":"0.0.1"}`), 0o444); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(existing, 0o644) }()

	_, err := runPkgInstall(t, project, "--source", tgz)
	if err == nil {
		t.Fatal("install into a read-only target must refuse")
	}
	ue, ok := err.(*protocol.UpipeError)
	if !ok {
		t.Fatalf("error should be a UpipeError, got %T: %v", err, err)
	}
	if ue.Kind != "ReadOnlyTarget" {
		t.Errorf("error kind should be ReadOnlyTarget, got %q", ue.Kind)
	}
	if !strings.Contains(ue.Message, "p4") || !strings.Contains(ue.Message, "read-only") {
		t.Errorf("refusal should carry P4 guidance, got: %s", ue.Message)
	}
	// The read-only file must be untouched.
	data, rerr := os.ReadFile(existing)
	if rerr != nil || string(data) != `{"version":"0.0.1"}` {
		t.Error("refused install must not modify the read-only file")
	}
}

func TestPkgInstall_WritesAndRegistersProject(t *testing.T) {
	project := t.TempDir()
	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"9.9.9"}`,
		"package/Runtime/x.cs": "content",
	})

	// Redirect the instance registry to a sandbox file.
	registry := filepath.Join(t.TempDir(), "projects.txt")
	old := knownProjectsFile
	knownProjectsFile = func() string { return registry }
	defer func() { knownProjectsFile = old }()

	res, err := runPkgInstall(t, project, "--source", tgz)
	if err != nil {
		t.Fatalf("install should succeed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("install should return a summary map, got %T", res)
	}
	if m["written"].(int) != 2 {
		t.Errorf("summary should report 2 written files, got %v", m["written"])
	}
	// Files actually on disk.
	if _, err := os.Stat(filepath.Join(project, "Packages", "com.unity.pipeline", "Runtime", "x.cs")); err != nil {
		t.Error("installed file missing on disk")
	}
	// Project registered for `upipe instances`.
	reg, rerr := os.ReadFile(registry)
	if rerr != nil {
		t.Fatalf("read registry: %v", rerr)
	}
	if !strings.Contains(string(reg), filepath.ToSlash(project)) && !strings.Contains(string(reg), project) {
		t.Errorf("project path should be registered, registry content: %q", string(reg))
	}
}

// TestPkgInstall_ReportsRemovedWithoutDeletingByDefault locks the safe
// default for item 9: a stale target file absent from the tgz is reported
// under "removed" but left on disk unless --prune is passed.
func TestPkgInstall_ReportsRemovedWithoutDeletingByDefault(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(filepath.Join(target, "Runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "Runtime", "stale.cs"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := filepath.Join(t.TempDir(), "projects.txt")
	old := knownProjectsFile
	knownProjectsFile = func() string { return registry }
	defer func() { knownProjectsFile = old }()

	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"9.9.9"}`,
		// Runtime/stale.cs intentionally absent.
	})

	res, err := runPkgInstall(t, project, "--source", tgz)
	if err != nil {
		t.Fatalf("install should succeed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	removed, ok := m["removed"].([]string)
	if !ok || len(removed) != 1 || removed[0] != "Runtime/stale.cs" {
		t.Fatalf("expected removed=[Runtime/stale.cs], got %v", m["removed"])
	}
	if _, present := m["removedNote"]; !present {
		t.Error("expected a removedNote hint about --prune when removed files exist and --prune was not passed")
	}
	if _, present := m["deleted"]; present {
		t.Error("deleted key should not be present without --prune")
	}
	// The stale file must still exist on disk — default is report-only.
	if _, err := os.Stat(filepath.Join(target, "Runtime", "stale.cs")); err != nil {
		t.Fatal("stale.cs should NOT have been deleted without --prune")
	}
}

// TestPkgInstall_PruneDeletesRemovedFiles locks the opt-in destructive path.
func TestPkgInstall_PruneDeletesRemovedFiles(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(filepath.Join(target, "Runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "Runtime", "stale.cs"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	registry := filepath.Join(t.TempDir(), "projects.txt")
	old := knownProjectsFile
	knownProjectsFile = func() string { return registry }
	defer func() { knownProjectsFile = old }()

	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"9.9.9"}`,
	})

	res, err := runPkgInstall(t, project, "--source", tgz, "--prune")
	if err != nil {
		t.Fatalf("install should succeed: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	deleted, ok := m["deleted"].([]string)
	if !ok || len(deleted) != 1 || deleted[0] != "Runtime/stale.cs" {
		t.Fatalf("expected deleted=[Runtime/stale.cs], got %v", m["deleted"])
	}
	if _, err := os.Stat(filepath.Join(target, "Runtime", "stale.cs")); !os.IsNotExist(err) {
		t.Fatal("stale.cs should have been deleted with --prune")
	}
}

// TestPkgInstall_DryRunReportsRemovedWithoutDeleting locks that --dry-run
// (no --prune) surfaces the removed list via the plan without touching disk,
// same as the non-dry-run default.
func TestPkgInstall_DryRunReportsRemovedWithoutDeleting(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.cs"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	tgz := buildTgz(t, map[string]string{"package/package.json": `{"version":"9.9.9"}`})

	res, err := runPkgInstall(t, project, "--source", tgz, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run should succeed: %v", err)
	}
	plan, ok := res.(*pkgPlan)
	if !ok {
		t.Fatalf("dry-run should return the plan, got %T", res)
	}
	if len(plan.Removed) != 1 || plan.Removed[0] != "stale.cs" {
		t.Fatalf("expected plan.Removed=[stale.cs], got %v", plan.Removed)
	}
	if _, err := os.Stat(filepath.Join(target, "stale.cs")); err != nil {
		t.Fatal("dry-run must not delete anything")
	}
}
