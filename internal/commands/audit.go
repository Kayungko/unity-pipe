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
		Synopsis: "run a Project Auditor scan and wait: upipe audit [--categories …] [--output …] [--no-wait] (requires Project Auditor)",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe audit")
			noWait := fs.Bool("no-wait", false, "submit and return immediately")
			categories := fs.String("categories", "", "comma-separated issue categories (default: all)")
			output := fs.String("output", "", "CSV path (default: Temp/pipeline-audit/<scanId>.csv)")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("audit: " + err.Error())
			}

			params := map[string]any{}
			if *categories != "" {
				params["categories"] = *categories
			}
			if *output != "" {
				params["output"] = *output
			}
			trigger, err := rt.execSimple("audit", params)
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
