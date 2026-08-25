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
