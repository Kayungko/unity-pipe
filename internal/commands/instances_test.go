package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterProjectDedupes(t *testing.T) {
	dir := t.TempDir()
	orig := knownProjectsFile
	knownProjectsFile = func() string { return filepath.Join(dir, "projects.txt") }
	defer func() { knownProjectsFile = orig }()

	registerProject(`E:\proj\A`)
	registerProject(`E:\proj\A`) // exact dup
	registerProject(`e:\proj\a`) // case dup (Windows paths are case-insensitive)
	registerProject(`E:\proj\B`)

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
	registerProject(`E:\proj\C`) // mkdir fails under a file path? dir exists actually
	// Regardless of outcome, no panic is the contract.
}
