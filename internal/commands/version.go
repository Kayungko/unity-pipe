package commands

// Version is the upipe CLI version, injected at build time via
// -ldflags "-X github.com/Kayungko/unity-pipe/internal/commands.Version=v0.1.12".
// The dev placeholder marks an untagged local build.
var Version = "dev"

// Commit is the git commit hash the binary was built from, injected via
// -ldflags "-X github.com/Kayungko/unity-pipe/internal/commands.Commit=<sha>".
// "unknown" marks a build that did not set this (e.g. a plain `go build`
// during development) — distinct from an empty string so JSON output always
// carries a human-readable placeholder.
var Commit = "unknown"

// BuildDate is the build timestamp (RFC3339 recommended), injected via
// -ldflags "-X github.com/Kayungko/unity-pipe/internal/commands.BuildDate=<timestamp>".
// "unknown" marks a build that did not set this.
var BuildDate = "unknown"

// VendoredPkgRef is the adapted com.unity.pipeline version this repo
// distributes. Single source of truth for pkg-status reporting and the
// version command — bump it together with package/com.unity.pipeline/package.json
// (see package/ADAPTATION-2022.md upgrade flow, step 3).
const VendoredPkgRef = "0.6.0-2022.1"

// VendoredPkgSource is where the adapted package comes from.
const VendoredPkgSource = "github.com/Kayungko/unity-pipe"

// VersionCommand reports the CLI version and the vendored package reference.
func VersionCommand() SubCommand {
	return SubCommand{
		Name:      "version",
		Synopsis:  "print the upipe CLI version and vendored package reference",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			return map[string]any{
				"version":     Version,
				"commit":      Commit,
				"buildDate":   BuildDate,
				"vendoredPkg": VendoredPkgRef,
				"pkgSource":   VendoredPkgSource,
			}, nil
		},
	}
}
