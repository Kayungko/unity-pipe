package assetversion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestFieldHistoryFollowsGUIDAndComponentIdentity(t *testing.T) {
	const guid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const firstPath = "Assets/Old.prefab"
	const movedPath = "Assets/Moved.prefab"
	const yaml = `--- !u!1 &100
GameObject:
  m_Name: Root
--- !u!23 &101
MeshRenderer:
  m_GameObject: {fileID: 100}
  m_Value: `
	makeVersion := func(rev, name, value string) *Version {
		v := fixtureVersion(rev, map[string]string{
			name: yaml + value + "\n", name + ".meta": "guid: " + guid,
		})
		v.files[name] = file{name: name, object: value}
		v.files[name+".meta"] = file{name: name + ".meta", object: guid}
		return v
	}
	first := makeVersion("initial", firstPath, "1")
	modified := makeVersion("edit", firstPath, "7")
	head := makeVersion("move", movedPath, "7")
	origins := map[string]*Version{"edit": first, "move": modified}
	step := func(_ context.Context, current *Version, _ string) (historyStep, error) {
		return historyStep{before: origins[current.Revision], mark: FieldHistoryChange{Revision: current.Revision}, mayIntroduce: true}, nil
	}
	witness := func(_ context.Context, change *FieldHistoryChange) error {
		change.Author = "Original author"
		return nil
	}
	query := FieldSelector{Node: "/Root", Component: "MeshRenderer", Field: "/m_Value"}
	result, err := traceField(context.Background(), head, movedPath, query, 2, step, witness)
	if err != nil || result.Status != "found" || result.LastChange == nil {
		t.Fatalf("trace = %+v, %v", result, err)
	}
	last := result.LastChange
	if result.Examined != 2 || last.Path != firstPath || last.Revision != "edit" ||
		last.Author != "Original author" || last.Before.Value != json.Number("1") || last.After.Value != json.Number("7") {
		t.Fatalf("wrong origin: %+v", last)
	}
	result, err = traceField(context.Background(), head, movedPath, query, 1, step, witness)
	if err != nil || result.Status != "history_insufficient" || result.Reason != "revision_limit" || result.LastChange != nil {
		t.Fatalf("limit result = %+v, %v", result, err)
	}
	query.Field = "/missing"
	result, err = traceField(context.Background(), head, movedPath, query, 2,
		func(context.Context, *Version, string) (historyStep, error) {
			return historyStep{}, errors.New("walked history")
		}, witness)
	if err != nil || result.Status != "field_not_found" || result.Examined != 0 || result.LastChange != nil {
		t.Fatalf("missing field result = %+v, %v", result, err)
	}
}
