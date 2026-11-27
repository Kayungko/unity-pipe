package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/assetversion"
	"github.com/Kayungko/unity-pipe/internal/protocol"
)

type testCaseResult struct {
	FullName   string
	Assembly   string
	Mode       string
	Status     string
	Duration   float64
	Message    string
	StackTrace string
}

type testCounts struct {
	Total        int `json:"total" xml:"total,attr"`
	Passed       int `json:"passed" xml:"passed,attr"`
	Failed       int `json:"failed" xml:"failed,attr"`
	Skipped      int `json:"skipped" xml:"skipped,attr"`
	Inconclusive int `json:"inconclusive" xml:"inconclusive,attr"`
}

type testRunReport struct {
	Status       string     `json:"status"`
	RunID        string     `json:"runId"`
	Mode         string     `json:"mode"`
	StartedAtUTC string     `json:"startedAtUtc"`
	Duration     float64    `json:"duration"`
	Summary      testCounts `json:"summary"`
	Phases       struct {
		EditMode *testCounts `json:"editMode"`
		PlayMode *testCounts `json:"playMode"`
	}
	Results []testCaseResult `json:"results"`
}

func (report testRunReport) phaseOf(i int) string {
	if mode := report.Results[i].Mode; mode != "" {
		return mode
	}
	switch strings.ToLower(report.Mode) {
	case "editmode":
		return "EditMode"
	case "playmode":
		return "PlayMode"
	case "all":
		// Older packages flattened the two phases in this order.
		if report.Summary.Total == len(report.Results) && report.Phases.EditMode != nil && report.Phases.PlayMode != nil &&
			report.Phases.EditMode.Total+report.Phases.PlayMode.Total == len(report.Results) {
			if i < report.Phases.EditMode.Total {
				return "EditMode"
			}
			return "PlayMode"
		}
	}
	return ""
}

func affectedTestPlan(rt *Runtime, source *assetSourceFlags, from, to string) (assetversion.TestImpact, error) {
	var empty assetversion.TestImpact
	if err := source.validate(from, to); err != nil {
		return empty, err
	}
	ctx, cancel := assetDeadline(rt)
	defer cancel()
	open := func(rev string) (*assetversion.Version, error) {
		if source.p4.Root != "" {
			return assetversion.OpenP4Project(ctx, source.p4, rev)
		}
		return assetversion.OpenGitProject(ctx, rt.ProjectPath, rev)
	}
	before, err := open(from)
	if err != nil {
		return empty, assetError(ctx, err)
	}
	after, err := open(to)
	if err != nil {
		return empty, assetError(ctx, err)
	}
	plan, err := assetversion.TestSelection(ctx, before, after)
	if err != nil {
		return empty, assetError(ctx, err)
	}
	return plan, nil
}

func recentFailedTests(rt *Runtime, mode, runID string) (map[string]any, int, error) {
	params := map[string]any{}
	if runID != "" {
		params["run_id"] = runID
	}
	reply, err := rt.execSimple("test_status", params)
	if err != nil {
		return nil, 0, err
	}
	var report testRunReport
	if err := json.Unmarshal(protocol.UnwrapJSONString(reply.Result), &report); err != nil || report.Status == "" {
		return nil, 0, protocol.ErrProtocol("test_status did not return a test run")
	}
	if report.Status != "completed" {
		return nil, 0, protocol.ErrCommandFailed(&protocol.ExecResponse{Error: "rerun requires a retained completed test run"})
	}
	return failedTestSelection(report, mode)
}

func failedTestSelection(report testRunReport, mode string) (map[string]any, int, error) {
	edit := map[string][]string{}
	play := map[string][]string{}
	found, selected := 0, 0
	for i, test := range report.Results {
		if !strings.EqualFold(test.Status, "Failed") {
			continue
		}
		found++
		var target map[string][]string
		switch report.phaseOf(i) {
		case "EditMode":
			if mode != "playmode" && mode != "play" {
				target = edit
			}
		case "PlayMode":
			if mode != "editor" && mode != "editmode" {
				target = play
			}
		default:
			return nil, 0, protocol.ErrProtocol("the previous report does not identify the failed test's mode")
		}
		if strings.TrimSpace(test.FullName) == "" {
			return nil, 0, protocol.ErrProtocol("the previous report contains an unnamed failed test")
		}
		if target != nil && !slices.Contains(target[test.Assembly], test.FullName) {
			target[test.Assembly] = append(target[test.Assembly], test.FullName)
			selected++
		}
	}
	if found != report.Summary.Failed {
		return nil, 0, protocol.ErrProtocol("the previous report does not contain every failed test")
	}
	return map[string]any{"edit": edit, "play": play}, selected, nil
}

func requireExactTestSelection(rt *Runtime) error {
	raw, err := rt.Client.CommandCatalog("flat", "", "run_tests", true)
	if err != nil {
		return err
	}
	var body struct {
		Commands []struct {
			Name       string
			Parameters []struct{ Name string }
		}
	}
	if json.Unmarshal(raw, &body) == nil {
		for _, command := range body.Commands {
			if command.Name != "run_tests" {
				continue
			}
			for _, param := range command.Parameters {
				if param.Name == "exact_selection" {
					return nil
				}
			}
		}
	}
	return protocol.ErrProtocol("the connected Pipeline package does not support exact test selection")
}

func reportAffectedTests(plan assetversion.TestImpact) {
	if plan.RunAll {
		fmt.Fprintf(os.Stderr, "upipe: affected test selection falls back to all tests (%s)\n", strings.Join(plan.Reasons, "; "))
	} else {
		fmt.Fprintf(os.Stderr, "upipe: %d affected assemblies\n", len(plan.Assemblies))
	}
}

func noSelectedTests(nunitPath, message string, requireMatch bool) (any, error) {
	if nunitPath != "" {
		if err := writeNUnit(nunitPath, testRunReport{Status: "no_tests"}); err != nil {
			return nil, err
		}
	}
	body := map[string]any{"status": "no_tests", "message": message, "summary": testCounts{}}
	if requireMatch {
		return nil, protocol.ErrCommandFailed(&protocol.ExecResponse{Error: message, Result: mustMarshal(body)})
	}
	return body, nil
}
