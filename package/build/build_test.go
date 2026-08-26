package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildTarballLayout runs the real packaging flow in a sandboxed copy of
// the vendored tree and asserts the distribution contract: every entry lives
// under package/, and nothing the build must strip (Tests/, signatures, repo
// instruction files) ever ships. This locks what actually gets distributed —
// a regression here silently changes what lands in users' projects.
func TestBuildTarballLayout(t *testing.T) {
	repoRoot := findRepoRoot(t)

	// Sandbox: copy the vendored tree so run() writes its dist/ output inside
	// the temp dir instead of the repo.
	tmp := t.TempDir()
	src := filepath.Join(repoRoot, "package", "com.unity.pipeline")
	dst := filepath.Join(tmp, "package", "com.unity.pipeline")
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copy vendored tree: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}

	// run() reads the working directory layout (vendoredDir/distDir consts);
	// execute it with the sandbox as CWD.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	if err := run(); err != nil {
		t.Fatalf("build run(): %v", err)
	}

	// Locate the produced tgz (name = <pkg>-<version>.tgz).
	matches, _ := filepath.Glob(filepath.Join(tmp, "dist", "*.tgz"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one tgz in dist/, got %v", matches)
	}
	tgzPath := matches[0]

	f, err := os.Open(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()

	var names []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		names = append(names, filepath.ToSlash(hdr.Name))
	}

	// Contract: every entry starts with package/ and nothing stripped ships.
	forbidden := []string{
		"package/Tests/", "package/Tests.meta",
		"package/.signature", "package/.attestation.p7m",
		"package/CLAUDE.md", "package/CLAUDE.md.meta",
		"package/CODEOWNERS", "package/CODEOWNERS.meta",
		"package/.claude",
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "package/") {
			t.Errorf("entry outside package/ root: %q", n)
		}
		for _, bad := range forbidden {
			if n == bad || strings.HasPrefix(n, bad+"/") {
				t.Errorf("stripped content shipped in tgz: %q", n)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("tgz contains no entries")
	}

	// The .sha256 sidecar must match the tgz's actual digest.
	sidecar, err := os.ReadFile(tgzPath + ".sha256")
	if err != nil {
		t.Fatalf("read sha256 sidecar: %v", err)
	}
	want, err := fileSHA256(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sidecar), want) {
		t.Errorf("sha256 sidecar %q does not contain actual digest %s", strings.TrimSpace(string(sidecar)), want)
	}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repo root not found")
	return ""
}
