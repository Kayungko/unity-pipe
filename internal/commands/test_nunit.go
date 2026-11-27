package commands

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type nunitResult struct {
	testCounts
	Result string `xml:"result,attr"`
	Cases  int    `xml:"testcasecount,attr"`
}

func nunitOutcome(counts testCounts) string {
	switch {
	case counts.Failed > 0:
		return "Failed"
	case counts.Passed > 0 || counts.Total == 0:
		return "Passed"
	case counts.Inconclusive > 0:
		return "Inconclusive"
	default:
		return "Skipped"
	}
}

type nunitRun struct {
	XMLName  xml.Name `xml:"test-run"`
	ID       string   `xml:"id,attr,omitempty"`
	Start    string   `xml:"start-time,attr,omitempty"`
	Duration float64  `xml:"duration,attr"`
	nunitResult
	Suites []*nunitSuite `xml:"test-suite"`
}

type nunitSuite struct {
	Type     string  `xml:"type,attr"`
	Name     string  `xml:"name,attr"`
	FullName string  `xml:"fullname,attr"`
	Duration float64 `xml:"duration,attr"`
	nunitResult
	Tests []nunitCase `xml:"test-case"`
}

type nunitCase struct {
	Name     string           `xml:"name,attr"`
	FullName string           `xml:"fullname,attr"`
	Result   string           `xml:"result,attr"`
	Duration float64          `xml:"duration,attr"`
	Failure  *nunitDiagnostic `xml:"failure,omitempty"`
	Reason   *nunitDiagnostic `xml:"reason,omitempty"`
}

type nunitDiagnostic struct {
	Message string `xml:"message"`
	Stack   string `xml:"stack-trace,omitempty"`
}

func makeNUnitReport(report testRunReport) (nunitRun, error) {
	doc := nunitRun{ID: report.RunID, Start: report.StartedAtUTC, Duration: report.Duration}
	var actual testCounts
	groups := map[[2]string]*nunitSuite{}
	for i, test := range report.Results {
		mode := report.phaseOf(i)
		key := [2]string{mode, test.Assembly}
		suite := groups[key]
		if suite == nil {
			name := firstNonEmpty(test.Assembly, mode, "Unity tests")
			if mode != "" && test.Assembly != "" {
				name += " (" + mode + ")"
			}
			suite = &nunitSuite{Type: "Assembly", Name: name, FullName: name}
			groups[key] = suite
			doc.Suites = append(doc.Suites, suite)
		}
		item := nunitCase{Name: test.FullName, FullName: test.FullName, Duration: test.Duration}
		switch strings.ToLower(test.Status) {
		case "passed":
			item.Result = "Passed"
			actual.Passed++
			suite.Passed++
		case "failed":
			item.Result = "Failed"
			item.Failure = &nunitDiagnostic{Message: test.Message, Stack: test.StackTrace}
			actual.Failed++
			suite.Failed++
		case "skipped":
			item.Result = "Skipped"
			item.Reason = &nunitDiagnostic{Message: test.Message}
			actual.Skipped++
			suite.Skipped++
		case "inconclusive":
			item.Result = "Inconclusive"
			item.Reason = &nunitDiagnostic{Message: test.Message}
			actual.Inconclusive++
			suite.Inconclusive++
		default:
			return doc, fmt.Errorf("unexpected test status %q in NUnit report", test.Status)
		}
		if strings.TrimSpace(test.FullName) == "" {
			return doc, fmt.Errorf("unnamed test case in NUnit report")
		}
		actual.Total++
		suite.Total++
		suite.Duration += test.Duration
		suite.Tests = append(suite.Tests, item)
	}
	if actual != report.Summary {
		return doc, fmt.Errorf("test summary does not match the available test cases")
	}
	for _, suite := range doc.Suites {
		suite.Cases = suite.Total
		suite.Result = nunitOutcome(suite.testCounts)
	}
	doc.nunitResult = nunitResult{testCounts: actual, Cases: actual.Total, Result: nunitOutcome(actual)}
	return doc, nil
}

func writeNUnit(output string, report testRunReport) error {
	doc, err := makeNUnitReport(report)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".upipe-nunit-*.xml")
	if err != nil {
		return fmt.Errorf("create NUnit XML: %w", err)
	}
	defer os.Remove(file.Name())
	_, err = file.WriteString(xml.Header)
	if err == nil {
		encoder := xml.NewEncoder(file)
		encoder.Indent("", "  ")
		err = encoder.Encode(doc)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write NUnit XML: %w", err)
	}
	if err := os.Rename(file.Name(), output); err != nil {
		return fmt.Errorf("publish NUnit XML: %w", err)
	}
	return nil
}

// projectTestDetail applies the existing result projections after writing the
// full XML from the same terminal snapshot.
func projectTestDetail(body json.RawMessage, detail string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if _, ok := fields["results"]; !ok {
		return body, nil
	}
	kept := []json.RawMessage{}
	if detail == "failures" {
		var records []json.RawMessage
		if err := json.Unmarshal(fields["results"], &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			var test testCaseResult
			if err := json.Unmarshal(record, &test); err != nil {
				return nil, err
			}
			if strings.EqualFold(test.Status, "Failed") {
				kept = append(kept, record)
			}
		}
	}
	fields["results"] = mustMarshal(kept)
	return json.Marshal(fields)
}
