package commands

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// ExecCommand is the main channel: run any server-discovered command by name
// (aliases: `c`, `command`). Parameters are type-coerced from --set strings:
// values parseable as JSON scalars ("true", "3", "1.5", "null") keep their
// type, everything else is a string.
func ExecCommand() SubCommand {
	return SubCommand{
		Name:     "exec",
		Aliases:  []string{"c", "command"},
		Synopsis: "execute a server command: upipe exec <name> [--set k=v ...] [--job]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe exec")
			sets := multiFlag{}
			fs.Var(&sets, "set", "command parameter k=v (repeatable; JSON-scalar values keep their type)")
			paramsJSON := fs.String("params", "", "command parameters as a JSON object (merged after --set)")
			job := fs.Bool("job", false, "submit as a detached job; prints the jobId handle")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("exec: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("exec: exactly one command name expected, e.g. upipe exec editor_status")
			}
			name := rest[0]

			params := map[string]any{}
			for _, kv := range sets {
				k, v, ok := strings.Cut(kv, "=")
				if !ok || k == "" {
					return nil, usageErr(fmt.Sprintf("exec: bad --set %q (expected k=v)", kv))
				}
				params[k] = coerceScalar(v)
			}
			if *paramsJSON != "" {
				var extra map[string]any
				if err := json.Unmarshal([]byte(*paramsJSON), &extra); err != nil {
					return nil, usageErr(fmt.Sprintf("exec: --params is not a JSON object: %v", err))
				}
				for k, v := range extra {
					params[k] = v
				}
			}

			opts := protocol.ExecOptions{Parameters: params, Job: *job}
			if rt.Opts.Timeout > 0 {
				opts.Timeout = rt.Opts.Timeout
			}
			resp, err := rt.Client.ExecReloadTolerant(name, opts, 0)
			if err != nil {
				return nil, err
			}
			return resp, nil
		},
	}
}

// multiFlag collects repeated string flag values.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// coerceScalar converts a CLI string into the JSON-typed value the server
// expects: "true"/"false" → bool, integers/floats → numbers, "null" → nil,
// everything else stays a string.
func coerceScalar(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}
