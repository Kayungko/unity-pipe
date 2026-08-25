package commands

import (
	"encoding/json"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// AuditCommand runs a Project Auditor scan and polls audit_status until
// terminal (completed | failed | interrupted | unavailable).
func AuditCommand() SubCommand {
	return SubCommand{
		Name:     "audit",
		Synopsis: "run a Project Auditor scan and wait: upipe audit [--no-wait] (requires Project Auditor)",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe audit")
			noWait := fs.Bool("no-wait", false, "submit and return immediately")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("audit: " + err.Error())
			}

			trigger, err := rt.execSimple("audit", map[string]any{})
			if err != nil {
				return nil, err
			}
			if *noWait {
				return trigger, nil
			}

			result, err := rt.pollCommand("audit_status", protocol.PollConfig{
				TerminalOK:   []string{"completed"},
				TerminalFail: []string{"failed", "interrupted", "unavailable", "error"},
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
