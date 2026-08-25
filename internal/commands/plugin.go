package commands

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// PluginCommand installs the harness skill set into the AI agent skill
// directories: AI Mana (~/.claude/skills) and Codex (~/.codex/skills).
// Existing installs are backed up (never deleted); every copied file is
// SHA256-verified against the source.
func PluginCommand() SubCommand {
	return SubCommand{
		Name:      "plugin",
		Aliases:   []string{"plugin-install"},
		Synopsis:  "install harness skills: upipe plugin install [--claude|--codex|--all] [--from <repo>] [--apply-rules [--co-use-whitelist <glob>] [--co-use-depot <depot>]]",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe plugin install")
			claude := fs.Bool("claude", false, "install into ~/.claude/skills (AI Mana)")
			codex := fs.Bool("codex", false, "install into ~/.codex/skills (Codex)")
			from := fs.String("from", "", "unity-pipe repo root (default: auto-detect from cwd)")
			dryRun := fs.Bool("dry-run", false, "show the plan without writing")
			applyRules := fs.Bool("apply-rules", false, "also append the routing rules to the global harness docs (~/.claude/CLAUDE.md / ~/.codex/AGENTS.md); idempotent, existing docs are backed up first")
			coUseWhitelist := fs.String("co-use-whitelist", "", "co-use project path glob for --apply-rules (e.g. 'E:\\P4\\SGAME_*'); empty = pipeline-only safe default")
			coUseDepot := fs.String("co-use-depot", "", "P4 depot of whitelisted projects for --apply-rules (e.g. //sgproj/trunk)")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("plugin: " + err.Error())
			}
			// Tolerate the documented `upipe plugin install ...` spelling.
			if len(rest) > 0 && rest[0] == "install" {
				rest = rest[1:]
			}
			if len(rest) > 0 {
				return nil, usageErr("plugin: unexpected argument " + rest[0])
			}

			doClaude, doCodex := *claude, *codex
			if !doClaude && !doCodex {
				doClaude, doCodex = true, true // default: both
			}

			repoRoot := *from
			if repoRoot == "" {
				repoRoot, err = findRepoRoot()
				if err != nil {
					return nil, usageErr("plugin: " + err.Error())
				}
			}
			src := filepath.Join(repoRoot, "harness", "skills")
			skills, err := listSkillDirs(src)
			if err != nil {
				return nil, usageErr("plugin: " + err.Error())
			}

			result := map[string]any{
				"source":  src,
				"skills":  skills,
				"targets": []string{},
				"dry_run": *dryRun,
			}
			var targets []string
			if doClaude {
				targets = append(targets, userHomeDir(".claude", "skills"))
			}
			if doCodex {
				targets = append(targets, userHomeDir(".codex", "skills"))
			}

			type installReport struct {
				Target   string   `json:"target"`
				Backups  []string `json:"backups"`
				Files    int      `json:"files"`
				Verified bool     `json:"verified"`
			}
			var reports []installReport
			for _, target := range targets {
				rep := installReport{Target: target, Backups: []string{}}
				if !*dryRun {
					backups, files, verified, ierr := installSkills(src, skills, target)
					if ierr != nil {
						return nil, protocol.AsUpipeError(ierr)
					}
					rep.Backups, rep.Files, rep.Verified = backups, files, verified
				}
				reports = append(reports, rep)
			}
			result["targets"] = targets
			result["installs"] = reports

			if *applyRules {
				// Deterministic replacement for the old "paste the template
				// yourself" step: render harness/rules/*.md with the local
				// paths filled in and append a managed block to the global
				// harness docs.
				bin, _ := os.Executable()
				if bin == "" {
					bin = "upipe"
				}
				vars := ruleVars{
					Repo:      repoRoot,
					Bin:       bin,
					Guide:     userHomeDir(".codex", "UNITY-GUIDE.md"),
					Whitelist: *coUseWhitelist,
					Depot:     *coUseDepot,
				}
				docs := map[string]string{}
				if doClaude {
					docs[userHomeDir(".claude", "CLAUDE.md")] = filepath.Join(repoRoot, "harness", "rules", "claude.md")
				}
				if doCodex {
					docs[userHomeDir(".codex", "AGENTS.md")] = filepath.Join(repoRoot, "harness", "rules", "agents.md")
				}
				type ruleReport struct {
					Doc     string `json:"doc"`
					Status  string `json:"status"`
					Backup  string `json:"backup,omitempty"`
					Warning string `json:"warning,omitempty"`
				}
				var ruleReports []ruleReport
				for _, doc := range sortedKeys(docs) {
					tmpl := docs[doc]
					rep := ruleReport{Doc: doc}
					body, terr := extractRuleBody(tmpl)
					if terr != nil {
						rep.Status = "failed"
						rep.Warning = terr.Error()
						ruleReports = append(ruleReports, rep)
						continue
					}
					status, backup, aerr := applyRulesDoc(doc, renderRule(body, vars), *dryRun)
					if aerr != nil {
						return nil, protocol.AsUpipeError(aerr)
					}
					rep.Status, rep.Backup = status, backup
					ruleReports = append(ruleReports, rep)
				}
				result["rules_applied"] = ruleReports
			}

			if !*dryRun {
				result["note"] = "AI Mana hot-loads skills in the current session; Codex needs a new session."
				// Skills reference the global routing rules (co-use whitelist,
				// serialized-asset red lines) by name; the deploy is complete
				// once the rules section lands in the global docs.
				rules := filepath.Join(repoRoot, "harness", "rules")
				if _, err := os.Stat(rules); err == nil {
					result["rules_template"] = rules
					note, _ := result["note"].(string)
					if *applyRules {
						result["note"] = note + " Routing rules appended as a managed block (search \"unity-pipe:rules\"); re-running is a no-op. If --co-use-whitelist was not passed, the block defaults to pipeline-only — edit it to enable co-use."
					} else {
						result["note"] = note + " Routing rules (co-use whitelist etc.) are NOT installed by this command — re-run with --apply-rules to append them to ~/.claude/CLAUDE.md and ~/.codex/AGENTS.md (idempotent, backs up first), or paste harness/rules/*.md manually (see harness/rules/README.md)."
					}
				}
			}
			return result, nil
		},
	}
}

