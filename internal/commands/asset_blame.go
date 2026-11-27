package commands

import "github.com/Kayungko/unity-pipe/internal/assetversion"

func AssetBlameCommand() SubCommand {
	return SubCommand{
		Name:      "asset-blame",
		Synopsis:  "trace a serialized Prefab or Scene field: upipe asset-blame --at REV --node PATH --component TYPE --field POINTER ASSET",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-blame")
			at := fs.String("at", "", "Git revision or submitted P4 changelist")
			limit := fs.Int("limit", 256, "maximum history steps to examine")
			var selector assetversion.FieldSelector
			fs.StringVar(&selector.Node, "node", "", "node path from asset-diff (for example /Root/Same[2])")
			fs.StringVar(&selector.NodeID, "node-id", "", "node local File ID instead of --node")
			fs.StringVar(&selector.Component, "component", "", "serialized component type")
			fs.StringVar(&selector.ComponentID, "component-id", "", "component local File ID instead of --component")
			fs.StringVar(&selector.Field, "field", "", "serialized field JSON Pointer")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			paths, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(paths) != 1 || !assetversion.ValidUnityPath(paths[0]) {
				return nil, usageErr("asset-blame: provide one project-relative .prefab or .unity path under Assets or Packages")
			}
			if !selector.Valid() {
				return nil, usageErr("asset-blame: use --node or --node-id, --component or --component-id, and --field with a JSON Pointer")
			}
			if *limit < 1 || *limit > assetversion.MaxFieldHistoryLimit {
				return nil, usageErr("asset-blame: --limit must be between 1 and 10000")
			}
			if err := source.validate(*at); err != nil {
				return nil, err
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			version, err := source.open(ctx, rt, *at)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			var result assetversion.FieldHistoryResult
			if source.p4.Root != "" {
				result, err = assetversion.TraceP4Field(ctx, source.p4, version, paths[0], selector, *limit)
			} else {
				result, err = assetversion.TraceGitField(ctx, rt.Opts.ProjectPath, version, paths[0], selector, *limit)
			}
			if err != nil {
				return nil, assetError(ctx, err)
			}
			return result, nil
		},
	}
}
