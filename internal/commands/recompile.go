package commands

import (
	"encoding/json"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// RecompileCommand triggers a script recompile and (by default) polls
// recompile_status through the domain reload until completed/up_to_date.
// Compile errors surface as exit code 20 with the error list in error.data.
func RecompileCommand() SubCommand {
	return SubCommand{
		Name:     "recompile",
		Synopsis: "trigger a script recompile and wait for completion (--no-wait to fire-and-forget)",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe recompile")
			noWait := fs.Bool("no-wait", false, "return right after triggering; poll recompile_status yourself")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("recompile: " + err.Error())
			}

			trigger, err := rt.execSimple("recompile", map[string]any{})
			if err != nil {
				return nil, err
			}
			if *noWait {
				return trigger, nil
			}

			result, err := rt.pollCommand("recompile_status", protocol.PollConfig{
				TerminalOK:   []string{"completed", "up_to_date"},
				TerminalFail: []string{"failed"},
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"status":   result.Status,
				"detail":   json.RawMessage(result.Body),
				"attempts": result.Attempts,
			}, nil
		},
	}
}

// pollCommand polls a server command's JSON status body until terminal.
func (rt *Runtime) pollCommand(command string, cfg protocol.PollConfig) (*protocol.PollResult, error) {
	deadline := cfg.Deadline
	if rt.Opts.Timeout > 0 {
		deadline = rt.Opts.Timeout
	}
	cfg.Deadline = deadline
	return protocol.Poll(func() (protocol.PollStep, error) {
		resp, err := rt.execSimple(command, map[string]any{})
		if err != nil {
			return protocol.PollStep{}, err
		}
		er, ok := resp.(*protocol.ExecResponse)
		if !ok {
			return protocol.PollStep{}, protocol.ErrProtocol("unexpected response type polling " + command)
		}
		// Commands like recompile_status return a double-encoded JSON string.
		body := protocol.UnwrapJSONString(er.Result)
		return protocol.PollStep{
			Status: protocol.ExtractStatus(body),
			Body:   body,
		}, nil
	}, cfg, nil, nil)
}
