package assetversion

import (
	"context"
	"maps"
	"slices"
	"testing"
)

func TestSelectionTransitiveDependenciesIncludeOldOwners(t *testing.T) {
	oldFiles := map[string]string{
		"Assets/Core/Core.asmdef":       "{\"name\":\"Core\"}",
		"Assets/Core/Removed.cs":        "old",
		"Assets/Sharing/Sharing.asmdef": "{\"name\":\"Sharing\",\"references\":[\"Core\"]}",
		"Assets/Tests/Selected.asmdef":  "{\"name\":\"Selected\",\"references\":[\"Sharing\"]}",
		"Assets/Other/Unrelated.asmdef": "{\"name\":\"Unrelated\"}",
	}
	snapshot := func(revision string, files map[string]string) *Version {
		version := fixtureVersion(revision, files)
		for name, ref := range version.files {
			ref.object = files[name]
			version.files[name] = ref
		}
		return version
	}
	nextFiles := maps.Clone(oldFiles)
	delete(nextFiles, "Assets/Core/Removed.cs")
	old := snapshot("old", oldFiles)
	next := snapshot("new", nextFiles)
	got, err := TestSelection(context.Background(), old, next)
	if err != nil || got.RunAll || !slices.Equal(got.Assemblies, []string{"Core", "Selected", "Sharing"}) {
		t.Fatalf("removed script impact: %+v %v", got, err)
	}
	nextFiles["Assets/Loose.cs"] = "new"
	got, err = TestSelection(context.Background(), old, snapshot("loose", nextFiles))
	if err != nil || !got.RunAll || len(got.Reasons) == 0 {
		t.Fatalf("fallback for predefined assembly: %+v %v", got, err)
	}
}
