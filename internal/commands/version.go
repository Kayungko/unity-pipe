package commands

// Version is the upipe CLI version, injected at build time via
// -ldflags "-X github.com/Kayungko/unity-pipe/internal/commands.Version=v0.1.4".
// The dev placeholder marks an untagged local build.
var Version = "dev"

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
				"vendoredPkg": VendoredPkgRef,
				"pkgSource":   VendoredPkgSource,
			}, nil
		},
	}
}
