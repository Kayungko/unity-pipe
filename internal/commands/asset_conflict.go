package commands

import (
	"context"
	"flag"

	"github.com/Kayungko/unity-pipe/internal/assetversion"
)

type conflictFlags struct {
	revisions [3]*string
	roots     [3]*string
	source    *assetSourceFlags
}

func attachConflictFlags(fs *flag.FlagSet) conflictFlags {
	var flags conflictFlags
	for i, side := range []string{"base", "ours", "theirs"} {
		flags.revisions[i] = fs.String(side, "", side+" Git revision or submitted P4 changelist")
		flags.roots[i] = fs.String(side+"-p4-root", "", "override the P4 project root for the "+side+" snapshot")
	}
	flags.source = attachAssetSource(fs)
	return flags
}

func (flags conflictFlags) open(ctx context.Context, rt *Runtime) ([3]*assetversion.Version, [3]string, error) {
	var versions [3]*assetversion.Version
	var roots [3]string
	if err := flags.source.validate(*flags.revisions[0], *flags.revisions[1], *flags.revisions[2]); err != nil {
		return versions, roots, err
	}
	for i, root := range flags.roots {
		roots[i] = *root
		if roots[i] == "" {
			roots[i] = flags.source.p4.Root
		} else if flags.source.p4.Root == "" || !assetversion.ValidP4Root(roots[i]) {
			return versions, roots, usageErr("per-side P4 roots require --p4-root and exact depot directories")
		}
	}
	for i, revision := range flags.revisions {
		side := *flags.source
		side.p4.Root = roots[i]
		version, err := side.open(ctx, rt, *revision)
		if err != nil {
			return versions, roots, assetError(ctx, err)
		}
		versions[i] = version
	}
	return versions, roots, nil
}

func AssetConflictCommand() SubCommand {
	return SubCommand{
		Name:      "asset-conflict",
		Synopsis:  "explain a three-way Prefab or Scene comparison: upipe asset-conflict --base REV --ours REV --theirs REV ASSET",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-conflict")
			flags := attachConflictFlags(fs)
			attachShared(fs, rt.Opts)
			paths, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(paths) != 1 || !assetversion.ValidUnityPath(paths[0]) {
				return nil, usageErr("asset-conflict: provide one project-relative .prefab or .unity path under Assets or Packages")
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			versions, roots, err := flags.open(ctx, rt)
			if err != nil {
				return nil, err
			}
			result, err := assetversion.ExplainConflict(ctx, versions[0], versions[1], versions[2], paths[0])
			if err != nil {
				return nil, assetError(ctx, err)
			}
			result.Base.P4Root, result.Ours.P4Root, result.Theirs.P4Root = roots[0], roots[1], roots[2]
			return result, nil
		},
	}
}
