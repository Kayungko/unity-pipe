package assetversion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNestedPrefabOverrideUsesProjectedObjectID(t *testing.T) {
	const leafGUID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const middleGUID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const leaf = `--- !u!1 &3
GameObject:
  m_Name: Leaf
--- !u!4 &4
Transform:
  m_GameObject: {fileID: 3}
  m_Father: {fileID: 0}
--- !u!135 &5
SphereCollider:
  m_GameObject: {fileID: 3}
  m_Radius: 1
`
	const middle = `--- !u!1001 &8
PrefabInstance:
  m_Modification:
    m_Modifications: []
  m_SourcePrefab: {fileID: 100100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
--- !u!1001 &16
PrefabInstance:
  m_Modification:
    m_Modifications: []
  m_SourcePrefab: {fileID: 100100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 3}
`
	const host = `--- !u!1001 &32
PrefabInstance:
  m_Modification:
    m_Modifications:
    - target: {fileID: 13, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 3}
      propertyPath: m_Radius
      value: 9
      objectReference: {fileID: 0}
  m_SourcePrefab: {fileID: 100100000, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 3}
`
	before := fixtureVersion("old", map[string]string{
		"Assets/Leaf.prefab": leaf, "Assets/Leaf.prefab.meta": "guid: " + leafGUID,
		"Assets/Middle.prefab": middle, "Assets/Middle.prefab.meta": "guid: " + middleGUID,
		"Assets/Host.prefab": host,
	})
	after := fixtureVersion("new", map[string]string{
		"Assets/Moved/Leaf.prefab":      strings.Replace(leaf, "m_Radius: 1", "m_Radius: 4", 1),
		"Assets/Moved/Leaf.prefab.meta": "guid: " + leafGUID,
		"Assets/Middle.prefab":          middle, "Assets/Middle.prefab.meta": "guid: " + middleGUID,
		"Assets/Host.prefab": host,
	})
	diff, err := DiffWithInheritance(context.Background(), before, after, "Assets/Host.prefab", "Assets/Host.prefab")
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Nodes) != 0 || diff.Inherited == nil || len(diff.Inherited.Sources) != 2 {
		t.Fatalf("unexpected diff: %+v", diff)
	}
	protected, inherited := diff.Inherited.Sources[0], diff.Inherited.Sources[1]
	if strings.Join(protected.InstanceIDs, "/") != "32/8" || len(protected.Nodes) != 0 ||
		protected.After.Asset.Path != "Assets/Moved/Leaf.prefab" {
		t.Fatalf("overridden sibling changed: %+v", protected)
	}
	if strings.Join(inherited.InstanceIDs, "/") != "32/16" || len(inherited.Nodes) != 1 ||
		len(inherited.Nodes[0].Components) != 1 || len(inherited.Nodes[0].Components[0].Fields) != 1 {
		t.Fatalf("unexpected unoverridden sibling: %+v", inherited)
	}
	change := inherited.Nodes[0].Components[0].Fields[0]
	if change.Path != "/m_Radius" || change.Before.Value != json.Number("1") || change.After.Value != json.Number("4") {
		t.Fatalf("unexpected effective radius: %+v", change)
	}
}
