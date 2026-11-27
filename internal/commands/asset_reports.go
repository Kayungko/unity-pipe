package commands

import (
	"context"
	"flag"

	"github.com/Kayungko/unity-pipe/internal/assetversion"
)

func parseAssetReport(fs *flag.FlagSet, args []string, under *string) error {
	positions, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positions) != 0 {
		return usageErr(fs.Name() + ": unexpected positional arguments")
	}
	if !assetversion.ValidReferenceScope(*under) {
		return usageErr(fs.Name() + ": invalid --under path")
	}
	return nil
}

func AssetCheckCommand() SubCommand {
	return SubCommand{
		Name:      "asset-check",
		Synopsis:  "check Unity asset metadata and serialized references: upipe asset-check --at REV",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-check")
			at := fs.String("at", "", "Git revision or submitted P4 changelist")
			under := fs.String("under", "", "limit integrity findings to a project-relative path")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			if err := parseAssetReport(fs, args, under); err != nil {
				return nil, err
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
			result, err := version.CheckIntegrity(ctx, *under)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			return result, nil
		},
	}
}

func AssetImpactCommand() SubCommand {
	return SubCommand{
		Name:      "asset-impact",
		Synopsis:  "find affected Prefabs and Scenes across revisions: upipe asset-impact --from OLD --to NEW (or --at REV)",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-impact")
			at := fs.String("at", "", "compare a Git commit or submitted P4 changelist against its predecessor")
			from := fs.String("from", "", "older Git revision or submitted P4 changelist")
			to := fs.String("to", "", "newer Git revision or submitted P4 changelist")
			under := fs.String("under", "", "limit the Prefab and Scene reference graph to a project-relative path")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			if err := parseAssetReport(fs, args, under); err != nil {
				return nil, err
			}
			if *at != "" {
				if *from != "" || *to != "" {
					return nil, usageErr("asset-impact: use --at or --from and --to")
				}
				if err := source.validate(*at); err != nil {
					return nil, err
				}
			} else if err := source.validate(*from, *to); err != nil {
				return nil, err
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			result, err := compareAssetRevisions(ctx, source, rt, *at, *from, *to, *under)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			return result, nil
		},
	}
}

func compareAssetRevisions(ctx context.Context, source *assetSourceFlags, rt *Runtime, at, from, to, under string) (assetversion.AssetImpactResult, error) {
	if at != "" {
		to = at
	}
	after, err := source.open(ctx, rt, to)
	if err != nil {
		return assetversion.AssetImpactResult{}, err
	}
	var before *assetversion.Version
	switch {
	case at == "":
		before, err = source.open(ctx, rt, from)
	case source.p4.Root != "":
		before, err = assetversion.PreviousP4(ctx, source.p4, after)
	default:
		before, err = assetversion.PreviousGit(ctx, rt.Opts.ProjectPath, after)
	}
	if err != nil {
		return assetversion.AssetImpactResult{}, err
	}
	return assetversion.Impact(ctx, before, after, under)
}
