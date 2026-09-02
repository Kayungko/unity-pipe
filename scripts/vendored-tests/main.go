// Command vendored-tests stages the vendored package's Editor test suite
// into a live Unity project so it can be compiled and run there — the CI
// substitute for a Unity license: the suite is excluded from the
// distribution tgz (upstream tests target Unity 6 APIs) and therefore only
// runs where this harness stages it.
//
// What it does to the host project (all reversible via --teardown):
//   - copies a minimal, self-sufficient subset of Tests/Editor/ into
//     Packages/com.unity.pipeline/Tests/Editor/ (the classes under test,
//     the PipelineTestServer helpers, the HTTP client, plus a trimmed
//     asmdef without Unity-6-only references)
//   - adds "com.unity.pipeline" to Packages/manifest.json testables
//   - applies a 2022.3 compatibility shim to the staged Material tests
//     (rawRenderQueue -> effective-queue assertion)
//
// It never deletes or overwrites unrelated content; --teardown removes
// exactly what --setup created (tracked in a manifest file).
//
// Usage:
//
//	go run ./scripts/vendored-tests --project <unity-project> --setup
//	go run ./scripts/vendored-tests --project <unity-project> --teardown
//	# then, outside: upipe -project-path <proj> recompile && upipe test --mode editor --filter <class>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	setupMarkName    = ".vendored-tests-staged.json"
	manifestTestable = "com.unity.pipeline"
)

// stagedFiles is the Tests/Editor subset that is self-sufficient on 2022.3:
// test classes + the PipelineTestServer trio + the HTTP client (normally in
// Tests/Runtime, moved into the Editor test assembly to avoid staging a
// second asmdef).
var stagedFiles = []string{
	"Tests/Editor/Prefabs/PrefabCommandsTests.cs",
	"Tests/Editor/Materials/MaterialCommandsTests.cs",
	"Tests/Editor/GameObjects/ComponentCommandsTests.cs",
	"Tests/Editor/Scenes/SceneCommandsTests.cs",
	"Tests/Editor/PipelineTestServer.cs",
	"Tests/Editor/TestEditorPipelineServer.cs",
	"Tests/Runtime/PipelineClient.cs", // staged into Tests/Editor/
	// 0.6 additions (AUTHAPI-27 batch / AUTHAPI-68 command line binding).
	"Tests/Editor/Batch/BatchCommandTests.cs",
	"Tests/Editor/CommandLineBinderTests.cs",
	"Tests/Editor/CommandLineTokenizerTests.cs",
}

// trimmedAsmdef drops the Unity-6-only references (InputSystem test framework,
// Tests.Runtime assembly) so the suite compiles on a 2022.3 project.
const trimmedAsmdef = `{
    "name": "Unity.Pipeline.Tests.Editor",
    "rootNamespace": "Unity.Pipeline.Tests.Editor",
    "references": [
        "UnityEditor.TestRunner",
        "Unity.Pipeline.Editor",
        "Unity.Pipeline",
        "UnityEngine.TestRunner"
    ],
    "includePlatforms": [
        "Editor"
    ],
    "excludePlatforms": [],
    "allowUnsafeCode": false,
    "overrideReferences": false,
    "precompiledReferences": [
        "nunit.framework.dll"
    ],
    "autoReferenced": true,
    "defineConstraints": [],
    "versionDefines": [],
    "noEngineReferences": false
}
`

type stageManifest struct {
	Project     string   `json:"project"`
	Copied      []string `json:"copied"`
	Wrote       []string `json:"wrote"` // generated files (asmdef etc.)
	ManifestHad string   `json:"manifestHadTestables"` // "" = had none
}

func main() {
	project := flag.String("project", "", "Unity project root (required)")
	setup := flag.Bool("setup", false, "stage the test suite into the project")
	teardown := flag.Bool("teardown", false, "remove exactly what --setup created")
	flag.Parse()

	if *project == "" || (!*setup && !*teardown) {
		fmt.Fprintln(os.Stderr, "usage: vendored-tests --project <unity-project> (--setup | --teardown)")
		os.Exit(2)
	}
	if err := run(*project, *setup); err != nil {
		fmt.Fprintln(os.Stderr, "vendored-tests:", err)
		os.Exit(1)
	}
}

