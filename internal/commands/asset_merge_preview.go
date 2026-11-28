package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/assetversion"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func AssetMergePreviewCommand() SubCommand {
	return SubCommand{
		Name: "asset-merge-preview", NoRuntime: true,
		Synopsis: "preview a Prefab or Scene merge with UnityYAMLMerge without editing the project",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-merge-preview")
			flags := attachConflictFlags(fs)
			tool := fs.String("unity-yaml-merge", "", "path to the UnityYAMLMerge executable from your Editor installation")
			output := fs.String("output-dir", os.TempDir(), "existing parent directory outside the Unity project (default: system temp)")
			attachShared(fs, rt.Opts)
			positional, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(positional) != 1 || !assetversion.ValidUnityPath(positional[0]) || *tool == "" {
				return nil, usageErr("asset-merge-preview: provide --unity-yaml-merge and a project-relative .prefab or .unity")
			}
			executable, err := filepath.Abs(*tool)
			if err != nil {
				return nil, usageErr("asset-merge-preview: invalid UnityYAMLMerge path")
			}
			parent, err := mergeOutputParent(*output, rt.Opts.ProjectPath)
			if err != nil {
				return nil, usageErr("asset-merge-preview: " + err.Error())
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			versions, roots, err := flags.open(ctx, rt)
			if err != nil {
				return nil, err
			}
			result, err := assetversion.PreviewMerge(ctx, versions, positional[0], executable, parent)
			result.Analysis.Base.P4Root, result.Analysis.Ours.P4Root, result.Analysis.Theirs.P4Root = roots[0], roots[1], roots[2]
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					return nil, protocol.ErrTimeout("UnityYAMLMerge preview timed out")
				}
				return nil, &protocol.UpipeError{Code: protocol.ExitCommandFailed,
					Kind: "CommandFailed", Message: err.Error(), Data: result}
			}
			return result, nil
		},
	}
}

func mergeOutputParent(output, project string) (string, error) {
	parent, err := filepath.Abs(output)
	if err != nil {
		return "", err
	}
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		return "", fmt.Errorf("--output-dir must be an existing directory: %w", err)
	}
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", fmt.Errorf("--output-dir must be an existing directory")
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return "", err
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		return "", fmt.Errorf("could not resolve the project directory: %w", err)
	}
	if relative, err := filepath.Rel(project, parent); err == nil &&
		relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("--output-dir must be outside the project")
	}
	return parent, nil
}
