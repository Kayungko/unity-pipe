package commands

import (
	"flag"
	"io"
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
	Run      func(rt *Runtime, args []string) (any, error)
}

// All returns the full subcommand registry in help order.
func All() []SubCommand {
	return []SubCommand{
		StatusCommand(),
		CommandsCommand(),
		ExecCommand(),
	}
}

// attachShared registers the shared option flags on a subcommand flag set.
func attachShared(fs *flag.FlagSet, o *options.Options) {
	options.Attach(fs, o)
}

// nullWriter silences flag package usage output (the cli layer renders its own).
type nullWriter struct{}

func (nullWriter) Write(p []byte) (int, error) { return io.Discard.Write(p) }

// newFlagSet builds a silenced flag set for a subcommand.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(nullWriter{})
	return fs
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
