package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kayungko/unity-pipe/internal/assetversion"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

type assetSourceFlags struct {
	p4 assetversion.P4Options
}

func attachAssetSource(fs *flag.FlagSet) *assetSourceFlags {
	source := &assetSourceFlags{}
	fs.StringVar(&source.p4.Root, "p4-root", "", "project root in a P4 depot; use submitted changelists as revisions")
	fs.StringVar(&source.p4.Script, "p4-ops", os.Getenv("UPIPE_P4_OPS"), "path to the installed P4 Suite p4-ops.js bridge")
	fs.StringVar(&source.p4.Port, "p4-port", "", "explicit P4 server")
	fs.StringVar(&source.p4.User, "p4-user", "", "explicit P4 user")
	fs.StringVar(&source.p4.Client, "p4-client", "", "explicit P4 client")
	return source
}

func (source *assetSourceFlags) validate(revisions ...string) error {
	for _, revision := range revisions {
		if revision == "" {
			return usageErr("a revision is required")
		}
	}
	if source.p4.Root == "" {
		if source.p4.Port != "" || source.p4.User != "" || source.p4.Client != "" {
			return usageErr("P4 connection flags require --p4-root")
		}
		return nil
	}
	if !assetversion.ValidP4Root(source.p4.Root) {
		return usageErr("--p4-root must name an exact depot directory, e.g. //depot/Project")
	}
	if source.p4.Script == "" || source.p4.Port == "" || source.p4.User == "" || source.p4.Client == "" {
		return usageErr("P4 requires --p4-ops, --p4-port, --p4-user and --p4-client")
	}
	for _, revision := range revisions {
		if !assetversion.ValidChangelist(revision) {
			return usageErr("use a numeric submitted P4 changelist as the revision")
		}
	}
	var err error
	source.p4.Script, err = filepath.Abs(source.p4.Script)
	if err != nil {
		return usageErr("invalid --p4-ops path")
	}
	return nil
}

func (source *assetSourceFlags) open(ctx context.Context, rt *Runtime, rev string) (*assetversion.Version, error) {
	if source.p4.Root != "" {
		return assetversion.OpenP4(ctx, source.p4, rev)
	}
	return assetversion.OpenGit(ctx, rt.Opts.ProjectPath, rev)
}

func assetDeadline(rt *Runtime) (context.Context, context.CancelFunc) {
	timeout := rt.Opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return context.WithTimeout(context.Background(), timeout)
}

func assetError(ctx context.Context, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return protocol.ErrTimeout("versioned asset read timed out")
	}
	return protocol.ErrCommandFailed(&protocol.ExecResponse{Error: err.Error()})
}

func AssetGUIDCommand() SubCommand {
	return SubCommand{
		Name:      "asset-guid",
		Synopsis:  "resolve Unity asset GUIDs in a Git revision or submitted P4 changelist: upipe asset-guid --at REV GUID [GUID ...]",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-guid")
			at := fs.String("at", "", "Git revision or submitted P4 changelist")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			guids, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(guids) == 0 {
				return nil, usageErr("asset-guid: provide one or more Unity GUIDs")
			}
			for _, guid := range guids {
				if !assetversion.ValidGUID(guid) {
					return nil, usageErr(fmt.Sprintf("asset-guid: invalid Unity GUID %q", guid))
				}
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
			index, err := version.GUIDs(ctx)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			results := make([]assetversion.Resolution, 0, len(guids))
			for _, guid := range guids {
				results = append(results, index.Resolve(guid))
			}
			return struct {
				Version assetversion.Identity     `json:"version"`
				Results []assetversion.Resolution `json:"results"`
			}{version.Identity, results}, nil
		},
	}
}

func AssetRefsCommand() SubCommand {
	return SubCommand{
		Name:      "asset-refs",
		Synopsis:  "find Prefab and Scene references to a Unity GUID in a fixed revision: upipe asset-refs --at REV GUID",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-refs")
			at := fs.String("at", "", "Git revision or submitted P4 changelist")
			under := fs.String("under", "", "restrict the search to a project-relative folder or asset under Assets or Packages")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			guids, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(guids) != 1 || !assetversion.ValidGUID(guids[0]) {
				return nil, usageErr("asset-refs: provide one Unity GUID")
			}
			if !assetversion.ValidReferenceScope(*under) {
				return nil, usageErr("asset-refs: invalid --under path")
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
			result, err := version.FindReferences(ctx, guids[0], *under)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			return result, nil
		},
	}
}

func AssetDiffCommand() SubCommand {
	return SubCommand{
		Name:      "asset-diff",
		Synopsis:  "compare a text-serialized Unity Prefab or Scene across two snapshots: upipe asset-diff --from REV --to REV PATH",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe asset-diff")
			from := fs.String("from", "", "older Git revision or submitted P4 changelist")
			to := fs.String("to", "", "newer Git revision or submitted P4 changelist")
			toPath := fs.String("to-path", "", "path in the newer snapshot if the asset itself moved")
			includeInherited := fs.Bool("include-inherited", false, "compare effective fields inherited from nested Prefab sources")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			paths, err := parseArgs(fs, args)
			if err != nil {
				return nil, err
			}
			if len(paths) != 1 || !assetversion.ValidUnityPath(paths[0]) {
				return nil, usageErr("asset-diff: provide one project-relative .prefab or .unity path under Assets or Packages")
			}
			if *toPath == "" {
				*toPath = paths[0]
			}
			if !assetversion.ValidUnityPath(*toPath) {
				return nil, usageErr("asset-diff: invalid --to-path")
			}
			if err := source.validate(*from, *to); err != nil {
				return nil, err
			}
			ctx, cancel := assetDeadline(rt)
			defer cancel()
			before, err := source.open(ctx, rt, *from)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			after, err := source.open(ctx, rt, *to)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			compare := assetversion.Diff
			if *includeInherited {
				compare = assetversion.DiffWithInheritance
			}
			diff, err := compare(ctx, before, after, paths[0], *toPath)
			if err != nil {
				return nil, assetError(ctx, err)
			}
			return diff, nil
		},
	}
}
