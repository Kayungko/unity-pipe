package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// SubCommand is one upipe subcommand. Run returns either a data payload
// (rendered by the cli layer per --json/human mode) or an error carrying an
// exit code (protocol.UpipeError).
type SubCommand struct {
	Name     string
	Aliases  []string
	Synopsis string
	// NoRuntime marks commands that need neither a running editor nor a
	// project (e.g. plugin install) — the cli layer skips instance resolution.
	NoRuntime bool
	Run       func(rt *Runtime, args []string) (any, error)
}

// All returns the full subcommand registry in help order.
func All() []SubCommand {
	return []SubCommand{
		VersionCommand(),
		InstancesCommand(),
		StatusCommand(),
		CommandsCommand(),
		ExecCommand(),
		EvalCommand(),
		EvalFileCommand(),
		RecompileCommand(),
		TestCommand(),
		AuditCommand(),
		JobCommand(),
		JobCancelCommand(),
		ProgressCommand(),
		PkgStatusCommand(),
		PkgInstallCommand(),
		PluginCommand(),
	}
}

// mustMarshal is a non-failing marshal for values that are known-serializable.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// jsonRaw is a trivial conversion helper for raw bodies.
func jsonRaw(b []byte) json.RawMessage { return json.RawMessage(b) }

// attachShared registers the shared option flags on a subcommand flag set.
func attachShared(fs *flag.FlagSet, o *options.Options) {
	options.Attach(fs, o)
}

// nullWriter silences flag package usage output (the cli layer renders its own).
type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return io.Discard.Write(p) }

// newFlagSet builds a flag set for a subcommand. Parse errors (and the
// ErrHelp triggered by --help/-h) still print nothing via the flag package's
// own channel, but the Usage hook renders the command's real usage on stderr:
// synopsis from the registry plus the flags actually registered below.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(nullWriter{})
	fs.Usage = func() { printSubUsage(os.Stderr, fs) }
	return fs
}

// printSubUsage renders one subcommand's usage: the registry entry matching
// the flag-set name (synopsis) and the flags registered on fs at call time.
func printSubUsage(w io.Writer, fs *flag.FlagSet) {
	name := strings.TrimPrefix(fs.Name(), "upipe ")
	sc, ok := lookupSubCommand(name)
	fmt.Fprintf(w, "Usage: upipe %s [flags] [args]\n", name)
	if ok && sc.Synopsis != "" {
		fmt.Fprintf(w, "\n%s\n", sc.Synopsis)
	}
	fmt.Fprint(w, "\nFlags:\n")
	fs.SetOutput(w)
	fs.PrintDefaults()
}

// lookupSubCommand resolves a flag-set name (e.g. "plugin install") to its
// registry entry, falling back to the parent command name.
func lookupSubCommand(name string) (SubCommand, bool) {
	if sc, ok := matchSubCommand(name); ok {
		return sc, true
	}
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return matchSubCommand(name[:i])
	}
	return SubCommand{}, false
}

func matchSubCommand(name string) (SubCommand, bool) {
	for _, sc := range All() {
		if sc.Name == name {
			return sc, true
		}
		for _, a := range sc.Aliases {
			if a == name {
				return sc, true
			}
		}
	}
	return SubCommand{}, false
}

// usageErr wraps a flag/usage failure as exit code 2.
func usageErr(msg string) *protocol.UpipeError {
	return &protocol.UpipeError{Code: protocol.ExitUsage, Kind: "Usage", Message: msg}
}

// parseArgs reorders interspersed arguments so the stdlib flag package can
// parse them: Go's flag stops at the first positional, but upipe accepts the
// natural `upipe exec <name> --set k=v` shape. Flags (and their values, looked
// up via the flag set's registry) are hoisted in front; positionals keep
// their relative order and are returned for the command to consume.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
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
				flags = append(flags, a)
				continue
			}
			fl := fs.Lookup(name)
			if fl != nil && !isBoolValue(fl.Value) && i+1 < len(args) {
				flags = append(flags, a, args[i+1])
				i++
				continue
			}
			// Bool flag, unknown flag (error surfaces from fs.Parse), or a
			// value-taking flag missing its value (ditto).
			flags = append(flags, a)
			continue
		}
		positional = append(positional, a)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

// isBoolValue reports whether a flag value is boolean (no separate value token).
func isBoolValue(v flag.Value) bool {
	bf, ok := v.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
