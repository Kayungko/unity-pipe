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
			editor, err := rt.Client.EditorStatus()
			if err != nil {
				return nil, err
			}

			return map[string]any{
				"instance": PublicDescriptor(desc),
				"server":   server,
				"editor":   editor,
			}, nil
		},
	}
}
