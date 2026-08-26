package lint_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionSourcesAgree locks the three manual version references to the
// same value: package/com.unity.pipeline/package.json ("version"),
// internal/commands.VendoredPkgRef, and — when a dist tgz exists — its file
// name. A mismatch between any pair is a release-bookkeeping bug that ships
// a CLI reporting the wrong vendored package version.
func TestVersionSourcesAgree(t *testing.T) {
	repo := findRepoRoot(t)

	// 1. package.json version.
	pkgJSON, err := os.ReadFile(filepath.Join(repo, "package", "com.unity.pipeline", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(pkgJSON, &pkg); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("package.json has empty version")
	}

	// 2. VendoredPkgRef constant in internal/commands/version.go.
	versionGo, err := os.ReadFile(filepath.Join(repo, "internal", "commands", "version.go"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = `VendoredPkgRef = "`
	idx := strings.Index(string(versionGo), marker)
	if idx < 0 {
		t.Fatal("VendoredPkgRef constant not found in version.go")
	}
	rest := string(versionGo)[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("malformed VendoredPkgRef constant")
	}
	ref := rest[:end]

	if pkg.Version != ref {
		t.Errorf("version mismatch: package.json says %q but VendoredPkgRef says %q — "+
			"bump both together (see ADAPTATION-2022.md upgrade flow step 3)", pkg.Version, ref)
	}

	// 3. Any tgz in dist/ must be the current version (stale generations must
	// be cleaned before release; the GitHub Release page keeps history).
	entries, err := os.ReadDir(filepath.Join(repo, "dist"))
	if err != nil || len(entries) == 0 {
		return // dist/ absent or empty — nothing to check
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".tgz") {
			continue
		}
		// Shape: com.unity.pipeline-<version>.tgz
		trimmed := strings.TrimSuffix(strings.TrimPrefix(name, "com.unity.pipeline-"), ".tgz")
		if trimmed != pkg.Version {
			t.Errorf("stale dist artifact %q (current package version is %q) — "+
				"remove old generations from dist/ before release", name, pkg.Version)
		}
	}
}
