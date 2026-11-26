package assetversion

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestVariantReferenceArrayUsesSizeAndCanonicalLocalIDs(t *testing.T) {
	const baseGUID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const base = `--- !u!1 &3
GameObject:
  m_Name: Root
--- !u!4 &4
Transform:
  m_GameObject: {fileID: 3}
  m_Father: {fileID: 0}
--- !u!114 &5
MonoBehaviour:
  m_GameObject: {fileID: 3}
  references:
  - {fileID: 3}
  packed: 0a00000014000000
`
	const header = `--- !u!1001 &16
PrefabInstance:
  m_SourcePrefab: {fileID: 100100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
  m_Modification:
    m_Modifications:
`
	const resize = `    - target: {fileID: 5, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
      propertyPath: references.Array.size
      value: 4
      objectReference: {fileID: 0}
`
	const elements = `    - target: {fileID: 5, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
      propertyPath: references.Array.data[0]
      value:
      objectReference: {fileID: 19}
    - target: {fileID: 5, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
      propertyPath: references.Array.data[2]
      value:
      objectReference: {fileID: 19}
    - target: {fileID: 5, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
      propertyPath: packed.Array.size
      value: 0
      objectReference: {fileID: 0}
`
	const name = "Assets/Variant.prefab"
	version := func(rev, content string) *Version {
		return fixtureVersion(rev, map[string]string{
			name: content, "Assets/Base.prefab": base, "Assets/Base.prefab.meta": "guid: " + baseGUID,
		})
	}
	before := version("before", strings.Replace(header, "m_Modifications:", "m_Modifications: []", 1))
	after := version("grown", header+elements+resize)
	diff, err := DiffWithInheritance(context.Background(), before, after, name, name)
	if err != nil {
		t.Fatal(err)
	}
	source := diff.Inherited.Sources[0]
	if len(source.After.Issues) != 1 || !strings.Contains(source.After.Issues[0], "packed.Array.size") {
		t.Fatalf("unexpected diagnostics: %v", source.After.Issues)
	}
	fields := source.Nodes[0].Components[0].Fields
	var paths, ids []string
	for _, field := range fields {
		paths = append(paths, field.Path)
		ref := field.After.Value.(Reference)
		ids = append(ids, ref.FileID)
		if ref.FileID != "0" && !slices.Equal(ref.InstanceIDs, []string{"16"}) {
			t.Fatalf("local reference lacks its instance chain: %+v", ref)
		}
	}
	if !slices.Equal(paths, []string{"/references/1", "/references/2", "/references/3"}) ||
		!slices.Equal(ids, []string{"0", "3", "0"}) {
		t.Fatalf("paths = %v, IDs = %v", paths, ids)
	}
	cleared := version("cleared", header+elements+strings.Replace(resize, "value: 4", "value: 0", 1))
	diff, err = DiffWithInheritance(context.Background(), after, cleared, name, name)
	if err != nil {
		t.Fatal(err)
	}
	fields = diff.Inherited.Sources[0].Nodes[0].Components[0].Fields
	if issues := diff.Inherited.Sources[0].After.Issues; len(issues) != 1 || !strings.Contains(issues[0], "packed.Array.size") {
		t.Fatalf("stale array entries should be ignored: %v", issues)
	}
	if len(fields) != 5 || fields[0].Path != "/references" || fields[0].Change != "added" ||
		fields[4].Path != "/references/3" || fields[4].Change != "removed" {
		t.Fatalf("unexpected clear delta: %+v", fields)
	}
}
