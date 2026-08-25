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
