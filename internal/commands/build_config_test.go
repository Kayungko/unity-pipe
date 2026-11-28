package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/options"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func TestLoadNamedBuild(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".upipe")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const body = `"target":"StandaloneWindows64","outputPath":"Builds/Game.exe","scenes":["Assets/B.unity","Assets/A.unity"],"options":["Development"]`
	const doc = `{"version":1,"builds":{"qa":{` + body + `}}}`
	want := namedBuild{
		Target: "StandaloneWindows64", OutputPath: "Builds/Game.exe",
		Scenes: []string{"Assets/B.unity", "Assets/A.unity"}, Options: []string{"Development"},
	}
	for _, tc := range []struct {
		description string
		document    string
		name        string
		errContains string
	}{
		{description: "valid", document: doc, name: "qa"},
		{"unknown name", doc, "QA", "not found"},
		{"misnamed field", strings.Replace(doc, "outputPath", "output", 1), "qa", "unknown field"},
		{"empty scene list", strings.Replace(doc, `"Assets/B.unity","Assets/A.unity"`, "", 1), "qa", "at least one scene"},
		{"empty option", strings.Replace(doc, "Development", " ", 1), "qa", "blank option"},
		{"future version", strings.Replace(doc, `"version":1`, `"version":2`, 1), "qa", "expected 1"},
		{"extra document", doc + " {}", "qa", "exactly one JSON object"},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "builds.json"), []byte(tc.document), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := loadNamedBuild(project, tc.name)
			if tc.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error = %v, want %q", err, tc.errContains)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("configuration = %+v, error = %v", got, err)
			}
		})
	}
}

func TestNamedBuildRejectsExplicitOverrides(t *testing.T) {
	rt := &Runtime{Opts: &options.Options{}}
	for _, flag := range []string{"target", "output-path", "scene", "option", "profile", "scene-preview-bundle"} {
		t.Run(flag, func(t *testing.T) {
			_, err := BuildCommand().Run(rt, []string{"--config", "qa", "--" + flag + "="})
			if err == nil || protocol.AsUpipeError(err).Code != protocol.ExitUsage ||
				!strings.Contains(err.Error(), "--config cannot be combined with --"+flag) {
				t.Fatalf("error = %v, want a flag conflict", err)
			}
		})
	}
}
