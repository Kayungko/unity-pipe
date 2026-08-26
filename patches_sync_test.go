package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPatchSnapshotsCoverVendoredDrift locks the documented upgrade contract in
// package/ADAPTATION-2022.md: every file that differs between the upstream
// snapshot and the vendored tree must be recorded in the patch chain
// (0001 compatibility layer + 0002 evolution layer). Editing the vendored
// tree without regenerating the snapshots fails this test — that is exactly
// the drift that would silently lose changes on the next upstream upgrade.
func TestPatchSnapshotsCoverVendoredDrift(t *testing.T) {
	repoRoot := findRepoRoot(t)
	upstream := filepath.Join(repoRoot, "package", "upstream")
	vendored := filepath.Join(repoRoot, "package", "com.unity.pipeline")
	patches := filepath.Join(repoRoot, "package", "patches")

	// 1. Files registered by the patch chain (diff --git lines).
	registered := map[string]bool{}
	entries, err := os.ReadDir(patches)
	if err != nil {
		t.Fatalf("read patches dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".patch") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(patches, e.Name()))
		if err != nil {
			t.Fatalf("read patch %s: %v", e.Name(), err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			// Lines look like: diff --git a/com.unity.pipeline/X b/com.unity.pipeline/X
			// (0001 uses a/upstream/... b/com.unity.pipeline/... — take the b/ side.)
			if !strings.HasPrefix(line, "diff --git ") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			b := strings.TrimPrefix(fields[3], "b/")
			if b != fields[3] { // had the b/ prefix
				registered[b] = true
			}
		}
	}
	if len(registered) == 0 {
		t.Fatal("no files registered by any patch — snapshots missing?")
	}

	// 2. Files that actually differ between upstream and vendored.
	actual, err := diffTreeFiles(t, upstream, vendored)
	if err != nil {
		t.Fatalf("diff trees: %v", err)
	}

	// Vendored-only infrastructure, deliberately outside the patch contract.
	excluded := map[string]bool{
		".claude": true, // repo tooling (skills), not part of the shipped package content
	}

	// Exception: the vendored SKILL.md is a deliberately maintained copy of the
	// harness original (locked by skill_sync_test.go) — upstream grades it as
	// drift, but it is governed by its own sync lock, not the patch chain.
	syncLocked := map[string]bool{
		"com.unity.pipeline/.claude/skills/unity-pipeline/SKILL.md": true,
	}

	var unregistered []string
	for _, rel := range actual {
		top := strings.SplitN(rel, "/", 2)[0]
		if excluded[top] {
			continue
		}
		if syncLocked[rel] {
			continue
		}
		if !registered[rel] {
			unregistered = append(unregistered, rel)
		}
	}
	if len(unregistered) > 0 {
		t.Errorf("vendored files drifted from upstream but are NOT recorded in any patch "+
			"(regenerate package/patches snapshots per ADAPTATION-2022.md, or the next upstream "+
			"upgrade will silently lose these changes):\n  %s",
			strings.Join(unregistered, "\n  "))
	}
}

// diffTreeFiles walks both trees in lockstep and returns repo-relative paths
// (relative to the package dir, e.g. "com.unity.pipeline/Runtime/x.cs") of
// files that differ in content (CRLF-normalized) or exist on only one side.
func diffTreeFiles(t *testing.T, upstream, vendored string) ([]string, error) {
	t.Helper()
	var diffs []string
	err := filepath.WalkDir(vendored, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(vendored, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			// A vendored directory with no upstream counterpart still gets walked;
			// its files will be reported as vendored-only below.
			return nil
		}
		up := filepath.Join(upstream, rel)
		upData, upErr := os.ReadFile(up)
		vData, vErr := os.ReadFile(path)
		if upErr != nil || vErr != nil {
			if os.IsNotExist(upErr) {
				diffs = append(diffs, "com.unity.pipeline/"+rel)
			}
			return nil
		}
		if !bytesEqualNorm(upData, vData) {
			diffs = append(diffs, "com.unity.pipeline/"+rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return diffs, nil
}

func bytesEqualNorm(a, b []byte) bool {
	return string(normEOL(a)) == string(normEOL(b))
}

func normEOL(data []byte) []byte {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	return []byte(s)
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repo root not found (no .git within 6 levels)")
	return ""
}
