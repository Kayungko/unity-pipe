package commands

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// PkgStatusCommand reports the installed pipeline package version of a
// project (from its Packages/com.unity.pipeline/package.json).
func PkgStatusCommand() SubCommand {
	return SubCommand{
		Name:     "pkg-status",
		Aliases:  []string{"pkg"},
		Synopsis: "report the installed com.unity.pipeline package version",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe pkg-status")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("pkg-status: " + err.Error())
			}
			installed, err := installedPkgVersion(rt.ProjectPath)
			if err != nil {
				return nil, protocol.ErrNoInstance("pipeline package: " + err.Error())
			}
			return map[string]any{
				"project":     rt.ProjectPath,
				"package":     "com.unity.pipeline",
				"version":     installed,
				"vendoredRef": "0.5.0-2022.1 (github.com/Kayungko/unity-pipe)",
			}, nil
		},
	}
}

// PkgInstallCommand installs/updates the adapted com.unity.pipeline package
// into a project from a tgz built by package/build. It never performs P4
// operations — a read-only target aborts with guidance.
func PkgInstallCommand() SubCommand {
	return SubCommand{
		Name:     "pkg-install",
		Synopsis: "install the adapted pipeline package: upipe pkg-install --source <dist tgz>",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe pkg-install")
			source := fs.String("source", "", "path to the com.unity.pipeline-*.tgz built by this repo")
			dryRun := fs.Bool("dry-run", false, "list the planned changes without writing")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("pkg-install: " + err.Error())
			}
			if *source == "" {
				return nil, usageErr("pkg-install: --source <tgz> is required")
			}

			target := filepath.Join(rt.ProjectPath, "Packages", "com.unity.pipeline")
			plan, err := extractPlan(*source, target)
			if err != nil {
				return nil, protocol.AsUpipeError(err)
			}

			// Preflight: a P4-managed workspace keeps unchanged files
			// read-only; refuse instead of half-failing.
			if ro := firstReadOnly(target); ro != "" {
				return nil, &protocol.UpipeError{
					Code: protocol.ExitCommandFailed,
					Kind: "ReadOnlyTarget",
					Message: fmt.Sprintf("target contains read-only file %s — this looks like a P4-managed workspace; "+
						"p4 edit / reconcile the package first (upipe never runs p4 itself)", ro),
				}
			}

			if *dryRun {
				return plan, nil
			}
			written, err := applyPlan(plan)
			if err != nil {
				return nil, protocol.AsUpipeError(err)
			}
			return map[string]any{
				"source":    *source,
				"target":    target,
				"written":   written,
				"updated":   len(plan.Updated),
				"added":     len(plan.Added),
				"unchanged": len(plan.Unchanged),
			}, nil
		},
	}
}

// pkgPlan is the diff-based install plan against an existing install.
type pkgPlan struct {
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	Added     []string `json:"added"`
	Updated   []string `json:"updated"`
	Unchanged []string `json:"unchanged"`
	entries   map[string][]byte
}

func installedPkgVersion(project string) (string, error) {
	data, err := os.ReadFile(filepath.Join(project, "Packages", "com.unity.pipeline", "package.json"))
	if err != nil {
		return "", err
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", err
	}
	return pkg.Version, nil
}

// extractPlan reads the tgz and classifies each shipped file against the
// target directory.
func extractPlan(tgzPath, target string) (*pkgPlan, error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("not a gzip/tgz: %w", err)
	}
	defer gz.Close()

	plan := &pkgPlan{
		Source:    tgzPath,
		Target:    target,
		Added:     []string{},
		Updated:   []string{},
		Unchanged: []string{},
		entries:   map[string][]byte{},
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(filepath.ToSlash(hdr.Name), "package/")
		if name == "" || strings.HasPrefix(name, "Tests/") {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		plan.entries[name] = data
		dst := filepath.Join(target, filepath.FromSlash(name))
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			plan.Added = append(plan.Added, name)
		} else {
			old, rerr := os.ReadFile(dst)
			if rerr == nil && bytesEqual(old, data) {
				plan.Unchanged = append(plan.Unchanged, name)
			} else {
				plan.Updated = append(plan.Updated, name)
			}
		}
	}
	if len(plan.entries) == 0 {
		return nil, protocol.ErrProtocol("tgz contains no package/ entries — not a UPM package tarball?")
	}
	return plan, nil
}

func applyPlan(plan *pkgPlan) (int, error) {
	if err := os.MkdirAll(plan.Target, 0o755); err != nil {
		return 0, err
	}
	written := 0
	for name, data := range plan.entries {
		dst := filepath.Join(plan.Target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", name, err)
		}
		written++
	}
	return written, nil
}

// firstReadOnly returns one read-only file under dir ("" if none).
func firstReadOnly(dir string) string {
	var found string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil && info.Mode().Perm()&0o200 == 0 {
			found = path
		}
		return nil
	})
	return found
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
