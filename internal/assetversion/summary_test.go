package assetversion

import (
	"context"
	"strings"
	"testing"
)

func TestSummaryReportsMovedAssetsAndCoverage(t *testing.T) {
	const guid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const prefab = "Assets/Host.prefab"
	const material = "Assets/Material.mat"
	const moved = "Assets/New/Material.mat"
	const opaque = "Assets/Large.prefab"
	const host = `--- !u!1 &10
GameObject:
  m_Name: <Root>
--- !u!23 &11
MeshRenderer:
  m_GameObject: {fileID: 10}
  value: 3
  m_Materials:
  - {fileID: 2100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  - {fileID: 2100000, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
`
	start := fixtureVersion("before", map[string]string{
		prefab: host, material: "material", material + ".meta": "guid: " + guid,
		opaque: "version https://git-lfs.github.com/spec/v1\n",
	})
	end := fixtureVersion("after", map[string]string{
		prefab: strings.Replace(host, "value: 3", "value: 8", 1),
		moved:  "material", moved + ".meta": "guid: " + guid, opaque: "\x00\x00\x01",
	})
	end.files[prefab] = file{name: prefab, object: "new-blob"}
	end.files[opaque] = file{name: opaque, object: "other-blob"}
	summary, err := Summarize(context.Background(), start, end, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Complete || len(summary.NewlyInvalid) != 0 || len(summary.Changes) != 3 {
		t.Fatalf("unexpected overview: %+v", summary)
	}
	node := summary.Changes[0]
	if node.Path != prefab || len(node.Nodes) != 1 || node.Nodes[0].Components[0].Fields[1].Path != "/value" {
		t.Fatalf("missing direct field changes: %+v", node)
	}
	rename := summary.Changes[2]
	if rename.Change != "moved" || rename.Path != moved || rename.FromPath != material ||
		rename.BeforeGUID != guid || rename.AfterGUID != guid {
		t.Fatalf("unexpected move: %+v", rename)
	}
	markdown := summary.Markdown()
	for _, phrase := range []string{
		"## Newly invalid references (0)", "None found in the scanned files.", "git_lfs_pointer", "not_unity_text",
		"**moved** `Assets/Material.mat` → `Assets/New/Material.mat`", "`/value` (`3` → `8`)", "`/<Root>`",
	} {
		if !strings.Contains(markdown, phrase) {
			t.Fatalf("missing %q in Markdown:\n%s", phrase, markdown)
		}
	}
	empty, err := Summarize(context.Background(), start, start, prefab)
	if err != nil || !empty.Complete || len(empty.Changes)+len(empty.Affected)+len(empty.NewlyInvalid) != 0 {
		t.Fatalf("same scoped revision should yield a complete empty report: %+v, %v", empty, err)
	}
}
