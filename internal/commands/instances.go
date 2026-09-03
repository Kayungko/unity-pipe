package commands

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// knownProjectsFile is the machine-local registry of Unity projects that have
// interacted with upipe (registered after any successful command — see the cli
// layer — and by pkg-install; readable by instances). It lives under the
// user's home (NOT inside any project) per the "no project-local harness
// config" rule. Variable (not const func) so tests can redirect it.
var knownProjectsFile = func() string {
	return userHomeDir(".upipe", "projects.txt")
}

// RegisterProject appends a project root to the registry (deduplicated).
// Best-effort: a failure to persist never fails the calling command.
func RegisterProject(projectPath string) {
	if projectPath == "" {
		return
	}
	path := knownProjectsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	known := readKnownProjects()
	for _, p := range known {
		if strings.EqualFold(p, projectPath) {
			return // already registered
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, projectPath)
}

// readKnownProjects loads the registry; malformed lines are skipped.
func readKnownProjects() []string {
	data, err := os.ReadFile(knownProjectsFile())
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[strings.ToLower(line)] {
			continue
		}
		seen[strings.ToLower(line)] = true
		out = append(out, line)
	}
	return out
}

// writeKnownProjects overwrites the registry with exactly the given project
// list (one per line). Used by pruneKnownProjects; RegisterProject keeps
// using its append-only path for the common case.
func writeKnownProjects(projects []string) error {
	path := knownProjectsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, p := range projects {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// deadProjectReason classifies a registry entry as dead ("" if still live):
// its port file is missing/corrupt, or present but unreachable AND stale
// (the same "definitely dead" bar the default `instances` listing uses to
// hide leftovers). A reachable port (even with a stale heartbeat — an idle
// editor stops ticking the heartbeat but keeps serving) counts as live.
func deadProjectReason(root string, now time.Time) string {
	data, err := os.ReadFile(protocol.ProjectPortFile(root))
	if err != nil {
		return "port file unreadable (project moved, deleted, or never installed)"
	}
	d, err := protocol.ParseDescriptor(data)
	if err != nil {
		return "port file corrupt"
	}
	if !portReachable(d.Port) && protocol.IsStale(d, now) {
		return "port closed and heartbeat stale"
	}
	return ""
}

// pruneKnownProjects rewrites the registry keeping only projects that are
// still live per deadProjectReason. Returns the removed entries (with their
// reason) — best-effort, mirroring RegisterProject's philosophy: a failure to
// persist is reported as an error to the caller (unlike registration, prune
// is an explicit user action and should not fail silently).
func pruneKnownProjects(now time.Time) (removed []map[string]string, err error) {
	known := readKnownProjects()
	var kept []string
	for _, root := range known {
		if reason := deadProjectReason(root, now); reason != "" {
			removed = append(removed, map[string]string{"project": root, "reason": reason})
			continue
		}
		kept = append(kept, root)
	}
	if len(removed) == 0 {
		return removed, nil
	}
	if err := writeKnownProjects(kept); err != nil {
		return nil, err
	}
	return removed, nil
}

// InstancesCommand lists every running pipeline instance known to this
// machine: it reads the project registry, plus the project resolved from the
// current directory, and probes each candidate's port file for a live
// heartbeat. Read-only; no state is written.
func InstancesCommand() SubCommand {
	return SubCommand{
		Name:      "instances",
		Synopsis:  "list all running pipeline instances (known projects + cwd project) [--all] [--prune]",
		NoRuntime: true,
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe instances")
			all := fs.Bool("all", false, "also show stale instances (heartbeat older than 30s)")
			prune := fs.Bool("prune", false, "remove dead entries from the registry (port unreadable/corrupt, or unreachable+stale) before listing")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("instances: " + err.Error())
			}

			now := time.Now()
			var pruned []map[string]string
			if *prune {
				removed, err := pruneKnownProjects(now)
				if err != nil {
					return nil, protocol.AsUpipeError(err)
				}
				pruned = removed
			}

			// Candidate roots: registry entries + cwd-resolved project (a
			// project that was never pkg-installed from this machine still
			// shows up when the command runs inside it).
			candidates := readKnownProjects()
			if cwd, err := os.Getwd(); err == nil {
				if root, ok := protocol.FindProjectRoot(cwd, func(dir string) bool {
					st, err := os.Stat(protocol.ProjectPortFile(dir))
					return err == nil && !st.IsDir()
				}); ok {
					candidates = append(candidates, root)
				}
			}

			type instance struct {
				Project     string `json:"project"`
				ProjectName string `json:"projectName"`
				UnityVer    string `json:"unityVersion"`
				Port        int    `json:"port"`
				Pid         int    `json:"pid"`
				Mode        string `json:"mode"`
				Heartbeat   string `json:"lastHeartbeat"`
				Stale       bool   `json:"stale"`
				Reachable   bool   `json:"reachable"`
			}
			var found []instance
			seen := map[string]bool{}
			for _, root := range candidates {
				if seen[strings.ToLower(root)] {
					continue
				}
				seen[strings.ToLower(root)] = true
				data, err := os.ReadFile(protocol.ProjectPortFile(root))
				if err != nil {
					continue // not running (or never installed)
				}
				d, err := protocol.ParseDescriptor(data)
				if err != nil {
					continue // corrupt port file — not an instance to report
				}
				// Liveness: a TCP dial to the port is authoritative (an idle
				// editor may stop rewriting the heartbeat but keeps serving);
				// the heartbeat age is informational. Default listing shows
				// reachable instances plus fresh-heartbeat ones; --all adds
				// unreachable leftovers (likely crashed editors).
				reachable := portReachable(d.Port)
				stale := protocol.IsStale(d, now)
				if !*all && !reachable && stale {
					continue // dead: port closed AND heartbeat expired
				}
				found = append(found, instance{
					Project:     root,
					ProjectName: d.ProjectName,
					UnityVer:    d.UnityVersion,
					Port:        d.Port,
					Pid:         d.Pid,
					Mode:        d.Mode,
					Heartbeat:   d.LastHeartbeat.Format(time.RFC3339),
					Stale:       stale,
					Reachable:   reachable,
				})
			}
			sort.Slice(found, func(i, j int) bool { return found[i].ProjectName < found[j].ProjectName })

			result := map[string]any{
				"instances": found,
				"count":     len(found),
				"registry":  knownProjectsFile(),
			}
			if *prune {
				result["pruned"] = pruned
				result["prunedCount"] = len(pruned)
			}
			if len(found) == 0 {
				result["hint"] = "no live instances. If a project is missing from the list, run any upipe command inside it once (or pkg-install it) so it gets registered."
			}
			return result, nil
		},
	}
}

// portReachable does a quick TCP dial to distinguish "editor up" from
// "port file left behind by a crash".
func portReachable(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
