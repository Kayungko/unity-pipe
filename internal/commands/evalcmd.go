package commands

import (
	"os"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// EvalCommand evaluates C# code in the editor (server command `eval`).
func EvalCommand() SubCommand {
	return SubCommand{
		Name:     "eval",
		Synopsis: "evaluate C# code in the running editor: upipe eval \"return 2 + 2;\"",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe eval")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("eval: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("eval: exactly one code argument expected (quote it in the shell)")
			}
			return rt.execSimple("eval", map[string]any{"code": rest[0]})
		},
	}
}

// EvalFileCommand evaluates a C# file in the editor (server command `eval_file`,
// parameter name is `file`).
func EvalFileCommand() SubCommand {
	return SubCommand{
		Name:     "eval-file",
		Aliases:  []string{"eval_file"},
		Synopsis: "evaluate a C# file in the running editor: upipe eval-file Assets/Scratch.cs",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe eval-file")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("eval-file: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("eval-file: exactly one file path expected")
			}
			path := rest[0]
			if _, err := os.Stat(path); err != nil {
				return nil, usageErr("eval-file: " + err.Error())
			}
			return rt.execSimple("eval_file", map[string]any{"file": path})
		},
	}
}

// execSimple runs one server command reload-tolerantly with the shared timeout.
func (rt *Runtime) execSimple(name string, params map[string]any) (any, error) {
	opts := protocol.ExecOptions{Parameters: params}
	if rt.Opts.Timeout > 0 {
		opts.Timeout = rt.Opts.Timeout
	}
	return rt.Client.ExecReloadTolerant(name, opts, 0)
}
