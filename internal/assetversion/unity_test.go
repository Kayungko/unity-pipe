package assetversion

import (
	"context"
	"testing"
)

func TestDiffFindsVersionedReferenceChangesInUnchangedPrefab(t *testing.T) {
	const keep = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const lost = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const prefab = "Assets/Host.prefab"
	const yamlText = `%YAML 1.1
%TAG !u! tag:unity3d.com,2011:
--- !u!1 &101
GameObject:
  m_Name: Root
--- !u!4 &102
Transform:
  m_GameObject: {fileID: 101}
  m_Father: {fileID: 0}
--- !u!23 &103
MeshRenderer:
  m_GameObject: {fileID: 101}
  m_Materials:
  - {fileID: 2100000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  - {fileID: 2100000, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
`
	before := fixtureVersion("old", map[string]string{
		prefab: yamlText, "Assets/Keep.mat": "", "Assets/Keep.mat.meta": "guid: " + keep,
		"Assets/Lost.mat": "", "Assets/Lost.mat.meta": "guid: " + lost,
	})
	after := fixtureVersion("new", map[string]string{
		prefab: yamlText, "Assets/Moved/Keep.mat": "", "Assets/Moved/Keep.mat.meta": "guid: " + keep,
	})
	diff, err := Diff(context.Background(), before, after, prefab, prefab)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Nodes) != 1 || diff.Nodes[0].After.Path != "/Root" || len(diff.Nodes[0].Components) != 1 {
		t.Fatalf("unexpected node diff: %+v", diff.Nodes)
	}
	fields := diff.Nodes[0].Components[0].Fields
	if len(fields) != 2 || fields[0].Path != "/m_Materials/0" || fields[1].Path != "/m_Materials/1" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
	moved := fields[0].After.Value.(Reference)
	dropped := fields[1].After.Value.(Reference)
	if moved.FileID != "2100000" || moved.Asset.GUID != keep || moved.Asset.Path != "Assets/Moved/Keep.mat" ||
		dropped.Asset.GUID != lost || dropped.Asset.Status != "unresolved" {
		t.Fatalf("moved = %+v; dropped = %+v", moved.Asset, dropped.Asset)
	}
}

func fixtureVersion(revision string, contents map[string]string) *Version {
	paths := make(map[string]file)
	for name := range contents {
		paths[name] = file{name: name}
	}
	return &Version{Identity: Identity{"git", revision}, files: paths,
		read: func(_ context.Context, files []file, consume func(file, []byte) error) error {
			for _, file := range files {
				if err := consume(file, []byte(contents[file.name])); err != nil {
					return err
				}
			}
			return nil
		}}
}
