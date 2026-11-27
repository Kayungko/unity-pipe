package assetversion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConflictSeparatesIndependentEditsFromDeletion(t *testing.T) {
	const name = "Assets/Test.prefab"
	const base = `--- !u!1 &1
GameObject:
  m_Name: Root
--- !u!65 &2
BoxCollider:
  m_GameObject: {fileID: 1}
  size: {x: 1, y: 2}
`
	ours := strings.Replace(base, "x: 1", "x: 4", 1)
	theirs := strings.Replace(base, "y: 2", "y: 5", 1)
	version := func(rev, yaml string) *Version {
		return fixtureVersion(rev, map[string]string{name: yaml})
	}
	report, err := ExplainConflict(context.Background(), version("base", base), version("ours", ours), version("theirs", theirs), name)
	if err != nil || report.HasConflicts || report.Classification != conflictIndependent || len(report.Nodes) != 1 {
		t.Fatalf("unexpected independent edits: %+v, %v", report, err)
	}
	fields := report.Nodes[0].Components[0].Fields
	if len(fields) != 2 || fields[0].Path != "/size/x" || fields[0].Theirs.Value != json.Number("1") ||
		fields[1].Path != "/size/y" || fields[1].Ours.Value != json.Number("2") {
		t.Fatalf("three-sided values missing: %+v", fields)
	}

	removed := strings.Split(base, "--- !u!65")[0]
	report, err = ExplainConflict(context.Background(), version("base", base), version("ours", removed), version("theirs", theirs), name)
	if err != nil || !report.HasConflicts || len(report.Nodes) != 1 {
		t.Fatalf("expected delete/modify conflict: %+v, %v", report, err)
	}
	component := report.Nodes[0].Components[0]
	if component.Reason != "delete_modify" || component.Ours != nil || component.Base.FileID != "2" ||
		component.Fields[2].Reason != "delete_modify" || component.Fields[2].Ours != nil {
		t.Fatalf("missing delete/modify detail: %+v", component)
	}
}

func TestConflictValueComparesNumbersExactlyAndPreservesNull(t *testing.T) {
	value := func(n string) *FieldValue { return &FieldValue{Value: json.Number(n)} }
	finding := classifyField(ConflictValues[FieldValue]{Base: value("1"), Ours: value("2.0"), Theirs: value("2")})
	if finding.Classification != conflictSame {
		t.Fatalf("same numeric value reported as %s", finding.Classification)
	}
	if equalConflictValue(value("9007199254740992"), value("9007199254740993")) {
		t.Fatal("different large integers collapsed")
	}
	empty := &FieldValue{Value: nil}
	if equalConflictValue(empty, nil) {
		t.Fatal("present null and a missing field are different")
	}
	for _, path := range []string{"Assets/New.mat", ""} {
		original := &FieldValue{Value: Reference{FileID: "5", Asset: &Resolution{GUID: "guid", Path: "Assets/Old.mat"}}}
		moved := &FieldValue{Value: Reference{FileID: "5", Asset: &Resolution{GUID: "guid", Path: path}}}
		if !equalConflictValue(original, moved) {
			t.Fatal("reference identity changed with its resolved path")
		}
	}
}