// findRepoRoot walks up from cwd looking for the harness/skills directory.
func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, ok := protocol.FindProjectRoot(cwd, func(dir string) bool {
		st, err := os.Stat(filepath.Join(dir, "harness", "skills"))
		return err == nil && st.IsDir()
	})
	if !ok {
		return "", fmt.Errorf("cannot locate the unity-pipe repo (harness/skills not found upward from cwd); pass --from <repo root>")
	}
	return root, nil
}

func listSkillDirs(src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, fmt.Errorf("harness/skills not found under the repo: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no skill directories in %s", src)
	}
	return out, nil
}

// installSkills backs up existing dirs, copies, and SHA256-verifies.
// Backups land OUTSIDE the skills directory (a sibling <skills>-backup dir) —
// harnesses scan every subdirectory of skills/, so .bak folders inside would
// surface as duplicate skills.
func installSkills(src string, skills []string, target string) ([]string, int, bool, error) {
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, 0, false, err
	}
	stamp := time.Now().Format("20060102-150405")
	backupDir := target + "-backup"
	var backups []string
	files := 0
	for _, skill := range skills {
		dst := filepath.Join(target, skill)
		if _, err := os.Stat(dst); err == nil {
			if err := os.MkdirAll(backupDir, 0o755); err != nil {
				return backups, files, false, err
			}
			bak := filepath.Join(backupDir, fmt.Sprintf("%s-%s", skill, stamp))
			if err := os.Rename(dst, bak); err != nil {
				return backups, files, false, fmt.Errorf("backup %s: %w", dst, err)
			}
			backups = append(backups, filepath.Base(bak))
		}
		n, err := copyDir(filepath.Join(src, skill), dst)
		if err != nil {
			return backups, files, false, err
		}
		files += n
	}
	// Verify: hash-compare every installed file against the source.
	verified := true
	for _, skill := range skills {
		skerr := filepath.WalkDir(filepath.Join(src, skill), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			a, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			b, rerr := os.ReadFile(filepath.Join(target, rel))
			if rerr != nil {
				return rerr
			}
			if fmt.Sprintf("%x", sha256.Sum256(a)) != fmt.Sprintf("%x", sha256.Sum256(b)) {
				verified = false
			}
			return nil
		})
		if skerr != nil {
			return backups, files, false, skerr
		}
	}
	return backups, files, verified, nil
}

func copyDir(srcDir, dstDir string) (int, error) {
	n := 0
	err := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return rerr
		}
		dst := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		in, oerr := os.Open(path)
		if oerr != nil {
			return oerr
		}
		defer in.Close()
		out, oerr := os.Create(dst)
		if oerr != nil {
			return oerr
		}
		defer out.Close()
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		n++
		return nil
	})
	return n, err
}

func userHomeDir(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return strings.Join(parts, string(filepath.Separator))
	}
	return filepath.Join(append([]string{home}, parts...)...)
}
