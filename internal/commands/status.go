package commands

import (
	"fmt"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// StatusCommand merges `unity pipeline list` + `unity command editor_status`
// into one probe: the discovered instance, the server health, and the editor
// state.
func StatusCommand() SubCommand {
	return SubCommand{
		Name:     "status",
		Synopsis: "probe the pipeline instance: port file, server health, editor state",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe status")
			attachShared(fs, rt.Opts)
			if err := fs.Parse(args); err != nil {
				return nil, usageErr(fmt.Sprintf("status: %v", err))
			}

			// Fresh descriptor (also proves the port file is live).
			desc, err := rt.Client.Source()
			if err != nil {
				return nil, protocol.ErrNoInstance(err.Error())
			}

			server, err := rt.Client.Status()
			if err != nil {
				return nil, err
			}
			var editor *protocol.ExecResponse
			if desc.Mode != "player" && protocol.ExtractStatus(server) != "blocked_by_dialog" {
				editor, err = rt.Client.EditorStatus()
				if err != nil {
					return nil, err
				}
			}

			return map[string]any{
				"instance": PublicDescriptor(desc),
				"server":   server,
				"editor":   editor,
			}, nil
		},
	}
}

// DialogCommand uses the off-main-thread endpoint so it works while a modal blocks the Editor.
func DialogCommand() SubCommand {
	return SubCommand{
		Name:     "dialog",
		Synopsis: "read the currently detected modal dialogs in the Editor",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe dialog")
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("dialog: " + err.Error())
			}
			if len(rest) != 0 {
				return nil, usageErr("dialog: unexpected positional arguments")
			}
			return rt.Client.Dialog(rt.Opts.Timeout)
		},
	}
}
