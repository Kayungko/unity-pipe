// Package commands implements the upipe subcommands. Each command is a thin
// waiting-policy wrapper over protocol.PipelineClient — the command surface
// intentionally stays small; anything the server exposes beyond it is reached
// through `upipe exec <name>` with zero client changes.
package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// Runtime carries the wired-up client stack into a command. It is constructed
// by the cli layer, which is the only place allowed to instantiate a
// transport — commands stay transport-agnostic.
type Runtime struct {
	Opts        *options.Options
	Transport   protocol.Transport
	Client      *protocol.PipelineClient
	ProjectPath string // resolved Unity project root
}

// Close releases the underlying transport.
func (rt *Runtime) Close() {
	if rt.Transport != nil {
		_ = rt.Transport.Close()
	}
}

// PortFileSource returns a DescriptorSource that re-reads the project's port
// file on every call — mandatory for surviving domain reloads, which rewrite
// both the port and the eval token.
func PortFileSource(projectPath string) protocol.DescriptorSource {
	return func() (*protocol.InstanceDescriptor, error) {
		data, err := os.ReadFile(protocol.ProjectPortFile(projectPath))
		if err != nil {
			return nil, fmt.Errorf("no running pipeline instance for %s (port file unreadable)", projectPath)
		}
		return protocol.ParseDescriptor(data)
	}
}

// ResolveProjectPath picks the project root: an explicit --project-path wins;
// otherwise walk up from the working directory looking for a project with a
// pipeline port file.
func ResolveProjectPath(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", protocol.ErrNoInstance(fmt.Sprintf("invalid --project-path: %v", err))
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", protocol.ErrNoInstance(fmt.Sprintf("cannot determine working directory: %v", err))
	}
	root, ok := protocol.FindProjectRoot(cwd, func(dir string) bool {
		st, err := os.Stat(protocol.ProjectPortFile(dir))
		return err == nil && !st.IsDir()
	})
	if !ok {
		return "", protocol.ErrNoInstance(
			"no Unity project with a running pipeline instance found from the current directory; " +
				"pass --project-path <project root> or open the editor")
	}
	return root, nil
}

// PublicDescriptor strips secrets (eval token) from a descriptor before it is
// rendered to output.
func PublicDescriptor(d *protocol.InstanceDescriptor) map[string]any {
	return map[string]any{
		"pid":           d.Pid,
		"port":          d.Port,
		"projectPath":   d.ProjectPath,
		"projectName":   d.ProjectName,
		"unityVersion":  d.UnityVersion,
		"mode":          d.Mode,
		"startedAt":     d.StartedAt,
		"lastHeartbeat": d.LastHeartbeat,
	}
}
