package commands

import "github.com/Kayungko/unity-pipe/internal/assetversion"

func AssetConflictCommand() SubCommand {
	return SubCommand{
		Name:      "asset-conflict",
		Synopsis:  "explain a three-way Prefab or Scene comparison: upipe asset-conflict --base REV --ours REV --theirs REV ASSET",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-conflict")
			base := fs.String("base", "", "base Git revision or submitted P4 changelist")
			ours := fs.String("ours", "", "our Git revision or submitted P4 changelist")
			theirs := fs.String("theirs", "", "their Git revision or submitted P4 changelist")
			baseRoot := fs.String("base-p4-root", "", "override the P4 project root for the base snapshot")
			oursRoot := fs.String("ours-p4-root", "", "override the P4 project root for our snapshot")
			theirsRoot := fs.String("theirs-p4-root", "", "override the P4 project root for their snapshot")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			paths, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(paths) != 1 || !assetversion.ValidUnityPath(paths[0]) {
				return nil, usageErr("asset-conflict: provide one project-relative .prefab or .unity path under Assets or Packages")
			}
			if err := source.validate(*base, *ours, *theirs); err != nil {
				return nil, err
			}
			roots := [3]string{*baseRoot, *oursRoot, *theirsRoot}
			for _, root := range roots {
				if root != "" && (source.p4.Root == "" || !assetversion.ValidP4Root(root)) {
					return nil, usageErr("asset-conflict: per-side P4 roots require --p4-root and exact depot directories")
				}
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			var versions [3]*assetversion.Version
			for i, rev := range [3]string{*base, *ours, *theirs} {
				side := *source
				if roots[i] == "" {
					roots[i] = source.p4.Root
				}
				side.p4.Root = roots[i]
				versions[i], err = side.open(ctx, rt, rev)
				if err != nil {
					return nil, assetError(ctx, err)
				}
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
