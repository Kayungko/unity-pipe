package protocol

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Port file location, relative to a Unity project root.
const (
	PortFileName    = ".unity-pipeline-port"
	PortFileRelPath = "Library" + string(filepath.Separator) + "Pipeline" + string(filepath.Separator) + PortFileName
)

// StaleThreshold: a descriptor whose last heartbeat is older than this is
// considered stale (the editor rewrites the file on every heartbeat tick).
const StaleThreshold = 30 * time.Second

// ParseDescriptor decodes the port file body. It rejects descriptors with no
// port (corrupt file) but tolerates unknown fields.
func ParseDescriptor(data []byte) (*InstanceDescriptor, error) {
	var d InstanceDescriptor
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("corrupt port file: %w", err)
	}
	if d.Port <= 0 {
		return nil, fmt.Errorf("corrupt port file: missing port")
	}
	return &d, nil
}

// IsStale reports whether the descriptor's heartbeat is too old to trust.
func IsStale(d *InstanceDescriptor, now time.Time) bool {
	return now.Sub(d.LastHeartbeat) > StaleThreshold
}

// HostPort returns the dial address for the descriptor.
func (d *InstanceDescriptor) HostPort() string {
	return fmt.Sprintf("127.0.0.1:%d", d.Port)
}

// FindProjectRoot walks up from start looking for a directory containing the
// Library/Pipeline port file. `exists` is injected so the function stays pure.
// Returns the project root and whether it was found.
func FindProjectRoot(start string, exists func(dir string) bool) (string, bool) {
	dir := strings.TrimRight(start, string(filepath.Separator))
	if dir == "" {
		return "", false
	}
	for {
		if exists(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// ProjectPortFile returns the absolute port file path for a project root.
func ProjectPortFile(projectPath string) string {
	return filepath.Join(projectPath, "Library", "Pipeline", PortFileName)
}
