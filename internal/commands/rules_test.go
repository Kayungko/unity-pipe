package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractFencedMarkdown(t *testing.T) {
	tmpl := "# 说明\n\n> 贴入文档\n\n```markdown\n## 13. 规则\n\n- 条款一\n```\n"
	got, err := extractFencedMarkdown(tmpl, "test")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.TrimSpace(got) != "## 13. 规则\n\n- 条款一" {
		t.Fatalf("unexpected body: %q", got)
	}

	if _, err := extractFencedMarkdown("no fence here", "test"); err == nil {
		t.Fatal("expected error for missing fence")
	}
	if _, err := extractFencedMarkdown("```markdown\nunterminated", "test"); err == nil {
		t.Fatal("expected error for unterminated fence")
	}
}

func TestExtractRuleBodyRealTemplates(t *testing.T) {
	// The shipped templates must remain machine-parseable: if someone edits
	// the prose and breaks the fenced block, --apply-rules dies at runtime.
	for _, name := range []string{"claude.md", "agents.md"} {
		path := filepath.Join("..", "..", "harness", "rules", name)
		if _, err := extractRuleBody(path); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRenderRule(t *testing.T) {
	body := "repo {{UNITY_PIPE_REPO}} bin {{UPIPE_BIN}} guide {{UNITY_GUIDE}} wl {{CO_USE_WHITELIST}} depot {{CO_USE_DEPOT}}"
	got := renderRule(body, ruleVars{Repo: "E:\\unity-pipe", Bin: "C:\\bin\\upipe.exe", Guide: "G.md"})
	if !strings.Contains(got, "repo E:\\unity-pipe") || !strings.Contains(got, "bin C:\\bin\\upipe.exe") || !strings.Contains(got, "guide G.md") {
		t.Fatalf("placeholder substitution broken: %q", got)
	}
	// Empty co-use inputs fall back to the pipeline-only safe default.
	if !strings.Contains(got, "pipeline-only") || !strings.Contains(got, "depot N/A") {
		t.Fatalf("safe default missing: %q", got)
	}
	// Explicit values are honored verbatim.
	got = renderRule(body, ruleVars{Whitelist: "E:\\P4\\SGAME_*", Depot: "//sgproj/trunk"})
	if !strings.Contains(got, "wl E:\\P4\\SGAME_*") || !strings.Contains(got, "depot //sgproj/trunk") {
		t.Fatalf("explicit co-use values not honored: %q", got)
	}
}

func TestApplyRulesDocLifecycle(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "CLAUDE.md")
	body := "## 13. 规则内容"

	// 1. Fresh document -> created, no backup.
	status, backup, err := applyRulesDoc(doc, body, false)
	if err != nil || status != "created" || backup != "" {
		t.Fatalf("create: status=%q backup=%q err=%v", status, backup, err)
	}
	created, _ := os.ReadFile(doc)
	if !strings.Contains(string(created), ruleBeginMarker) || !strings.Contains(string(created), body) {
		t.Fatal("created doc missing managed block")
	}

	// 2. Re-run -> already-managed, file untouched.
	before, _ := os.ReadFile(doc)
	status, _, err = applyRulesDoc(doc, body, false)
	if err != nil || status != "already-managed" {
		t.Fatalf("re-run: status=%q err=%v", status, err)
	}
	after, _ := os.ReadFile(doc)
	if string(before) != string(after) {
		t.Fatal("re-run mutated an already-managed doc")
	}

	// 3. Existing unmanaged doc -> appended with backup.
	doc2 := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(doc2, []byte("existing content"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, backup, err = applyRulesDoc(doc2, body, false)
	if err != nil || status != "appended" || backup == "" {
		t.Fatalf("append: status=%q backup=%q err=%v", status, backup, err)
	}
	bak, _ := os.ReadFile(filepath.Join(dir, backup))
	if string(bak) != "existing content" {
		t.Fatal("backup does not preserve the original content")
	}
	out, _ := os.ReadFile(doc2)
	if !strings.HasPrefix(string(out), "existing content") || !strings.Contains(string(out), ruleEndMarker) {
		t.Fatal("appended doc malformed")
	}

	// 4. Dry-run writes nothing.
	doc3 := filepath.Join(dir, "NEW.md")
	status, _, err = applyRulesDoc(doc3, body, true)
	if err != nil || status != "dry-run(create)" {
		t.Fatalf("dry-run create: status=%q err=%v", status, err)
	}
	if _, serr := os.Stat(doc3); !os.IsNotExist(serr) {
		t.Fatal("dry-run created a file")
	}
}

func TestUpgradeRulesDocLifecycle(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "CLAUDE.md")
	v1 := "## 13. 规则\n- 条款A\n- 白名单：`E:\\P4\\SGAME_*`"

	// 1. No managed block -> missing-block.
	status, _, _, err := upgradeRulesDoc(doc, v1, false)
	if err != nil || status != "missing-block" {
		t.Fatalf("no block: status=%q err=%v", status, err)
	}

	// 2. Install v1, then upgrade to identical render -> up-to-date.
	if _, _, err := applyRulesDoc(doc, v1, false); err != nil {
		t.Fatal(err)
	}
	status, _, _, err = upgradeRulesDoc(doc, v1, false)
	if err != nil || status != "up-to-date" {
		t.Fatalf("same render: status=%q err=%v", status, err)
	}

	// 3. Upgrade to v2 -> upgraded with backup; custom lines (user edits
	// not in the new render) are reported.
	v2 := "## 13. 规则\n- 条款A\n- 条款B（新增）\n- 白名单：`E:\\P4\\SGAME_*`"
	// Simulate a user customization inside the installed block.
	data, _ := os.ReadFile(doc)
	doctored := strings.Replace(string(data), "- 条款A", "- 条款A\n- 我的自定义条款", 1)
	_ = os.WriteFile(doc, []byte(doctored), 0o644)

	status, backup, custom, err := upgradeRulesDoc(doc, v2, false)
	if err != nil || status != "upgraded" || backup == "" {
		t.Fatalf("upgrade: status=%q backup=%q err=%v", status, backup, err)
	}
	if len(custom) != 1 || !strings.Contains(custom[0], "我的自定义条款") {
		t.Fatalf("customization not reported: %v", custom)
	}
	// New content in place, old custom line reported but not preserved.
	out, _ := os.ReadFile(doc)
	if !strings.Contains(string(out), "条款B（新增）") {
		t.Fatal("upgraded doc missing new content")
	}

	// 4. Dry-run upgrade writes nothing.
	v3 := "## 13. 规则 v3"
	before, _ := os.ReadFile(doc)
	status, _, custom, err = upgradeRulesDoc(doc, v3, true)
	if err != nil || status != "dry-run(upgrade)" {
		t.Fatalf("dry-run: status=%q err=%v", status, err)
	}
	if len(custom) == 0 {
		t.Fatal("dry-run should still report customized lines")
	}
	after, _ := os.ReadFile(doc)
	if string(before) != string(after) {
		t.Fatal("dry-run mutated the doc")
	}
}

func TestPreserveWhitelist(t *testing.T) {
	oldLines := []string{
		"## 13. 规则",
		"例外（co-use）：当前白名单：`E:\\P4\\SGAME_*`（depot `//sgproj/trunk`）。",
		"- 其他条款",
	}
	rendered := "## 13. 规则\n例外（co-use）：当前白名单：未配置。（depot N/A）。\n- 其他条款"
	got := preserveWhitelist(oldLines, rendered)
	if !strings.Contains(got, `E:\P4\SGAME_*`) || !strings.Contains(got, "//sgproj/trunk") {
		t.Fatalf("whitelist custom not preserved: %q", got)
	}
	// No customization in old block -> render unchanged.
	if got := preserveWhitelist(nil, rendered); got != rendered {
		t.Fatalf("render should be unchanged without old block: %q", got)
	}
}

// removeRulesDoc is the inverse of applyRulesDoc; these tests lock its
// contract across every state applyRulesDoc's own lifecycle test exercises.
func TestRemoveRulesDocLifecycle(t *testing.T) {
	dir := t.TempDir()

	// 1. Document does not exist -> "absent", no error, nothing written.
	doc := filepath.Join(dir, "ABSENT.md")
	status, backup, err := removeRulesDoc(doc, false)
	if err != nil || status != "absent" || backup != "" {
		t.Fatalf("absent: status=%q backup=%q err=%v", status, backup, err)
	}

	// 2. Document exists but was never touched by upipe -> "no-managed-block".
	doc2 := filepath.Join(dir, "UNMANAGED.md")
	if err := os.WriteFile(doc2, []byte("hand-written content"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, backup, err = removeRulesDoc(doc2, false)
	if err != nil || status != "no-managed-block" || backup != "" {
		t.Fatalf("unmanaged: status=%q backup=%q err=%v", status, backup, err)
	}
	after, _ := os.ReadFile(doc2)
	if string(after) != "hand-written content" {
		t.Fatal("no-managed-block path must not touch the document")
	}

	// 3. Document created purely to hold the block (applyRulesDoc "created"
	// path) -> stripping leaves it empty, so the file is deleted entirely.
	doc3 := filepath.Join(dir, "CREATED.md")
	if _, _, err := applyRulesDoc(doc3, "## 13. 规则", false); err != nil {
		t.Fatal(err)
	}
	status, backup, err = removeRulesDoc(doc3, false)
	if err != nil || status != "removed-file-deleted" || backup == "" {
		t.Fatalf("created-then-removed: status=%q backup=%q err=%v", status, backup, err)
	}
	if _, serr := os.Stat(doc3); !os.IsNotExist(serr) {
		t.Fatal("emptied document should have been deleted")
	}
	bak, _ := os.ReadFile(filepath.Join(dir, backup))
	if !strings.Contains(string(bak), ruleBeginMarker) {
		t.Fatal("backup should preserve the pre-removal content")
	}

	// 4. Document with surrounding user content -> block stripped, rest kept.
	doc4 := filepath.Join(dir, "MIXED.md")
	if err := os.WriteFile(doc4, []byte("existing content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyRulesDoc(doc4, "## 13. 规则", false); err != nil {
		t.Fatal(err)
	}
	status, backup, err = removeRulesDoc(doc4, false)
	if err != nil || status != "removed" || backup == "" {
		t.Fatalf("mixed: status=%q backup=%q err=%v", status, backup, err)
	}
	out, _ := os.ReadFile(doc4)
	if strings.Contains(string(out), ruleBeginMarker) {
		t.Fatal("managed block should have been stripped")
	}
	if !strings.Contains(string(out), "existing content") {
		t.Fatal("user content around the block should survive")
	}

	// 5. Dry-run never writes, for every state.
	doc5 := filepath.Join(dir, "DRYRUN.md")
	if _, _, err := applyRulesDoc(doc5, "## 13. 规则", false); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(doc5)
	status, backup, err = removeRulesDoc(doc5, true)
	if err != nil || status != "dry-run(remove)" || backup != "" {
		t.Fatalf("dry-run remove: status=%q backup=%q err=%v", status, backup, err)
	}
	afterDry, _ := os.ReadFile(doc5)
	if string(before) != string(afterDry) {
		t.Fatal("dry-run must not mutate the document")
	}
}

// TestReadBlockLines locks the thin readManagedBlock->lines wrapper used by
// upgradeRulesDoc to diff customizations: absent block -> nil, present block
// -> its lines.
func TestReadBlockLines(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "CLAUDE.md")

	if got := readBlockLines(doc); got != nil {
		t.Fatalf("missing document should yield nil lines, got %v", got)
	}

	if _, _, err := applyRulesDoc(doc, "line one\nline two", false); err != nil {
		t.Fatal(err)
	}
	got := readBlockLines(doc)
	if len(got) != 2 || got[0] != "line one" || got[1] != "line two" {
		t.Fatalf("unexpected block lines: %v", got)
	}
}
