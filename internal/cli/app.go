// Package cli is the entry layer: argument dispatch, envelope rendering and
// exit-code propagation. It owns no business logic — it wires options,
// transport and commands together and formats their results.
package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/commands"
	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
	"github.com/Kayungko/unity-pipe/internal/transport"
)

// Main is the program body; it returns the process exit code.
//
// Usage shape: upipe [global flags] <subcommand> [subcommand flags] [args]
// Shared flags are accepted both before and after the subcommand.
func Main(argv []string) int {
	opts := &options.Options{}

	global := flag.NewFlagSet("upipe", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	options.Attach(global, opts)
	// Any usage rendering (bare `upipe`, --help, bad global flags) shows the
	// full command table, not just the shared flag defaults.
	global.Usage = func() { printUsage(global) }
	if err := global.Parse(argv); err != nil {
		return protocol.ExitUsage
	}
	if global.NArg() < 1 {
		printUsage(global)
		return protocol.ExitUsage
	}

	name := global.Arg(0)
	sub, ok := lookup(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "upipe: unknown command %q\n\n", name)
		printUsage(global)
		return protocol.ExitUsage
	}

	// A fresh options copy for the subcommand pass (global values already set
	// remain as defaults; re-specifying after the subcommand overrides them).
	subOpts := *opts
	rt := &commands.Runtime{Opts: &subOpts}

	// `upipe <cmd> --help` short-circuits before instance resolution: the
	// command's Run builds its real flag set, the ErrHelp path renders the
	// usage (see newFlagSet), and no live editor is required to read help.
	if hasHelpFlag(global.Args()[1:]) {
		_, err := sub.Run(rt, global.Args()[1:])
		return render(err, &subOpts)
	}

	// Pre-parse the shared flags out of the subcommand arguments BEFORE the
	// runtime is constructed: newRuntime resolves the project from
	// opts.ProjectPath, so a `upipe status --project-path X` (flag after the
	// subcommand — the documented shape) must land in subOpts first. Each
	// command's Run re-parses its own flag set; a shared pre-parse that only
	// consumes known shared flags keeps that path untouched.
	preFS := flag.NewFlagSet("upipe "+name, flag.ContinueOnError)
	preFS.SetOutput(os.Stderr)
	options.Attach(preFS, &subOpts)
	// Reorder so interspersed shared flags parse: hoist flags, keep the rest.
	rest := reorderForFlags(preFS, global.Args()[1:])
	_ = preFS.Parse(rest)

	if !sub.NoRuntime {
		r, err := newRuntime(&subOpts)
		if err != nil {
			return render(err, &subOpts)
		}
		rt = r
	}
	defer rt.Close()

	data, err := sub.Run(rt, global.Args()[1:])
	if err != nil {
		return render(err, &subOpts)
	}
	// A successful command proves this machine touched the project; register
	// it (best-effort, deduplicated) so `upipe instances` can find it later.
	// NoRuntime commands carry an empty ProjectPath and are skipped inside.
	commands.RegisterProject(rt.ProjectPath)
	return render(nil, &subOpts, data)
}

// hasHelpFlag reports whether args contain a help request (-h/--help) before
// the "--" terminator — the tokens the flag package answers with ErrHelp.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "-h" || a == "-help" || a == "--h" || a == "--help" {
			return true
		}
	}
	return false
}

func lookup(name string) (commands.SubCommand, bool) {
	for _, sc := range commands.All() {
		if sc.Name == name {
			return sc, true
		}
		for _, a := range sc.Aliases {
			if a == name {
				return sc, true
			}
		}
	}
	return commands.SubCommand{}, false
}

// newRuntime resolves the project, builds the transport and client stack.
func newRuntime(opts *options.Options) (*commands.Runtime, error) {
	project, err := commands.ResolveProjectPath(opts.ProjectPath)
	if err != nil {
		return nil, err
	}
	opts.ProjectPath = project
	tr := transport.New()
	source := commands.PortFileSource(project)
	client := &protocol.PipelineClient{Transport: tr, Source: source}
	return &commands.Runtime{Opts: opts, Transport: tr, Client: client, ProjectPath: project}, nil
}

func printUsage(fs *flag.FlagSet) {
	var b strings.Builder
	b.WriteString("upipe — thin CLI for the Unity Pipeline HTTP API (self-maintained, no unity binary)\n\n")
	b.WriteString("Usage:\n  upipe [global flags] <command> [flags] [args]\n\nCommands:\n")
	for _, sc := range commands.All() {
		aliases := ""
		if len(sc.Aliases) > 0 {
			aliases = " (" + strings.Join(sc.Aliases, ", ") + ")"
		}
		fmt.Fprintf(&b, "  %-12s%s %s\n", sc.Name, aliases, sc.Synopsis)
	}
	b.WriteString("\nGlobal flags (also accepted per command):\n")
	fs.SetOutput(&nopWriteCloser{&b})
	fs.PrintDefaults()
	fs.SetOutput(os.Stderr)
	fmt.Fprint(os.Stderr, b.String())
}

type nopWriteCloser struct{ b *strings.Builder }

func (w *nopWriteCloser) Write(p []byte) (int, error) { return w.b.Write(p) }

// reorderForFlags hoists SHARED flag tokens (and their values for value-taking
// flags, looked up in fs) in front of the positional arguments so a single
// flag.Parse can see them. Flags unknown to fs (subcommand-specific flags
// like exec's --set) stay in their original position — the subcommand's own
// flag pass handles them.
func reorderForFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				if fs.Lookup(name[:strings.Index(name, "=")]) != nil {
					flags = append(flags, a)
				} else {
					positional = append(positional, a)
				}
				continue
			}
			fl := fs.Lookup(name)
			if fl == nil {
				positional = append(positional, a) // subcommand's own flag
				continue
			}
			if !isBoolFlag(fl) && i+1 < len(args) {
				flags = append(flags, a, args[i+1])
				i++
				continue
			}
			flags = append(flags, a)
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}

func isBoolFlag(fl *flag.Flag) bool {
	bf, ok := fl.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
