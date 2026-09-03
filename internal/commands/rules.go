package commands

import (
	"crypto/sha256"
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

// rulesBlockHash returns a stable hash of the managed block content (the
// rendered body between the markers, whitespace-trimmed). Used to detect
// whether the installed block still matches what the current templates
// produce — i.e. whether an upgrade would change anything.
func rulesBlockHash(rendered string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(rendered))))
}

// readManagedBlock extracts the current managed block body from a document
// ("", false) if absent).
func readManagedBlock(docPath string) (string, bool, error) {
	data, err := os.ReadFile(docPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	s := string(data)
	begin := strings.Index(s, ruleBeginMarker)
	end := strings.LastIndex(s, ruleEndMarker)
	if begin < 0 || end < 0 || end < begin {
		return "", false, nil
	}
	body := s[begin+len(ruleBeginMarker) : end]
	return strings.TrimSpace(body), true, nil
}

// upgradeRulesDoc replaces the managed block in a document with the freshly
// rendered rules, PRESERVING user customizations made inside the old block:
// any line in the old block that does not appear in the new render is
// reported so the human can re-apply it (we do not silently merge — the
// report is the safety net).
//
// Statuses: "up-to-date" (hash match, untouched), "upgraded" (replaced,
// backup taken), "missing-block" (nothing to upgrade — run --apply-rules
// first), "dry-run(upgrade)" / "dry-run(up-to-date)".
func upgradeRulesDoc(docPath, rendered string, dryRun bool) (status, backup string, custom []string, err error) {
	old, ok, rerr := readManagedBlock(docPath)
	if rerr != nil {
		return "", "", nil, rerr
	}
	if !ok {
		return "missing-block", "", nil, nil
	}
	if rulesBlockHash(old) == rulesBlockHash(rendered) {
		return "up-to-date", "", nil, nil
	}
	// User customizations: lines present in the old block but absent from
	// the new render (ignoring the placeholder lines that legitimately
	// change between machines).
	newLines := map[string]bool{}
	for _, l := range strings.Split(rendered, "\n") {
		newLines[strings.TrimSpace(l)] = true
	}
	for _, l := range strings.Split(old, "\n") {
		t := strings.TrimSpace(l)
		if t != "" && !newLines[t] {
			custom = append(custom, l)
		}
	}

	if dryRun {
		if len(custom) > 0 {
			return "dry-run(upgrade)", "", custom, nil
		}
		return "dry-run(upgrade)", "", nil, nil
	}

	data, err := os.ReadFile(docPath)
	if err != nil {
		return "", "", nil, err
	}
	s := string(data)
	begin := strings.Index(s, ruleBeginMarker)
	end := strings.LastIndex(s, ruleEndMarker)
	if begin < 0 || end < 0 || end < begin {
		return "missing-block", "", nil, nil
	}
	stamp := time.Now().Format("20060102-150405")
	backup = docPath + ".upipe-bak-" + stamp
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", "", nil, fmt.Errorf("backup %s: %w", backup, err)
	}
	block := ruleBeginMarker + "\n" + rendered + "\n" + ruleEndMarker
	out := s[:begin] + block + s[end+len(ruleEndMarker):]
	if err := os.WriteFile(docPath, []byte(out), 0o644); err != nil {
		return "", "", nil, err
	}
	return "upgraded", filepath.Base(backup), custom, nil
}

// readBlockLines returns the lines of the managed block (nil if absent).
func readBlockLines(docPath string) []string {
	body, ok, err := readManagedBlock(docPath)
	if err != nil || !ok {
		return nil
	}
	return strings.Split(body, "\n")
}

// removeRulesDoc strips the managed rules block from a global harness
// document — the inverse of applyRulesDoc, used by `upipe plugin uninstall
// --remove-rules`. It backs up the document first (same convention as
// apply/upgrade). If stripping the block leaves the document empty (it was
// created purely to hold the block — the "created" status path of
// applyRulesDoc), the now-empty file is deleted instead of left as a husk.
//
// Statuses: "absent" (document does not exist — nothing to do),
// "no-managed-block" (document exists but was never touched by upipe),
// "removed" (block stripped, backup taken), "removed-file-deleted" (block
// stripped and the now-empty file was deleted), "dry-run(...)" variants of
// the above with nothing written.
func removeRulesDoc(docPath string, dryRun bool) (status, backup string, err error) {
	data, rerr := os.ReadFile(docPath)
	if os.IsNotExist(rerr) {
		if dryRun {
			return "dry-run(absent)", "", nil
		}
		return "absent", "", nil
	}
	if rerr != nil {
		return "", "", rerr
	}
	s := string(data)
	begin := strings.Index(s, ruleBeginMarker)
	end := strings.LastIndex(s, ruleEndMarker)
	if begin < 0 || end < 0 || end < begin {
		if dryRun {
			return "dry-run(no-managed-block)", "", nil
		}
		return "no-managed-block", "", nil
	}
	if dryRun {
		return "dry-run(remove)", "", nil
	}

	stamp := time.Now().Format("20060102-150405")
	backup = docPath + ".upipe-bak-" + stamp
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", "", fmt.Errorf("backup %s: %w", backup, err)
	}
	out := strings.TrimSpace(s[:begin] + s[end+len(ruleEndMarker):])
	if out == "" {
		if err := os.Remove(docPath); err != nil {
			return "", "", err
		}
		return "removed-file-deleted", filepath.Base(backup), nil
	}
	if err := os.WriteFile(docPath, []byte(out+"\n"), 0o644); err != nil {
		return "", "", err
	}
	return "removed", filepath.Base(backup), nil
}

// preserveWhitelist keeps the whitelist/depot configuration from the old
// block when upgrading without explicit --co-use-* flags: those values are
// machine customizations, and resetting them to the pipeline-only default
// on a template refresh would silently change routing behavior.
func preserveWhitelist(oldLines []string, rendered string) string {
	var oldWL, oldDepot string
	for _, l := range oldLines {
		if strings.Contains(l, "当前白名单：") {
			oldWL = strings.TrimSpace(l)
		}
		if strings.Contains(l, "（depot") {
			oldDepot = strings.TrimSpace(l)
		}
	}
	if oldWL == "" {
		return rendered // no customization found in old block
	}
	lines := strings.Split(rendered, "\n")
	for i, l := range lines {
		if strings.Contains(l, "当前白名单：") && oldWL != "" {
			lines[i] = oldWL
		}
		if strings.Contains(l, "（depot") && oldDepot != "" {
			lines[i] = oldDepot
		}
	}
	return strings.Join(lines, "\n")
}
