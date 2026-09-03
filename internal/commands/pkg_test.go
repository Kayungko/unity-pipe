package commands

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildTgz writes a tgz whose entries are the given name->content map.
func buildTgz(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.tgz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractPlanClassifiesEntries(t *testing.T) {
	target := t.TempDir()
	// Pre-existing files: one identical (unchanged), one different (updated).
	_ = os.MkdirAll(filepath.Join(target, "Runtime"), 0o755)
	_ = os.WriteFile(filepath.Join(target, "Runtime", "same.cs"), []byte("same"), 0o644)
	_ = os.WriteFile(filepath.Join(target, "Runtime", "diff.cs"), []byte("old"), 0o644)

	tgz := buildTgz(t, map[string]string{
		"package/package.json":     `{"version":"0.5.0-2022.1"}`,
		"package/Runtime/same.cs":  "same",
		"package/Runtime/diff.cs":  "new",
		"package/Runtime/new.cs":   "brand new",
		"package/Tests/skip.cs":    "tests are stripped",
		"package/some/dir/deep.cs": "nested",
		"not-package-prefix.cs":    "trimmed prefix lands at root",
	})

	plan, err := extractPlan(tgz, target)
	if err != nil {
		t.Fatalf("extractPlan: %v", err)
	}
	expectSet(t, "added", plan.Added, []string{"package.json", "Runtime/new.cs", "some/dir/deep.cs", "not-package-prefix.cs"})
	expectSet(t, "updated", plan.Updated, []string{"Runtime/diff.cs"})
	expectSet(t, "unchanged", plan.Unchanged, []string{"Runtime/same.cs"})
	if _, ok := plan.entries["Tests/skip.cs"]; ok {
		t.Fatal("Tests/ entry not stripped")
	}
}

// TestExtractPlanDetectsRemovedFiles locks the "removed" classification: a
// target file that exists on disk but is absent from the tgz's entry set
// (an upstream rename/drop) must be reported without being deleted by
// extractPlan itself — deletion is a separate, opt-in step (deleteRemoved).
func TestExtractPlanDetectsRemovedFiles(t *testing.T) {
	target := t.TempDir()
	_ = os.MkdirAll(filepath.Join(target, "Runtime"), 0o755)
	_ = os.WriteFile(filepath.Join(target, "Runtime", "gone.cs"), []byte("stale"), 0o644)
	_ = os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"version":"0.1"}`), 0o644)

	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"0.2"}`,
		// Runtime/gone.cs intentionally absent — upstream dropped it.
	})

	plan, err := extractPlan(tgz, target)
	if err != nil {
		t.Fatalf("extractPlan: %v", err)
	}
	expectSet(t, "removed", plan.Removed, []string{"Runtime/gone.cs"})
	// It must still be on disk — extractPlan only classifies, never deletes.
	if _, err := os.Stat(filepath.Join(target, "Runtime", "gone.cs")); err != nil {
		t.Fatal("extractPlan must not delete removed files itself")
	}
}

// TestExtractPlanFreshInstallHasNoRemoved locks that a fresh (nonexistent)
// target reports zero removed files — there is nothing to compare against.
func TestExtractPlanFreshInstallHasNoRemoved(t *testing.T) {
	target := filepath.Join(t.TempDir(), "does-not-exist-yet")
	tgz := buildTgz(t, map[string]string{"package/package.json": `{"version":"1"}`})
	plan, err := extractPlan(tgz, target)
	if err != nil {
		t.Fatalf("extractPlan: %v", err)
	}
	if len(plan.Removed) != 0 {
		t.Fatalf("fresh install should report no removed files, got %v", plan.Removed)
	}
}

