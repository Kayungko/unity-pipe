package assetversion

import (
	"context"
	"strings"
	"testing"
)

func TestSummaryIncludesReferenceChangesWithoutConsumerEdits(t *testing.T) {
	const source = "Assets/Config.asset"
	const oldPath = "Assets/Old.asset"
	const movedPath = "Assets/Moved.asset"
	const deletedPath = "Assets/Deleted.asset"
	const asset = `--- !u!114 &11400000
MonoBehaviour:
  m_Name: Config
  linked: {fileID: 11400000, guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, type: 2}
  gone: {fileID: 11400000, guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb, type: 2}
`
	before := fixtureVersion("before", map[string]string{
		source: asset, oldPath: asset, oldPath + ".meta": "guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		deletedPath: asset, deletedPath + ".meta": "guid: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	after := fixtureVersion("after", map[string]string{
		source: asset, movedPath: asset, movedPath + ".meta": "guid: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	ctx := context.Background()
	summary, err := Summarize(ctx, before, after, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Changes) != 0 || len(summary.ReferenceChanges) != 1 || summary.ReferenceChanges[0].Path != source {
		t.Fatalf("expected reference-only changes: %+v", summary)
	}
	fields := summary.ReferenceChanges[0].Nodes[0].Components[0].Fields
	if len(fields) != 2 || fields[0].Path != "/gone" || fields[1].Path != "/linked" {
		t.Fatalf("unexpected fields: %+v", fields)
	}
	deleted := fields[0].After.Value.(Reference).Target
	if deleted.Status != "deleted" || deleted.Path != deletedPath {
		t.Fatalf("incorrect deleted target: %+v", deleted)
	}
	moved := fields[1].After.Value.(Reference).Target
	if moved.Status != "moved" || moved.Path != movedPath || moved.PreviousPath != oldPath {
		t.Fatalf("incorrect move: %+v", moved)
	}
}

func TestSerializedAssetDiffExplainsLocalObjectsAndBinaryCoverage(t *testing.T) {
	const source = "Assets/Config.asset"
	const opaque = "Assets/Opaque.asset"
	const asset = `--- !u!114 &11400000
MonoBehaviour:
  m_Name: Config
  child: {fileID: 23}
  rows:
  - score: 1
--- !u!114 &23
MonoBehaviour:
  m_Name: Child
`
	ctx := context.Background()
	before := fixtureVersion("before", map[string]string{source: asset, opaque: "\x00\x01"})
	edited := strings.ReplaceAll(strings.ReplaceAll(asset, "m_Name: Child", "m_Name: Renamed"), "score: 1", "score: 2")
	after := fixtureVersion("after", map[string]string{source: edited, opaque: "\x00\x02"})
	after.files[source] = file{name: source, object: "edited-text"}
	after.files[opaque] = file{name: opaque, object: "edited-binary"}
	diff, err := Diff(ctx, before, after, source, source)
	if err != nil {
		t.Fatal(err)
	}
	fields := diff.Nodes[0].Components[0].Fields
	if len(diff.Nodes) != 2 || len(fields) != 2 || fields[0].Path != "/child" || fields[1].Path != "/rows/0/score" {
		t.Fatalf("missing object/array differences: %+v", diff.Nodes)
	}
	if before, after := fields[0].Before.Value.(Reference).Target, fields[0].After.Value.(Reference).Target; before.ObjectName != "Child" || after.ObjectName != "Renamed" || after.Kind != "local" {
		t.Fatalf("unexplained local references: %+v, %+v", before, after)
	}
	if _, err := Diff(ctx, before, after, opaque, opaque); err == nil || !strings.Contains(err.Error(), "binary Unity serialization") {
		t.Fatalf("expected a binary diff error, got %v", err)
	}
	summary, err := Summarize(ctx, before, after, "")
	if err != nil || summary.Complete || len(summary.BeforeCoverage.SkippedFiles) != 1 ||
		summary.BeforeCoverage.SkippedFiles[0] != (SkippedReferenceAsset{opaque, "binary"}) ||
		len(summary.AfterCoverage.SkippedFiles) != 1 {
		t.Fatalf("missing per-side binary coverage: %+v, %v", summary, err)
	}
}
