// Package assetversion reads Unity assets and their metadata from fixed source
// control snapshots. It never checks out a revision or writes asset files.
package assetversion

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
)

type Identity struct {
	Backend  string `json:"backend"`
	Revision string `json:"revision"`
}

type file struct {
	name   string
	object string
}

type Version struct {
	Identity
	files map[string]file
	read  func(context.Context, []file, func(file, []byte) error) error
}

func emptyVersion(backend string, read func(context.Context, []file, func(file, []byte) error) error) *Version {
	return &Version{Identity: Identity{Backend: backend, Revision: "empty"}, read: read}
}

func (v *Version) readFile(ctx context.Context, name string) ([]byte, bool, error) {
	ref, exists := v.files[name]
	if !exists {
		return nil, false, nil
	}
	var data []byte
	err := v.read(ctx, []file{ref}, func(_ file, bytes []byte) error {
		data = bytes
		return nil
	})
	return data, true, err
}

var (
	guidPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	metaGUID    = regexp.MustCompile(`(?m)^guid:[ \t]*([0-9a-fA-F]{32})[ \t]*\r?$`)
	folderMeta  = regexp.MustCompile(`(?m)^folderAsset:[ \t]*yes[ \t]*\r?$`)
)

func ValidGUID(value string) bool { return guidPattern.MatchString(value) }

type Resolution struct {
	GUID       string   `json:"guid"`
	Status     string   `json:"status"`
	Path       string   `json:"path,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
}

type metadata struct {
	name   string
	exists bool
}

type GuidIndex map[string][]metadata

func (v *Version) GUIDs(ctx context.Context) (GuidIndex, error) {
	return v.readGUIDs(ctx, func(name string) error {
		return fmt.Errorf("%s does not contain a Unity GUID", name)
	})
}

func (v *Version) readGUIDs(ctx context.Context, invalidMeta func(string) error) (GuidIndex, error) {
	index := make(GuidIndex)
	metas := make([]file, 0)
	for name, ref := range v.files {
		if strings.HasSuffix(name, ".meta") && (strings.HasPrefix(name, "Assets/") || strings.HasPrefix(name, "Packages/")) {
			metas = append(metas, ref)
		}
	}
	slices.SortFunc(metas, func(a, b file) int { return strings.Compare(a.name, b.name) })
	if err := v.read(ctx, metas, func(ref file, data []byte) error {
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		match := metaGUID.FindSubmatch(data)
		if match == nil {
			return invalidMeta(ref.name)
		}
		name := strings.TrimSuffix(ref.name, ".meta")
		_, exists := v.files[name]
		guid := strings.ToLower(string(match[1]))
		index[guid] = append(index[guid], metadata{name, exists || folderMeta.Match(data)})
		return nil
	}); err != nil {
		return nil, err
	}
	return index, nil
}

func (index GuidIndex) Resolve(guid string) Resolution {
	guid = strings.ToLower(guid)
	result := Resolution{GUID: guid, Status: "unresolved"}
	switch guid {
	case "00000000000000000000000000000000":
		result.Status = "null"
		return result
	case "0000000000000000d000000000000000", "0000000000000000e000000000000000", "0000000000000000f000000000000000":
		result.Status = "builtin"
		return result
	}
	matches := index[guid]
	switch len(matches) {
	case 0:
	case 1:
		result.Path = matches[0].name
		result.Status = "resolved"
		if !matches[0].exists {
			result.Status = "missing_asset"
		}
	default:
		result.Status = "ambiguous"
		for _, match := range matches {
			result.Candidates = append(result.Candidates, match.name)
		}
	}
	return result
}

func safeProjectPath(name string) bool {
	return name != "" && path.Clean(name) == name && !strings.HasPrefix(name, "/") &&
		name != ".." && !strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\r\n")
}

func run(ctx context.Context, dir, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = dir
	body, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return body, fmt.Errorf("%s: %w: %s", executable, err, strings.TrimSpace(string(exit.Stderr)))
	}
	return body, err
}