// TestDeleteRemovedDeletesListedFiles locks deleteRemoved's contract: it
// deletes exactly the files named in plan.Removed and reports them back.
func TestDeleteRemovedDeletesListedFiles(t *testing.T) {
	target := t.TempDir()
	_ = os.MkdirAll(filepath.Join(target, "Runtime"), 0o755)
	_ = os.WriteFile(filepath.Join(target, "Runtime", "gone.cs"), []byte("stale"), 0o644)
	_ = os.WriteFile(filepath.Join(target, "keep.cs"), []byte("keep"), 0o644)

	plan := &pkgPlan{Target: target, Removed: []string{"Runtime/gone.cs"}}
	deleted, err := deleteRemoved(plan)
	if err != nil {
		t.Fatalf("deleteRemoved: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "Runtime/gone.cs" {
		t.Fatalf("expected Runtime/gone.cs deleted, got %v", deleted)
	}
	if _, err := os.Stat(filepath.Join(target, "Runtime", "gone.cs")); !os.IsNotExist(err) {
		t.Fatal("Runtime/gone.cs should have been deleted")
	}
	if _, err := os.Stat(filepath.Join(target, "keep.cs")); err != nil {
		t.Fatal("keep.cs (not in Removed) must survive")
	}
}

// TestDeleteRemovedReportsFirstErrorButContinues locks the "best effort,
// report first failure" contract: one missing/undeletable entry does not
// abort deletion of the rest.
func TestDeleteRemovedReportsFirstErrorButContinues(t *testing.T) {
	target := t.TempDir()
	_ = os.WriteFile(filepath.Join(target, "real.cs"), []byte("x"), 0o644)

	plan := &pkgPlan{Target: target, Removed: []string{"missing.cs", "real.cs"}}
	deleted, err := deleteRemoved(plan)
	if err == nil {
		t.Fatal("expected an error for the missing entry")
	}
	if len(deleted) != 1 || deleted[0] != "real.cs" {
		t.Fatalf("real.cs should still have been deleted despite the earlier failure, got %v", deleted)
	}
}

func TestExtractPlanRejectsTraversal(t *testing.T) {
	target := t.TempDir()
	for _, evil := range []string{
		"package/../../evil.cs",
		"package/../escape.cs",
		"package//../../../abs.cs",
		"package/C:/Windows/evil.cs",
		"package/C:autoexec.cs",
		"package//etc/passwd",
	} {
		tgz := buildTgz(t, map[string]string{evil: "payload"})
		if _, err := extractPlan(tgz, target); err == nil {
			t.Fatalf("traversal entry %q accepted", evil)
		} else if !strings.Contains(err.Error(), "escapes") {
			t.Fatalf("traversal entry %q: unexpected error %v", evil, err)
		}
	}
}

func TestExtractPlanRejectsEmptyAndGarbage(t *testing.T) {
	target := t.TempDir()

	// Only Tests/ entries -> no installable entries.
	tgz := buildTgz(t, map[string]string{"package/Tests/x.cs": "x"})
	if _, err := extractPlan(tgz, target); err == nil || !strings.Contains(err.Error(), "no package/") {
		t.Fatalf("tests-only tgz should be rejected, got %v", err)
	}

	// Not a gzip at all.
	plain := filepath.Join(t.TempDir(), "plain.tgz")
	if err := os.WriteFile(plain, []byte("this is not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := extractPlan(plain, target); err == nil {
		t.Fatal("non-gzip source accepted")
	}

	// Missing file.
	if _, err := extractPlan(filepath.Join(t.TempDir(), "nope.tgz"), target); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestApplyPlanWritesAndIsIdempotent(t *testing.T) {
	target := t.TempDir()
	tgz := buildTgz(t, map[string]string{
		"package/package.json": `{"version":"1"}`,
		"package/Runtime/a.cs": "alpha",
	})
	plan, err := extractPlan(tgz, target)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := applyPlan(plan); err != nil || n != 2 {
		t.Fatalf("applyPlan: n=%d err=%v", n, err)
	}
	for _, p := range []string{"package.json", filepath.Join("Runtime", "a.cs")} {
		if _, err := os.Stat(filepath.Join(target, p)); err != nil {
			t.Fatalf("expected %s written: %v", p, err)
		}
	}
	// Re-extract against the applied state: everything unchanged.
	plan2, err := extractPlan(tgz, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan2.Updated) != 0 || len(plan2.Added) != 0 || len(plan2.Unchanged) != 2 {
		t.Fatalf("re-install not a no-op: +%d ~%d =%d", len(plan2.Added), len(plan2.Updated), len(plan2.Unchanged))
	}
}

func TestIsUnsafeEntryName(t *testing.T) {
	safe := []string{"package.json", "Runtime/a.cs", "dir/sub/x.dll", "Editor/Plugins/lib.dll"}
	for _, n := range safe {
		if isUnsafeEntryName(n) {
			t.Errorf("%q flagged unsafe", n)
		}
	}
	unsafe := []string{"../x", "/etc/x", `\windows\x`, "C:\\x", "C:x", "a/../../x", "../../x"}
	for _, n := range unsafe {
		if !isUnsafeEntryName(n) {
			t.Errorf("%q not flagged unsafe", n)
		}
	}
}

func expectSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	g := map[string]bool{}
	for _, s := range got {
		g[s] = true
	}
	for _, s := range want {
		if !g[s] {
			t.Errorf("%s: missing %q (got %v)", label, s, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%s: expected %d entries, got %d: %v", label, len(want), len(got), got)
	}
}

// TestInstalledPkgVersion locks the version-detection contract PkgStatusCommand
// relies on: read Packages/com.unity.pipeline/package.json and decode
// "version".
func TestInstalledPkgVersion(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte(`{"name":"com.unity.pipeline","version":"0.6.0-2022.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := installedPkgVersion(project)
	if err != nil {
		t.Fatalf("installedPkgVersion: %v", err)
	}
	if got != "0.6.0-2022.1" {
		t.Fatalf("got %q, want 0.6.0-2022.1", got)
	}
}

func TestInstalledPkgVersionMissingPackage(t *testing.T) {
	project := t.TempDir() // no Packages/com.unity.pipeline at all
	if _, err := installedPkgVersion(project); err == nil {
		t.Fatal("expected an error when the package is not installed")
	}
}

func TestInstalledPkgVersionCorruptJSON(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "Packages", "com.unity.pipeline")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "package.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installedPkgVersion(project); err == nil {
		t.Fatal("expected an error for corrupt package.json")
	}
}
