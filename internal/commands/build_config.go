package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type namedBuild struct {
	Target     string   `json:"target"`
	OutputPath string   `json:"outputPath"`
	Scenes     []string `json:"scenes"`
	Options    []string `json:"options"`
}

func loadNamedBuild(project, name string) (namedBuild, error) {
	var build namedBuild
	if strings.TrimSpace(name) == "" {
		return build, fmt.Errorf("--config requires a configuration name")
	}

	path := filepath.Join(project, ".upipe", "builds.json")
	file, err := os.Open(path)
	if err != nil {
		return build, fmt.Errorf("cannot open build configurations: %w", err)
	}
	defer file.Close()

	var document struct {
		Version int                   `json:"version"`
		Builds  map[string]namedBuild `json:"builds"`
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return build, fmt.Errorf("invalid build configuration file %q: %w", path, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return build, fmt.Errorf("build configuration file %q must contain exactly one JSON object", path)
	}
	if document.Version != 1 {
		return build, fmt.Errorf("unsupported build configuration version %d in %q; expected 1", document.Version, path)
	}

	build, exists := document.Builds[name]
	if !exists {
		return build, fmt.Errorf("configuration %q not found in %q", name, path)
	}
	if strings.TrimSpace(build.Target) == "" || strings.TrimSpace(build.OutputPath) == "" || len(build.Scenes) == 0 {
		return build, fmt.Errorf("configuration %q requires target, outputPath and at least one scene", name)
	}
	for _, scene := range build.Scenes {
		if strings.TrimSpace(scene) == "" {
			return build, fmt.Errorf("configuration %q contains a blank scene", name)
		}
	}
	for _, option := range build.Options {
		if strings.TrimSpace(option) == "" {
			return build, fmt.Errorf("configuration %q contains a blank option", name)
		}
	}
	return build, nil
}
