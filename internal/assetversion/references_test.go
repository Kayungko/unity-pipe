package assetversion

import (
	"context"
	"strings"
	"testing"
)

func TestFindReferencesSkipsBookkeepingAndReportsUnscannedAssets(t *testing.T) {
	const guid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const prefab = "Assets/Sub/Host.prefab"
	const body = `%YAML 1.1
--- !u!1 &100
GameObject:
  m_Name: Root
--- !u!4 &101
Transform:
  m_GameObject: {fileID: 100}
  m_Father: {fileID: 0}
--- !u!23 &9012345678901234567
MeshRenderer:
  m_GameObject: {fileID: 100}
  m_CorrespondingSourceObject: {fileID: 41, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  m_Materials:
  - {fileID: 2100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  - {fileID: 0, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  text: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
--- !u!1001 &200
PrefabInstance:
  m_SourcePrefab: {fileID: 100100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  m_Modification:
    m_TransformParent: {fileID: 101}
    m_Modifications:
    - target: {fileID: 50, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
      propertyPath: m_Materials.Array.data[0]
      value:
      objectReference: {fileID: 2100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
    m_RemovedComponents:
    - {fileID: 51, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
`
	v := fixtureVersion("test", map[string]string{
		prefab:                         body,
		"Assets/Sub/Opaque.prefab":     "\x00\x00\x01",
		"Assets/Sub/Large.unity":       "version https://git-lfs.github.com/spec/v1\noid sha256:a\nsize 1\n",
		"Assets/Submarine/Other.unity": body,
	})
	result, err := v.FindReferences(context.Background(), strings.ToUpper(guid), "Assets/Sub")
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Status != "unresolved" || result.Complete || result.ScannedFiles != 1 || len(result.SkippedFiles) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.SkippedFiles[0].Reason != "git_lfs_pointer" || result.SkippedFiles[1].Reason != "not_unity_text" {
		t.Fatalf("wrong skipped entries: %+v", result.SkippedFiles)
	}
	if len(result.Matches) != 3 {
		t.Fatalf("unexpected matches: %+v", result.Matches)
	}
	first, override, nested := result.Matches[0], result.Matches[1], result.Matches[2]
	if first.AssetPath != prefab || first.Node.Path != "/Root" || first.Component.FileID != "9012345678901234567" ||
		first.Field != "/m_Materials/0" || first.Reference.FileID != "2100000" {
		t.Fatalf("bad component use: %+v", first)
	}
	if override.Override == nil || override.Override.TargetFileID != "50" ||
		override.Override.PropertyPath != "m_Materials.Array.data[0]" || nested.Field != "/m_SourcePrefab" {
		t.Fatalf("lost the nested use or its override target: %+v, %+v", override, nested)
	}
	empty, err := v.FindReferences(context.Background(), "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", prefab)
	if err != nil || !empty.Complete || empty.ScannedFiles != 1 || len(empty.Matches) != 0 {
		t.Fatalf("zero matches should be a complete search: %+v, %v", empty, err)
	}
}
