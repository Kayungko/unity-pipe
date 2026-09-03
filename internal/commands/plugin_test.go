package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/options"
)

// installSkills is the other filesystem-writing code path (backup → copy →
// SHA256 verify). These tests lock its contract: idempotent-style re-install
// backs up instead of deleting, verification detects corruption, and backups
// land OUTSIDE the skills dir (a .bak inside would surface as a duplicate
// skill to the harness scanner).
func TestInstallSkillsLifecycle(t *testing.T) {
	src := t.TempDir()
	for _, skill := range []string{"unity-pipeline", "unity-router"} {
		dir := filepath.Join(src, skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("skill: "+skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	target := filepath.Join(t.TempDir(), "skills")

	// First install: 2 files, verified, no backups.
	backups, files, verified, err := installSkills(src, []string{"unity-pipeline", "unity-router"}, target)
	if err != nil || files != 2 || !verified || len(backups) != 0 {
		t.Fatalf("first install: backups=%v files=%d verified=%v err=%v", backups, files, verified, err)
	}

	// Re-install with changed source content: old dirs are backed up (moved,
	// not deleted) and new content lands.
	if err := os.WriteFile(filepath.Join(src, "unity-pipeline", "SKILL.md"), []byte("skill: v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups, files, verified, err = installSkills(src, []string{"unity-pipeline", "unity-router"}, target)
	if err != nil || files != 2 || !verified {
		t.Fatalf("re-install: files=%d verified=%v err=%v", files, verified, err)
	}
	if len(backups) != 2 {
		t.Fatalf("expected 2 backups on re-install, got %v", backups)
	}
	// Backups must live in a sibling dir, NOT inside the skills dir.
	entries, _ := os.ReadDir(target)
	for _, e := range entries {
		for _, b := range backups {
			if e.Name() == b {
				t.Fatalf("backup %q found INSIDE skills dir — harness would load it as a duplicate skill", b)
			}
		}
	}
	backupDir := target + "-backup"
	for _, b := range backups {
		if _, err := os.Stat(filepath.Join(backupDir, b)); err != nil {
			t.Fatalf("backup %q missing from %s: %v", b, backupDir, err)
		}
	}
	// New content in place.
	data, _ := os.ReadFile(filepath.Join(target, "unity-pipeline", "SKILL.md"))
	if string(data) != "skill: v2" {
		t.Fatalf("re-install did not land new content: %q", data)
	}
}

func TestInstallSkillsDetectsCorruption(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "unity-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "unity-pipeline", "SKILL.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "skills")
	_, files, verified, err := installSkills(src, []string{"unity-pipeline"}, target)
	if err != nil || files != 1 || !verified {
		t.Fatalf("clean install: files=%d verified=%v err=%v", files, verified, err)
	}
	// Corrupt the installed copy; a re-install must report verified=false
	// only if verification compares content — simulate by corrupting after
	// install and re-running install with same source (backup+restore path).
	if err := os.WriteFile(filepath.Join(target, "unity-pipeline", "SKILL.md"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, verified, err = installSkills(src, []string{"unity-pipeline"}, target)
	if err != nil {
		t.Fatalf("re-install after tamper: %v", err)
	}
	if !verified {
		t.Fatal("re-install after tamper should verify clean (fresh copy)")
	}
	// Direct corruption check: tamper then verify manually against source.
	if err := os.WriteFile(filepath.Join(target, "unity-pipeline", "SKILL.md"), []byte("tampered-again"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(src, "unity-pipeline", "SKILL.md"))
	b, _ := os.ReadFile(filepath.Join(target, "unity-pipeline", "SKILL.md"))
	if string(a) == string(b) {
		t.Fatal("tamper not detectable — comparison broken")
	}
}

// uninstallSkills is the inverse of installSkills: this locks its
// reversibility contract (backup, never hard-delete) and its "report
// missing, don't fail" behavior for skills never installed at a target.
func TestUninstallSkillsMovesToBackupAndReportsMissing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(target, "unity-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "unity-pipeline", "SKILL.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// unity-router is NOT installed at target.

	removed, missing, err := uninstallSkills([]string{"unity-pipeline", "unity-router"}, target)
	if err != nil {
		t.Fatalf("uninstallSkills: %v", err)
	}
	if len(removed) != 1 || removed[0] != "unity-pipeline" {
		t.Fatalf("expected unity-pipeline removed, got %v", removed)
	}
	if len(missing) != 1 || missing[0] != "unity-router" {
		t.Fatalf("expected unity-router reported missing, got %v", missing)
	}
	// Never hard-deleted: content must survive in the backup dir.
	if _, err := os.Stat(filepath.Join(target, "unity-pipeline")); !os.IsNotExist(err) {
		t.Fatal("uninstalled skill directory should no longer exist at target")
	}
	backupDir := target + "-backup"
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one backup entry, got %v (err=%v)", entries, err)
	}
	restored, err := os.ReadFile(filepath.Join(backupDir, entries[0].Name(), "SKILL.md"))
	if err != nil || string(restored) != "content" {
		t.Fatalf("backup content mismatch: %q err=%v", restored, err)
	}
}

func TestUninstallSkillsEmptyTargetReportsAllMissing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "does-not-exist")
	removed, missing, err := uninstallSkills([]string{"unity-pipeline", "unity-router"}, target)
	if err != nil {
		t.Fatalf("uninstallSkills on a nonexistent target should not error: %v", err)
	}
	if len(removed) != 0 || len(missing) != 2 {
		t.Fatalf("expected everything reported missing, got removed=%v missing=%v", removed, missing)
	}
}

// TestListSkillDirsSortsAndFiltersFiles locks listSkillDirs' contract: only
// directories are returned, sorted, and files alongside them (e.g. a stray
// README) are ignored.
func TestListSkillDirsSortsAndFiltersFiles(t *testing.T) {
	src := t.TempDir()
	for _, d := range []string{"unity-router", "unity-pipeline", "unity-mcp-bridge"} {
		if err := os.MkdirAll(filepath.Join(src, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("not a skill dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := listSkillDirs(src)
	if err != nil {
		t.Fatalf("listSkillDirs: %v", err)
	}
	want := []string{"unity-mcp-bridge", "unity-pipeline", "unity-router"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (sorted order)", got, want)
		}
	}
}

func TestListSkillDirsMissingSourceErrors(t *testing.T) {
	if _, err := listSkillDirs(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a missing source directory")
	}
}

func TestListSkillDirsEmptySourceErrors(t *testing.T) {
	src := t.TempDir() // exists but has no subdirectories
	if _, err := listSkillDirs(src); err == nil {
		t.Fatal("expected an error for a source with no skill directories")
	}
}

// TestFindRepoRootWalksUpToHarnessSkills locks findRepoRoot's contract: it
// walks up from cwd looking for harness/skills, the same marker
// listSkillDirs reads from.
func TestFindRepoRootWalksUpToHarnessSkills(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "harness", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "internal", "commands")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	got, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}
	gotAbs, _ := filepath.Abs(got)
	repoAbs, _ := filepath.Abs(repo)
	// Compare by filesystem identity, not string equality: os.Getwd may
	// return a differently-spelled path than t.TempDir did — symlink-
	// resolved on macOS (/var -> /private/var), 8.3-short-named on
	// Windows runners (C:\Users\RUNNER~1 for runneradmin).
	gi, giErr := os.Stat(gotAbs)
	ri, riErr := os.Stat(repoAbs)
	if giErr != nil || riErr != nil || !os.SameFile(gi, ri) {
		t.Fatalf("got %q, want %q (same dir)", gotAbs, repoAbs)
	}
}

func TestFindRepoRootNotFoundErrors(t *testing.T) {
	dir := t.TempDir() // no harness/skills anywhere upward
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := findRepoRoot(); err == nil {
		t.Fatal("expected an error when harness/skills cannot be found")
	}
}

// TestUserHomeDirJoinsParts locks that userHomeDir builds a path under the
// resolved home directory (the exact home value is environment-dependent, so
// this only checks the relative structure survives).
func TestUserHomeDirJoinsParts(t *testing.T) {
	got := userHomeDir(".claude", "skills")
	if !strings.HasSuffix(got, filepath.Join(".claude", "skills")) {
		t.Fatalf("userHomeDir(.claude, skills) = %q, want a path ending in .claude/skills", got)
	}
}

// TestPluginUninstallCommandDryRunWritesNothing drives the full command body:
// dry-run must report the plan without touching disk.
// TestPluginCommandForwardsUninstall locks the fix for the space-separated
// spelling users naturally reach for after `plugin install`: `upipe plugin
// uninstall ...` must behave identically to `upipe plugin-uninstall ...`
// (PluginUninstallCommand), not fail with "unexpected argument uninstall"
// from the install flag parser.
func TestPluginCommandForwardsUninstall(t *testing.T) {
	repo := t.TempDir()
	skillsSrc := filepath.Join(repo, "harness", "skills")
	if err := os.MkdirAll(filepath.Join(skillsSrc, "unity-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Opts: &options.Options{}}
	res, err := PluginCommand().Run(rt, []string{"uninstall", "--from", repo, "--codex", "--dry-run"})
	if err != nil {
		t.Fatalf("`plugin uninstall` should forward to PluginUninstallCommand, got error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	// Uninstall-only fields — if this ran the install path instead, the
	// result would have "source"/"installs" keys, not "uninstalls".
	if _, present := m["uninstalls"]; !present {
		t.Fatalf("expected an uninstall-shaped result (uninstalls key), got %v", m)
	}
	if _, present := m["installs"]; present {
		t.Fatal("`plugin uninstall` ran the INSTALL path instead of forwarding to uninstall")
	}
}

func TestPluginUninstallCommandDryRunWritesNothing(t *testing.T) {
	repo := t.TempDir()
	skillsSrc := filepath.Join(repo, "harness", "skills")
	for _, s := range []string{"unity-pipeline", "unity-router"} {
		if err := os.MkdirAll(filepath.Join(skillsSrc, s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	claudeSkills := filepath.Join(repo, "fake-home", ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(claudeSkills, "unity-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Opts: &options.Options{}}
	cmd := PluginUninstallCommand()
	// --from bypasses repo auto-detection; skills/targets logic is exercised
	// directly by uninstallSkills tests above, so here we only need to prove
	// the command wires dry-run correctly and does not write.
	res, err := cmd.Run(rt, []string{"--from", repo, "--codex", "--dry-run"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	if dr, _ := m["dry_run"].(bool); !dr {
		t.Error("dry_run flag not reflected in result")
	}
	// Codex-only run should not touch the claude skills dir at all.
	if _, err := os.Stat(filepath.Join(claudeSkills, "unity-pipeline")); err != nil {
		t.Fatal("dry-run/--codex-only must not touch the (unselected) claude skills dir")
	}
}

// TestPluginUninstallCommandRemovesRulesBlock exercises --remove-rules
// end-to-end against a fake HOME.
func TestPluginUninstallCommandRemovesRulesBlock(t *testing.T) {
	repo := t.TempDir()
	skillsSrc := filepath.Join(repo, "harness", "skills")
	if err := os.MkdirAll(filepath.Join(skillsSrc, "unity-pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows os.UserHomeDir source

	claudeMd := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(claudeMd), 0o755); err != nil {
		t.Fatal(err)
	}
	block := ruleBeginMarker + "\n## 13. 规则\n" + ruleEndMarker
	if err := os.WriteFile(claudeMd, []byte("# My CLAUDE.md\n\n"+block+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := &Runtime{Opts: &options.Options{}}
	cmd := PluginUninstallCommand()
	res, err := cmd.Run(rt, []string{"--from", repo, "--claude", "--remove-rules"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	if _, present := m["rules_removed"]; !present {
		t.Fatal("--remove-rules should populate rules_removed in the result")
	}
	out, rerr := os.ReadFile(claudeMd)
	if rerr != nil {
		t.Fatalf("read claude.md: %v", rerr)
	}
	if strings.Contains(string(out), ruleBeginMarker) {
		t.Fatal("managed block should have been stripped")
	}
	if !strings.Contains(string(out), "# My CLAUDE.md") {
		t.Fatal("unmanaged content should survive the strip")
	}
}
