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
	rt, err := newRuntime(&subOpts)
	if err != nil {
		return render(err, &subOpts)
	}
	defer rt.Close()

	data, err := sub.Run(rt, global.Args()[1:])
	if err != nil {
		return render(err, &subOpts)
	}
	return render(nil, &subOpts, data)
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
