// Command upipe is a thin, self-maintained CLI client for the Unity Pipeline
// HTTP API served by the com.unity.pipeline package inside a running Unity
// Editor or development Player.
//
// It deliberately has zero third-party dependencies and never goes through
// Unity's closed-source unity CLI binary: every capability is a direct HTTP
// call against the editor-local pipeline server, whose command surface is
// discovered at runtime via GET /api/commands.
package main

import (
	"os"

	"github.com/Kayungko/unity-pipe/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
