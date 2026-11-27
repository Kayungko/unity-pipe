package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
		Synopsis: "execute a server command: upipe exec <name> [--set k=v ...] [--stdin] [--stdout | --job]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe exec")
			sets := multiFlag{}
			fs.Var(&sets, "set", "command parameter k=v (repeatable; JSON-scalar values keep their type)")
			paramsJSON := fs.String("params", "", "command parameters as a JSON object (merged after --set)")
			job := fs.Bool("job", false, "submit as a detached job; prints the jobId handle")
			stdin := fs.Bool("stdin", false, "pass up to 512 KiB of raw stdin to a byte[] stdin parameter")
			stdout := fs.Bool("stdout", false, "write raw binary command output to stdout")
			logs := attachCommandLogFlags(fs)
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("exec: " + err.Error())
			}
			if len(rest) != 1 {
				return nil, usageErr("exec: exactly one command name expected, e.g. upipe exec editor_status")
			}
			if err := logs.validate(); err != nil {
				return nil, err
			}
			if *stdout && (rt.Opts.JSONOut || *job) {
				return nil, usageErr("exec: --stdout cannot be combined with --json or --job (use job --stdout to read a detached result)")
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
				if !json.Valid([]byte(*paramsJSON)) {
					return nil, usageErr("exec: --params is not valid JSON")
				}
				var extra map[string]any
				decoder := json.NewDecoder(strings.NewReader(*paramsJSON))
				decoder.UseNumber()
				if err := decoder.Decode(&extra); err != nil {
					return nil, usageErr(fmt.Sprintf("exec: --params is not a JSON object: %v", err))
				}
				for k, v := range extra {
					params[k] = v
				}
			}

			opts := protocol.ExecOptions{Parameters: params, Job: *job, BinaryIO: *stdin || *stdout}
			if *stdin {
				if _, exists := params["stdin"]; exists {
					return nil, usageErr("exec: --stdin cannot be combined with a stdin parameter")
				}
				if err := requireBinaryStdin(rt.Client, name); err != nil {
					return nil, err
				}
				input, err := readBinaryStdin(os.Stdin)
				if err != nil {
					return nil, err
				}
				opts.Stdin = &input
			}
			if rt.Opts.Timeout > 0 {
				opts.Timeout = rt.Opts.Timeout
			}
			// A lost response may already have registered a wait and its capture.
			if name == "wait_start" && !opts.BinaryIO {
				return rt.Client.Exec(name, opts)
			}
			resp, err := rt.Client.ExecReloadTolerant(name, opts, 0)
			if err != nil {
				logs.write(protocol.AsUpipeError(err).Logs)
				return nil, err
			}
			logs.write(resp.Logs)
			resp.Logs = nil
			if *stdout {
				return decodeBinaryOutput(resp.Result)
			}
			return resp, nil
		},
	}
}

type commandLogFlags struct {
	level  string
	format string
}

func attachCommandLogFlags(fs *flag.FlagSet) *commandLogFlags {
	settings := &commandLogFlags{}
	fs.StringVar(&settings.level, "log-level", "info", "minimum command log level: debug | info | warning | error")
	fs.StringVar(&settings.format, "log-format", "text", "stderr command log format: text | json")
	return settings
}

var commandLogLevels = map[string]int{"debug": 1, "info": 2, "warning": 3, "error": 4}

func (settings *commandLogFlags) validate() error {
	if commandLogLevels[settings.level] == 0 {
		return usageErr("--log-level must be debug, info, warning, or error")
	}
	if settings.format != "text" && settings.format != "json" {
		return usageErr("--log-format must be text or json")
	}
	return nil
}

func (settings *commandLogFlags) write(logs []protocol.CommandLog) {
	if len(logs) == 0 {
		return
	}
	encoder := json.NewEncoder(os.Stderr)
	minimum := commandLogLevels[settings.level]
	for _, log := range logs {
		if log.Dropped == 0 && commandLogLevels[log.Level] < minimum {
			continue
		}
		if settings.format == "json" {
			_ = encoder.Encode(log)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", log.Level, log.Message)
		}
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
