package commands

import (
	"encoding/json"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// CommandsCommand lists the command surface exposed by the connected editor
// instance (GET /api/commands). This is the dynamic discovery mechanism: new
// server commands appear here without any client change.
func CommandsCommand() SubCommand {
	return SubCommand{
		Name:     "commands",
		Aliases:  []string{"command-list"},
		Synopsis: "list the commands exposed by the connected editor instance",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe commands")
			full := fs.Bool("full", false, "include parameter JSON schemas (default: compact index)")
			query := fs.String("query", "", "case-insensitive substring filter on name/description/tags")
			attachShared(fs, rt.Opts)
			if err := fs.Parse(args); err != nil {
				return nil, usageErr("commands: " + err.Error())
			}

			if *full {
				raw, err := rt.Client.ListCommandsFull()
				if err != nil {
					return nil, err
				}
				if *query != "" {
					raw, err = filterFullCommands(raw, *query)
					if err != nil {
						return nil, err
					}
				}
				return raw, nil
			}
			list, err := rt.Client.ListCommands()
			if err != nil {
				return nil, err
			}
			if *query != "" {
				filterCommandSpecs(list, *query)
			}
			return list, nil
		},
	}
}

// filterCommandSpecs drops entries not matching q (case-insensitive substring
// on name, description or tags) in place and rewrites Count to match; Total
// keeps the server-reported total.
func filterCommandSpecs(list *protocol.CommandsResponse, q string) {
	q = strings.ToLower(q)
	kept := make([]protocol.CommandSpec, 0, len(list.Commands))
	for _, c := range list.Commands {
		if specMatchesQuery(c, q) {
			kept = append(kept, c)
		}
	}
	list.Commands = kept
	list.Count = len(kept)
}

// specMatchesQuery reports whether a compact command entry matches the already
// lower-cased query.
func specMatchesQuery(c protocol.CommandSpec, q string) bool {
	if strings.Contains(strings.ToLower(c.Name), q) ||
		strings.Contains(strings.ToLower(c.Description), q) {
		return true
	}
	for _, t := range c.Tags {
		if strings.Contains(strings.ToLower(t), q) {
			return true
		}
	}
	return false
}

// filterFullCommands applies the same query to a full (--full) raw response.
// Entries are handled as raw JSON so unknown schema fields survive untouched.
func filterFullCommands(raw json.RawMessage, q string) (json.RawMessage, error) {
	var body struct {
		Commands []json.RawMessage `json:"commands"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, protocol.ErrProtocol("commands: malformed full index: " + err.Error())
	}
	lq := strings.ToLower(q)
	kept := make([]json.RawMessage, 0, len(body.Commands))
	for _, entry := range body.Commands {
		var c protocol.CommandSpec
		if err := json.Unmarshal(entry, &c); err != nil {
			continue // tolerate malformed entries: keep them out, don't fail
		}
		if specMatchesQuery(c, lq) {
			kept = append(kept, entry)
		}
	}
	out, err := json.Marshal(map[string]any{
		"commands": kept,
		"total":    body.Total,
		"count":    len(kept),
	})
	if err != nil {
		return nil, protocol.ErrProtocol("commands: " + err.Error())
	}
	return out, nil
}
