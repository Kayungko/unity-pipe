package assetversion

import (
	"context"
	"strings"
	"testing"
)

func TestIntegrityAndImpactReportNewInvalidReferences(t *testing.T) {
	const target = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const consumer = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const material = "Assets/Materials/Target.mat"
	const host = "Assets/Host.prefab"
	const scene = "Assets/Use.unity"
	const meta = "guid: "
	const hostYAML = `--- !u!1 &100
GameObject:
  m_Name: Root
--- !u!23 &101
MeshRenderer:
  m_GameObject: {fileID: 100}
  m_Materials:
  - {fileID: 2100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  - {fileID: 2100000, guid: cccccccccccccccccccccccccccccccc, type: 2}
`
	const sceneYAML = `--- !u!1001 &200
PrefabInstance:
  m_SourcePrefab: {fileID: 100100000, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 3}
`
	old := fixtureVersion("old", map[string]string{
		host: hostYAML, host + ".meta": meta + consumer, scene: sceneYAML,
		material: "mat", material + ".meta": meta + target,
		"Assets/Materials.meta": "folderAsset: yes\nguid: dddddddddddddddddddddddddddddddd",
	})
	next := fixtureVersion("new", map[string]string{
		host: hostYAML, host + ".meta": meta + consumer, scene: sceneYAML,
		material: "mat", material + ".meta": meta + target,
		"Assets/Other.mat": "mat", "Assets/Other.mat.meta": meta + target,
		"Assets/Materials.meta":    "folderAsset: yes\nguid: dddddddddddddddddddddddddddddddd",
		"Assets/Broken.asset.meta": "invalid metadata",
		"Assets/Pointer.prefab":    "version https://git-lfs.github.com/spec/v1\n",
	})
	ctx := context.Background()
	check, err := next.CheckIntegrity(ctx, "Assets")
	if err != nil {
		t.Fatal(err)
	}
	var duplicate, missing, invalid bool
	for _, issue := range check.Issues {
		if issue.Kind == "duplicate_guid" && issue.GUID == target && len(issue.Candidates) == 2 {
			duplicate = true
		}
		if issue.Kind == "missing_meta" && issue.Path == scene {
			missing = true
		}
		if issue.Kind == "invalid_meta" && strings.HasSuffix(issue.Path, "Broken.asset.meta") {
			invalid = true
		}
	}
	if check.Complete || !duplicate || !missing || !invalid {
		t.Fatalf("unexpected integrity report: %+v", check)
	}
	diff, err := Impact(ctx, old, next, "")
	if err != nil {
		t.Fatal(err)
	}
	if diff.Complete || len(diff.NewlyInvalid) != 1 || diff.NewlyInvalid[0].Field != "/m_Materials/0" ||
		diff.NewlyInvalid[0].Reference.Asset.Status != "ambiguous" {
		t.Fatalf("unexpected invalid references: %+v", diff)
	}
	for _, asset := range diff.Affected {
		if asset.Path == scene && len(asset.Via) == 1 && asset.Via[0].Field == "/m_SourcePrefab" {
			return
		}
	}
	t.Fatalf("scene was not reached from the changed asset via the Prefab: %+v", diff.Affected)
}
