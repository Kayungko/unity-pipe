// Package options holds the shared command-line options understood by every
// upipe subcommand (attachable to any flag.FlagSet so both
// `upipe --json status` and `upipe status --json` work).
package options

import (
	"flag"
	"time"
)

// Options is the shared runtime configuration of one invocation.
type Options struct {
	ProjectPath string        // explicit --project-path; empty = auto-detect from cwd
	JSONOut     bool          // --json: single-envelope machine output on stdout
	Timeout     time.Duration // --timeout: per-command budget (0 = default)
}

// DefaultTimeout is the overall per-command budget when --timeout is unset.
const DefaultTimeout = 10 * time.Minute

// Attach registers the shared flags on fs, writing into o.
//
// The CURRENT values of o are used as flag defaults: flag registration resets
// the target variable to its default, so attaching the same Options to a
// second flag set (global parse, then subcommand parse) must pass the already
// parsed values as defaults — otherwise the subcommand registration would
// silently wipe what the global pass set.
func Attach(fs *flag.FlagSet, o *Options) {
	fs.StringVar(&o.ProjectPath, "project-path", o.ProjectPath,
		"Unity project root (default: auto-detect by walking up from the current directory)")
	fs.BoolVar(&o.JSONOut, "json", o.JSONOut, "machine-readable single-envelope JSON on stdout")
	fs.DurationVar(&o.Timeout, "timeout", o.Timeout, "per-command budget (e.g. 30s, 2m)")
}
