package commands

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestFailedSelectionFromLegacyCombinedReport(t *testing.T) {
	report := testRunReport{
		Mode: "All", Summary: testCounts{Total: 3, Failed: 2, Passed: 1},
		Results: []testCaseResult{
			{FullName: "QA.Editor.Failed(2)", Status: "Failed"},
			{FullName: "QA.Editor.Passed", Status: "Passed"},
			{FullName: "QA.Player.Failed", Status: "Failed"},
		},
	}
	report.Phases.EditMode = &testCounts{Total: 2, Failed: 1, Passed: 1}
	report.Phases.PlayMode = &testCounts{Total: 1, Failed: 1}
	selection, count, err := failedTestSelection(report, "all")
	if err != nil || count != 2 {
		t.Fatalf("legacy failed cases: %v %v", selection, err)
	}
	if edit := selection["edit"].(map[string][]string)[""]; len(edit) != 1 || edit[0] != "QA.Editor.Failed(2)" {
		t.Fatalf("edit selection: %v", edit)
	}
	if play := selection["play"].(map[string][]string)[""]; len(play) != 1 || play[0] != "QA.Player.Failed" {
		t.Fatalf("play selection: %v", play)
	}
	report.Results = report.Results[:1]
	if _, _, err := failedTestSelection(report, "all"); err == nil {
		t.Fatal("ambiguous incomplete historical results cannot drive a rerun")
	}
}

func TestNUnitUsesFullFailureCaseAndVerifiesCounts(t *testing.T) {
	report := testRunReport{
		Summary: testCounts{Total: 1, Failed: 1},
		Results: []testCaseResult{
			{FullName: "QA.Case(\"A<B\")", Assembly: "QA.Tests", Mode: "EditMode",
				Status: "Failed", Message: "failed <a>&", StackTrace: "at QA.Case"},
		},
	}
	nunit, err := makeNUnitReport(report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(nunit)
	if err != nil || !strings.Contains(string(data), "<test-run") ||
		!strings.Contains(string(data), "failed &lt;a&gt;&amp;") ||
		!strings.Contains(string(data), "<stack-trace>at QA.Case</stack-trace>") {
		t.Fatalf("NUnit XML mismatch: %s %v", data, err)
	}
	report.Summary.Passed++
	if _, err := makeNUnitReport(report); err == nil {
		t.Fatal("exported a summary inconsistent with test cases")
	}
}
