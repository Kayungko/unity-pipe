// Command build packages the vendored com.unity.pipeline tree into a UPM
// installable tgz under dist/, excluding content that must not ship:
//
//   - Tests/            (upstream tests target Unity 6 APIs, excluded from deployment)
//   - .signature        (upstream attestation, invalid for a modified tree)
//   - .attestation.p7m  (same)
//
// Usage: go run ./package/build   (from the repo root)
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const vendoredDir = "package/com.unity.pipeline"
const distDir = "dist"

// excludedTop marks top-level entries stripped from the distribution.
func excludedTop(name string) bool {
	switch name {
	case "Tests", "Tests.meta", ".signature", ".attestation.p7m":
		return true
	// Repo-internal instruction files must not ship inside the UPM package —
	// they are workspace tooling, not package content.
	case "CLAUDE.md", "CLAUDE.md.meta", "CODEOWNERS", "CODEOWNERS.meta", ".claude", ".claude.meta":
		return true
	}
	return false
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
}

func run() error {
	meta, err := os.ReadFile(filepath.Join(vendoredDir, "package.json"))
	if err != nil {
		return fmt.Errorf("read package.json: %w", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(meta, &pkg); err != nil {
		return fmt.Errorf("parse package.json: %w", err)
	}

	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}
	outPath := filepath.Join(distDir, fmt.Sprintf("%s-%s.tgz", pkg.Name, pkg.Version))
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	h := sha256.New()
	gz := gzip.NewWriter(io.MultiWriter(out, h))
	tw := tar.NewWriter(gz)

	files, skipped := 0, 0
	err = filepath.WalkDir(vendoredDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(vendoredDir, path)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		top := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if excludedTop(top) {
			if d.IsDir() {
				skipped += countFiles(path)
				return filepath.SkipDir
			}
			skipped++
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, rerr2 := os.ReadFile(path)
		if rerr2 != nil {
			return rerr2
		}
		// UPM tgz layout: everything under a single "package/" root.
		archivePath := "package/" + filepath.ToSlash(rel)
		hdr := &tar.Header{
			Name:     archivePath,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
			Format:   tar.FormatGNU,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
		files++
		return nil
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	sumPath := outPath + ".sha256"
	sumLine := fmt.Sprintf("%x  %s\n", h.Sum(nil), filepath.Base(outPath))
	if err := os.WriteFile(sumPath, []byte(sumLine), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s\n  files: %d (excluded: %d)\n  sha256: %s", outPath, files, skipped, sumLine)
	return nil
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}