func run(project string, setup bool) error {
	pkgDir := filepath.Join(project, "Packages", "com.unity.pipeline")
	if _, err := os.Stat(pkgDir); err != nil {
		return fmt.Errorf("project %s has no embedded com.unity.pipeline package — install it first (upipe pkg-install)", project)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	srcRoot := filepath.Join(repoRoot, "package", "com.unity.pipeline")
	markPath := filepath.Join(pkgDir, setupMarkName)

	if setup {
		return doSetup(repoRoot, srcRoot, pkgDir, project, markPath)
	}
	return doTeardown(pkgDir, project, markPath)
}

func doSetup(repoRoot, srcRoot, pkgDir, project, markPath string) error {
	if _, err := os.Stat(markPath); err == nil {
		return fmt.Errorf("already staged (%s present) — teardown first", setupMarkName)
	}
	var m stageManifest
	m.Project = project

	// 1. Copy the test subset (with .meta files — Unity needs them).
	for _, rel := range stagedFiles {
		dst := filepath.Join(pkgDir, filepath.FromSlash(rel))
		if strings.HasPrefix(rel, "Tests/Runtime/") {
			// PipelineClient lives in Tests/Runtime upstream; stage it inside
			// the Editor test assembly instead.
			dst = filepath.Join(pkgDir, "Tests", "Editor", filepath.Base(rel))
		}
		if err := copyFile(filepath.Join(srcRoot, filepath.FromSlash(rel)), dst); err != nil {
			return err
		}
		m.Copied = append(m.Copied, rel)
		// .meta companion.
		if err := copyFile(filepath.Join(srcRoot, filepath.FromSlash(rel+".meta")), dst+".meta"); err != nil {
			return err
		}
	}
	// Folder metas for the tree we create.
	for _, dir := range []string{"Tests", "Tests/Editor", "Tests/Editor/Prefabs", "Tests/Editor/Materials", "Tests/Editor/GameObjects", "Tests/Editor/Scenes", "Tests/Editor/Batch"} {
		if err := copyFile(filepath.Join(srcRoot, filepath.FromSlash(dir+".meta")), filepath.Join(pkgDir, filepath.FromSlash(dir+".meta"))); err != nil {
			return err
		}
	}

	// 2. Trimmed asmdef (regenerated — not copied).
	asmdef := filepath.Join(pkgDir, "Tests", "Editor", "Unity.Pipeline.Tests.Editor.asmdef")
	if err := os.MkdirAll(filepath.Dir(asmdef), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(asmdef, []byte(trimmedAsmdef), 0o644); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(srcRoot, "Tests", "Editor", "Unity.Pipeline.Tests.Editor.asmdef.meta"), asmdef+".meta"); err != nil {
		return err
	}
	m.Wrote = append(m.Wrote, "Tests/Editor/Unity.Pipeline.Tests.Editor.asmdef")

	// 3. 2022.3 shim on the staged Material tests.
	matTests := filepath.Join(pkgDir, "Tests", "Editor", "Materials", "MaterialCommandsTests.cs")
	if err := shim2022(matTests); err != nil {
		return err
	}

	// 4. testables entry in Packages/manifest.json.
	had, err := addTestable(filepath.Join(project, "Packages", "manifest.json"))
	if err != nil {
		return err
	}
	m.ManifestHad = had

	// 5. Write the stage manifest.
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(markPath, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("staged %d test files into %s\nnext: upipe -project-path %q recompile, then upipe test --mode editor --filter <TestClass>\n",
		len(m.Copied), pkgDir, project)
	return nil
}

func doTeardown(pkgDir, project, markPath string) error {
	data, err := os.ReadFile(markPath)
	if err != nil {
		return fmt.Errorf("nothing staged (no %s)", setupMarkName)
	}
	var m stageManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("corrupt stage manifest: %w", err)
	}
	for _, rel := range m.Copied {
		dst := filepath.Join(pkgDir, filepath.FromSlash(rel))
		if strings.HasPrefix(rel, "Tests/Runtime/") {
			dst = filepath.Join(pkgDir, "Tests", "Editor", filepath.Base(rel))
		}
		_ = os.Remove(dst)
		_ = os.Remove(dst + ".meta")
	}
	for _, rel := range m.Wrote {
		_ = os.Remove(filepath.Join(pkgDir, filepath.FromSlash(rel)))
	}
	// asmdef meta + folder metas + the Tests tree itself.
	_ = os.Remove(filepath.Join(pkgDir, "Tests", "Editor", "Unity.Pipeline.Tests.Editor.asmdef.meta"))
	_ = os.RemoveAll(filepath.Join(pkgDir, "Tests"))
	_ = os.Remove(filepath.Join(pkgDir, "Tests.meta"))

	if m.ManifestHad == "" {
		if err := removeTestable(filepath.Join(project, "Packages", "manifest.json")); err != nil {
			return err
		}
	} else if m.ManifestHad != manifestTestable {
		// Had other testables; only remove ours.
		if err := removeTestable(filepath.Join(project, "Packages", "manifest.json")); err != nil {
			return err
		}
	}
	_ = os.Remove(markPath)
	fmt.Println("torn down; run `upipe recompile` to settle the editor")
	return nil
}

// shim2022 rewrites the Unity-6-only assertion in the staged Material tests.
func shim2022(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const (
		old = `Assert.AreEqual(-1, mat.rawRenderQueue,`
		new = `Assert.AreEqual(mat.shader.renderQueue, mat.renderQueue,`
	)
	if bytes := string(data); strings.Contains(bytes, old) {
		data = []byte(strings.Replace(bytes, old, new, 1))
	}
	return os.WriteFile(path, data, 0o644)
}

// addTestable ensures "com.unity.pipeline" is in manifest.json testables;
// returns "" when the file had no testables section before.
func addTestable(manifestPath string) (string, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse manifest.json: %w", err)
	}
	var existing []string
	had := ""
	if raw, ok := doc["testables"]; ok {
		if err := json.Unmarshal(raw, &existing); err != nil {
			return "", err
		}
		had = strings.Join(existing, ",")
	}
	for _, t := range existing {
		if t == manifestTestable {
			return had, nil // already there
		}
	}
	existing = append(existing, manifestTestable)
	if had != "" {
		// Replace the testables array surgically to preserve formatting of the rest.
		newArr, _ := json.Marshal(existing)
		doc["testables"] = newArr
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return "", err
		}
		return had, os.WriteFile(manifestPath, append(out, '\n'), 0o644)
	}
	// No testables before: append a section before the closing brace.
	s := strings.TrimRight(string(data), "\r\n \t")
	if !strings.HasSuffix(s, "}") {
		return "", fmt.Errorf("unexpected manifest.json shape")
	}
	s = strings.TrimSuffix(s, "}")
	s = strings.TrimRight(s, "\r\n \t")
	if !strings.HasSuffix(s, ",") {
		s += ","
	}
	s += "\n  \"testables\": [\n    \"" + manifestTestable + "\"\n  ]\n}\n"
	return "", os.WriteFile(manifestPath, []byte(s), 0o644)
}

func removeTestable(manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	raw, ok := doc["testables"]
	if !ok {
		return nil
	}
	var existing []string
	if err := json.Unmarshal(raw, &existing); err != nil {
		return err
	}
	var kept []string
	for _, t := range existing {
		if t != manifestTestable {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(existing) {
		return nil // ours wasn't there
	}
	if len(kept) == 0 {
		delete(doc, "testables")
	} else {
		newArr, _ := json.Marshal(kept)
		doc["testables"] = newArr
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, append(out, '\n'), 0o644)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return os.WriteFile(dst, data, 0o644)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		dir = filepath.Dir(dir)
	}
	return "", fmt.Errorf("repo root not found — run from within the unity-pipe repo")
}
