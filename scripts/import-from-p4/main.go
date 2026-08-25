// Command import-from-p4 closes the emergency-hotfix loop: it diffs the
// pipeline package inside a P4-managed Unity project against the repo's
// vendored tree and prints a patch-style report for human review.
//
// It is READ-ONLY on both sides: it never runs p4 commands (that red line
// belongs to humans/p4-ops), never writes to the project, and never commits —
// the report is the entire output. Applying accepted diffs back into the
// vendored tree stays a manual, reviewed step so the repo remains the single
// source of truth (one-way distribution, per docs/MIGRATION.md).
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	project := flag.String("project", "", "Unity project root (the P4 workspace)")
	repo := flag.String("repo", "", "unity-pipe repo root (defaults to walking up from cwd)")
	flag.Parse()
	if *project == "" {
		fmt.Fprintln(os.Stderr, "usage: import-from-p4 -project <unity project> [-repo <unity-pipe root>]")
		os.Exit(2)
	}
	repoRoot := *repo
	if repoRoot == "" {
		r, err := findRepoRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		repoRoot = r
	}
	vendored := filepath.Join(repoRoot, "package", "com.unity.pipeline")
	installed := filepath.Join(*project, "Packages", "com.unity.pipeline")

	if _, err := os.Stat(vendored); err != nil {
		fmt.Fprintf(os.Stderr, "vendored tree not found: %s\n", vendored)
		os.Exit(2)
	}
	if _, err := os.Stat(installed); err != nil {
		fmt.Fprintf(os.Stderr, "installed package not found: %s\n", installed)
		os.Exit(2)
	}

	vendoredFiles := walkFiles(vendored)
	installedFiles := walkFiles(installed)

	type diff struct {
		path  string // relative, slash-separated
		state string // modified | project-only | vendored-only
	}
	var diffs []diff
	seen := map[string]bool{}
	for rel := range installedFiles {
		seen[rel] = true
		vh, ok := vendoredFiles[rel]
		ih := installedFiles[rel]
		switch {
		case !ok:
			diffs = append(diffs, diff{rel, "project-only"})
		case vh != ih:
			diffs = append(diffs, diff{rel, "modified"})
		}
	}
	for rel := range vendoredFiles {
		if !seen[rel] {
			diffs = append(diffs, diff{rel, "vendored-only"})
		}
	}
	if len(diffs) == 0 {
		fmt.Println("in sync: installed package matches the vendored tree")
		return
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].path < diffs[j].path })

	fmt.Printf("hotfix diff: project %s  vs  vendored %s\n\n", installed, vendored)
	for _, d := range diffs {
		switch d.state {
		case "modified":
			fmt.Printf("  M  %s\n", d.path)
			hint := diffHint(filepath.Join(installed, filepath.FromSlash(d.path)), filepath.Join(vendored, filepath.FromSlash(d.path)))
			for _, l := range hint {
				fmt.Printf("       %s\n", l)
			}
		case "project-only":
			fmt.Printf("  A  %s   (exists only in the project — candidate to vendor)\n", d.path)
		case "vendored-only":
			fmt.Printf("  D  %s   (missing from the project — not a hotfix; informational)\n", d.path)
		}
	}
	fmt.Printf("\n%d difference(s). Review, then copy accepted changes into %s and commit.\n", len(diffs), vendored)
	fmt.Println("This tool is read-only: it never runs p4, never writes, never commits.")
}

// diffHint prints a unified-ish mini diff (first differing lines) for small
// text files; for large or binary files it only reports sizes/hashes.
func diffHint(projFile, vendFile string) []string {
	pb, perr := os.ReadFile(projFile)
	vb, verr := os.ReadFile(vendFile)
	if perr != nil || verr != nil {
		return nil
	}
	if !isProbablyText(pb) || !isProbablyText(vb) || len(pb)+len(vb) > 2<<20 {
		return []string{fmt.Sprintf("(binary or large: project %d B, vendored %d B)", len(pb), len(vb))}
	}
	pl, vl := strings.Split(string(pb), "\n"), strings.Split(string(vb), "\n")
	var out []string
	shown := 0
	for i := 0; i < len(pl) || i < len(vl); i++ {
		var a, b string
		if i < len(pl) {
			a = pl[i]
		}
		if i < len(vl) {
			b = vl[i]
		}
		if a == b {
			continue
		}
		out = append(out, fmt.Sprintf("-%d %s", i+1, strings.TrimRight(b, "\r")), fmt.Sprintf("+%d %s", i+1, strings.TrimRight(a, "\r")))
		shown++
		if shown >= 8 {
			out = append(out, "  … (truncated; diff the files for the full picture)")
			break
		}
	}
	return out
}

func isProbablyText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	limit := 4096
	if len(b) < limit {
		limit = len(b)
	}
	for _, c := range b[:limit] {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return false
		}
	}
	return true
}

func walkFiles(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Skip files the install pipeline also skips.
		if strings.HasPrefix(rel, "Tests/") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		out[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		return nil
	})
	return out
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "package", "com.unity.pipeline")); err == nil {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("unity-pipe repo root not found (no package/com.unity.pipeline upward); pass -repo")
		}
		wd = parent
	}
}
