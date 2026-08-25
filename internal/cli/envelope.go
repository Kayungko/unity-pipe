package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// envelope is the single stdout JSON shape in --json mode. stdout is ALWAYS
// exactly one parseable JSON document; human-facing notes go to stderr.
type envelope struct {
	OK    bool                 `json:"ok"`
	Error *protocol.UpipeError `json:"error,omitempty"`
	Data  any                  `json:"data,omitempty"`
}

// render formats a command outcome and returns the process exit code.
func render(err error, opts *options.Options, data ...any) int {
	if err != nil {
		ue := protocol.AsUpipeError(err)
		if opts.JSONOut {
			writeJSON(envelope{OK: false, Error: ue})
		} else {
			fmt.Fprintf(os.Stderr, "upipe: %s\n", ue.Error())
		}
		return ue.Code
	}
	var payload any
	if len(data) > 0 {
		payload = data[0]
	}
	if opts.JSONOut {
		writeJSON(envelope{OK: true, Data: payload})
		return protocol.ExitOK
	}
	if payload != nil {
		pretty, merr := json.MarshalIndent(payload, "", "  ")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "upipe: render: %v\n", merr)
			return protocol.ExitInternal
		}
		fmt.Println(string(pretty))
	}
	return protocol.ExitOK
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "upipe: stdout encode failed: %v\n", err)
		os.Exit(protocol.ExitInternal)
	}
}
