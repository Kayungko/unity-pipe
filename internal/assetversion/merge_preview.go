package assetversion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

type MergePreviewResult struct {
	Status              string              `json:"status"`
	Analysis            AssetConflictResult `json:"analysis"`
	ToolExitCode        *int                `json:"toolExitCode,omitempty"`
	NativeReport        string              `json:"nativeReport,omitempty"`
	NativeReportTrimmed bool                `json:"nativeReportTruncated,omitempty"`
	Candidate           *MergeCandidate     `json:"candidate,omitempty"`
}

type MergeCandidate struct {
	Path              string       `json:"path"`
	SHA256            string       `json:"sha256"`
	ChangesFromOurs   []NodeChange `json:"changesFromOurs"`
	ChangesFromTheirs []NodeChange `json:"changesFromTheirs"`
}

// PreviewMerge runs the Editor's standalone merge tool on isolated copies of
// the three snapshots. The candidate is exported only after parsing it again.
func PreviewMerge(ctx context.Context, versions [3]*Version, name, tool, outputParent string) (MergePreviewResult, error) {
	result := MergePreviewResult{Status: "failed"}
	if !ValidUnityPath(name) {
		return result, fmt.Errorf("expected a project-relative Prefab or Scene path")
	}
	var sides [3]conflictTree
	for i, version := range versions {
		side, err := readConflictTree(ctx, version, name)
		if err != nil {
			return result, err
		}
		sides[i] = side
	}
	analysis, err := explainConflictTrees(ctx, sides, name)
	result.Analysis = analysis
	if err != nil {
		return result, err
	}
	for _, side := range sides {
		if side.snapshot.Exists {
			continue
		}
		if analysis.HasConflicts {
			result.Status = "conflicted"
			return result, nil
		}
		return result, fmt.Errorf("UnityYAMLMerge requires a text asset in all three snapshots")
	}

	directory, err := os.MkdirTemp(outputParent, "upipe-merge-preview-")
	if err != nil {
		return result, err
	}
	defer func() {
		if result.Candidate == nil {
			_ = os.RemoveAll(directory)
		}
	}()
	work := filepath.Join(directory, "work")
	if err := os.Mkdir(work, 0700); err != nil {
		return result, err
	}
	defer os.RemoveAll(work)
	extension := path.Ext(name)
	inputs := [3]string{"base" + extension, "ours" + extension, "theirs" + extension}
	for i, side := range sides {
		if err := os.WriteFile(filepath.Join(work, inputs[i]), side.data, 0600); err != nil {
			return result, err
		}
	}
	output := "merged" + extension
	const reportName = "conflicts.txt"
	cmd := exec.CommandContext(ctx, tool, "merge", "-h", "-p", "--fallback", "none", "-o", reportName,
		inputs[0], inputs[2], inputs[1], output)
	cmd.Dir = work
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ToolExitCode = &code
	}
	report, err := readMergeOutput(filepath.Join(work, reportName), 64<<10)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("read UnityYAMLMerge report: %w", err)
	}
	if len(report) > 64<<10 {
		result.NativeReportTrimmed = true
		report = report[:64<<10]
	}
	result.NativeReport = strings.TrimSpace(string(report))
	if result.NativeReport == "Conflicts:\nConflict handling:" {
		result.NativeReport = ""
	}
	if runErr != nil {
		var exit *exec.ExitError
		if errors.As(runErr, &exit) && exit.ExitCode() == 2 && result.NativeReport != "" {
			result.Status = "conflicted"
			return result, nil
		}
		return result, fmt.Errorf("UnityYAMLMerge failed: %w", runErr)
	}
	if analysis.HasConflicts {
		result.Status = "conflicted"
		return result, nil
	}

	merged, err := readMergeOutput(filepath.Join(work, output), maxFileBytes)
	if err != nil {
		return result, fmt.Errorf("read UnityYAMLMerge candidate: %w", err)
	}
	if len(merged) > maxFileBytes {
		return result, fmt.Errorf("UnityYAMLMerge candidate exceeds the supported file size")
	}
	docs, err := parseUnity(merged)
	if err != nil {
		return result, fmt.Errorf("invalid UnityYAMLMerge candidate: %w", err)
	}
	candidate := MergeCandidate{Path: filepath.Join(directory, "candidate"+extension), SHA256: fmt.Sprintf("%x", sha256.Sum256(merged))}
	diffs := [2]*[]NodeChange{&candidate.ChangesFromOurs, &candidate.ChangesFromTheirs}
	for i, side := range sides[1:] {
		// Other assets weren't merged. Use the same reference context on
		// both ends so the readback measures this file's serialized changes.
		delta, err := compareDocuments(
			documentSide{path: name, index: side.index, docs: side.docs},
			documentSide{path: name, index: side.index, docs: docs})
		if err != nil {
			return result, fmt.Errorf("compare UnityYAMLMerge candidate: %w", err)
		}
		*diffs[i] = delta
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.Rename(filepath.Join(work, output), candidate.Path); err != nil {
		return result, err
	}
	result.Status, result.Candidate = "merged", &candidate
	return result, nil
}

func readMergeOutput(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit+1))
}
