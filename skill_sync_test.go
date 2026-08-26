package lint_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// skill_sync_test.go keeps the vendored copy of the unity-pipeline SKILL.md
// (inside package/com.unity.pipeline/.claude/skills — what sessions touching
// the package directory actually discover) in lockstep with the harness
// original (harness/skills — the source that plugin install distributes).
//
// Before this lock the vendored copy was the upstream original describing the
// closed-source `unity` CLI contract, sitting BESIDE the real skill in every
// package-directory session and contradicting it (wrong CLI, wrong exit-code
// story) — a channel-selection hazard, not a cosmetic drift.
//
// package/upstream/.claude/skills is deliberately NOT locked: it is the
// read-only upstream snapshot.
func TestVendoredSkillMatchesHarness(t *testing.T) {
	repo := findRepoRoot(t)
	harness := filepath.Join(repo, "harness", "skills", "unity-pipeline", "SKILL.md")
	vendored := filepath.Join(repo, "package", "com.unity.pipeline", ".claude", "skills", "unity-pipeline", "SKILL.md")

	h, err := os.ReadFile(harness)
	if err != nil {
		t.Fatalf("read harness SKILL.md: %v", err)
	}
	v, err := os.ReadFile(vendored)
	if err != nil {
		t.Fatalf("read vendored SKILL.md: %v", err)
	}
	if !bytes.Equal(h, v) {
		t.Error("vendored SKILL.md (package/com.unity.pipeline/.claude/skills) differs from the harness original — " +
			"copy harness/skills/unity-pipeline/SKILL.md over it. A stale vendored copy competes with the real " +
			"skill in package-directory sessions and describes the wrong CLI contract.")
	}

	// The vendored copy must describe upipe, not the closed-source unity CLI.
	if bytes.Contains(v, []byte("Assumes the `unity` CLI is on PATH")) {
		t.Error("vendored SKILL.md still describes the closed-source unity CLI contract (upstream original) — " +
			"overwrite it with the harness version.")
	}
}
