package commands

import (
	"flag"
	"fmt"
	"os"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// EvalCommand evaluates C# code in the editor (server command `eval`).
func EvalCommand() SubCommand {
	return SubCommand{
		Name: "eval",
		Synopsis: "evaluate C# code in the running editor: upipe eval \"return 2 + 2;\" " +
			"[--eval-timeout <ms>]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe eval")
			evalTimeout := evalTimeoutFlag(fs)
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("eval: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("eval: exactly one code argument expected (quote it in the shell)")
			}
			params := map[string]any{"code": rest[0]}
			if err := applyEvalTimeout("eval", params, *evalTimeout); err != nil {
				return nil, err
			}
			return rt.execSimple("eval", params)
		},
	}
}

// EvalFileCommand evaluates a C# file in the editor (server command `eval_file`,
// parameter name is `file`).
func EvalFileCommand() SubCommand {
	return SubCommand{
		Name:     "eval-file",
		Aliases:  []string{"eval_file"},
		Synopsis: "evaluate a C# file in the running editor: upipe eval-file Assets/Scratch.cs [--eval-timeout <ms>]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe eval-file")
			evalTimeout := evalTimeoutFlag(fs)
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
			params := map[string]any{"file": path}
			if err := applyEvalTimeout("eval-file", params, *evalTimeout); err != nil {
				return nil, err
			}
			return rt.execSimple("eval_file", params)
		},
	}
}

// evalTimeoutFlag registers --eval-timeout: the server-side eval/eval_file
// command timeout in MILLISECONDS (int parameter `timeout`; server default
// 5000ms, max 86400000ms — see CodeEvalCommand.cs). It is deliberately a
// separate flag from the shared --timeout: that one parses Go durations
// ("30s", "2m" — a bare "60000" is rejected), drives the client round-trip
// budget and is sent as the body-level timeout hint, so its name cannot be
// reused for the command-level parameter.
func evalTimeoutFlag(fs *flag.FlagSet) *int {
	return fs.Int("eval-timeout", 0,
		"server-side eval timeout in milliseconds (0 = server default 5000ms); "+
			"independent of the shared --timeout duration budget")
}

// applyEvalTimeout merges the flag value into the command parameters. 0 means
// "not sent" so the server default applies; out-of-range values fail fast as
// usage errors instead of a remote command failure.
func applyEvalTimeout(cmd string, params map[string]any, ms int) error {
	if ms == 0 {
		return nil
	}
	if ms < 0 || ms > 86_400_000 {
		return usageErr(fmt.Sprintf("%s: --eval-timeout must be 1..86400000 ms, got %d", cmd, ms))
	}
	params["timeout"] = ms
	return nil
}

// execSimple runs one server command reload-tolerantly with the shared timeout.
func (rt *Runtime) execSimple(name string, params map[string]any) (any, error) {
	opts := protocol.ExecOptions{Parameters: params}
	if rt.Opts.Timeout > 0 {
		opts.Timeout = rt.Opts.Timeout
	}
	return rt.Client.ExecReloadTolerant(name, opts, 0)
}
