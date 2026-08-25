package commands

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
			_ = query // filtering is done client-side post-fetch (M2 refinement)

			if *full {
				raw, err := rt.Client.ListCommandsFull()
				if err != nil {
					return nil, err
				}
				return raw, nil
			}
			list, err := rt.Client.ListCommands()
			if err != nil {
				return nil, err
			}
			return list, nil
		},
	}
}
