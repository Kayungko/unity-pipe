package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// sortedKeys gives deterministic iteration over a doc->template map.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ruleMarkers wrap the appended rules section so re-runs are idempotent:
// a document that already contains the managed block is left untouched.
const (
	ruleBeginMarker = "<!-- unity-pipe:rules begin — managed by `upipe plugin install --apply-rules`; re-run is a no-op, edit freely inside -->"
	ruleEndMarker   = "<!-- unity-pipe:rules end -->"
)

// ruleVars carries the template substitutions for the harness rule templates.
type ruleVars struct {
	Repo      string // {{UNITY_PIPE_REPO}}
	Bin       string // {{UPIPE_BIN}}
	Guide     string // {{UNITY_GUIDE}}
	Whitelist string // {{CO_USE_WHITELIST}}
	Depot     string // {{CO_USE_DEPOT}}
}

// pipeline-only defaults used when the caller does not pass
// --co-use-whitelist/--co-use-depot: every project routes to Pipeline only,
// which is the safe default the skills' §0 fallback also lands on.
const (
	defaultWhitelist = "未配置——全部工程 pipeline-only（如需启用 co-use，编辑此段填入工程路径 glob，如 E:\\P4\\SGAME_*）"
	defaultDepot     = "N/A"
)

// extractRuleBody pulls the ```markdown fenced block out of a rule template
// file (harness/rules/*.md) — the prose around it is instructions for humans.
func extractRuleBody(tmplPath string) (string, error) {
	raw, err := os.ReadFile(tmplPath)
	if err != nil {
		return "", fmt.Errorf("rule template not found: %w", err)
	}
	return extractFencedMarkdown(string(raw), tmplPath)
}

func extractFencedMarkdown(s, src string) (string, error) {
	const fence = "```markdown"
	begin := strings.Index(s, fence)
	if begin < 0 {
		return "", fmt.Errorf("%s: no ```markdown fenced block found", src)
	}
	rest := s[begin+len(fence):]
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", fmt.Errorf("%s: unterminated ```markdown fenced block", src)
	}
	return strings.TrimSpace(rest[:end]), nil
}

// renderRule substitutes the {{...}} placeholders in a rule body.
func renderRule(body string, v ruleVars) string {
	if v.Whitelist == "" {
		v.Whitelist = defaultWhitelist
	}
	if v.Depot == "" {
		v.Depot = defaultDepot
	}
	r := strings.NewReplacer(
		"{{UNITY_PIPE_REPO}}", v.Repo,
		"{{UPIPE_BIN}}", v.Bin,
		"{{UNITY_GUIDE}}", v.Guide,
		"{{CO_USE_WHITELIST}}", v.Whitelist,
		"{{CO_USE_DEPOT}}", v.Depot,
	)
	return r.Replace(body)
}

// applyRulesDoc writes the rendered rules into a global harness document.
//
// Statuses: "created" (document did not exist), "appended" (existing document
// extended, backup taken), "already-managed" (managed block present — no-op),
// "dry-run" (nothing written).
func applyRulesDoc(docPath, rendered string, dryRun bool) (status, backup string, err error) {
	block := ruleBeginMarker + "\n" + rendered + "\n" + ruleEndMarker
	if dryRun {
		if _, serr := os.Stat(docPath); serr != nil {
			return "dry-run(create)", "", nil
		}
		existing, rerr := os.ReadFile(docPath)
		if rerr != nil {
			return "", "", rerr
		}
		if strings.Contains(string(existing), ruleBeginMarker) {
			return "dry-run(already-managed)", "", nil
		}
		return "dry-run(append)", "", nil
	}

	existing, rerr := os.ReadFile(docPath)
	if os.IsNotExist(rerr) {
		if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
			return "", "", err
		}
		if err := os.WriteFile(docPath, []byte(block+"\n"), 0o644); err != nil {
			return "", "", err
		}
		return "created", "", nil
	}
	if rerr != nil {
		return "", "", rerr
	}
	if strings.Contains(string(existing), ruleBeginMarker) {
		return "already-managed", "", nil
	}
	stamp := time.Now().Format("20060102-150405")
	backup = docPath + ".upipe-bak-" + stamp
	if err := os.WriteFile(backup, existing, 0o644); err != nil {
		return "", "", fmt.Errorf("backup %s: %w", backup, err)
	}
	sep := "\n"
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		sep = "\n\n"
	}
	out := string(existing) + sep + block + "\n"
	if err := os.WriteFile(docPath, []byte(out), 0o644); err != nil {
		return "", "", err
	}
	return "appended", filepath.Base(backup), nil
}
