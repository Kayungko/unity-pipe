package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// TestCommand runs Unity tests: run_tests in async mode, then polls
// test_status until a terminal state. A completed run with failing tests is
// still exit code 20 — the summary lives in error.data.
func TestCommand() SubCommand {
	return SubCommand{
		Name:     "test",
		Aliases:  []string{"tests"},
		Synopsis: "run tests and wait: upipe test [--mode editor|playmode|all] [--filter F] [--affected --from OLD --to NEW [--plan] | --rerun-failed] [--nunit FILE]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe test")
			mode := fs.String("mode", "all", "all | editor | playmode")
			filter := fs.String("filter", "", "test name filter (case-insensitive partial match)")
			filterType := fs.String("filter-type", "testName", "testName | assembly | category")
			detail := fs.String("detail", "full", "completed test results: full | failures | summary")
			noWait := fs.Bool("no-wait", false, "submit async and return immediately")
			requireMatch := fs.Bool("require-match", false, "fail if the completed run matched zero tests")
			testTimeout := fs.Int("test-timeout", 0, "server-side test budget in seconds for the whole run (0 uses server default)")
			affected := fs.Bool("affected", false, "select tests using Git or submitted P4 snapshots")
			from := fs.String("from", "", "baseline Git revision or P4 changelist for --affected")
			to := fs.String("to", "", "target Git revision or P4 changelist for --affected")
			planOnly := fs.Bool("plan", false, "show the affected assembly plan without running tests")
			rerun := fs.Bool("rerun-failed", false, "rerun the failed cases in the latest completed run")
			failedRunID := fs.String("failed-run-id", "", "require this retained run ID for --rerun-failed")
			nunit := fs.String("nunit", "", "write NUnit 3 XML for the completed run")
			source := attachAssetSource(fs)
			attachShared(fs, rt.Opts)
			rest, err := parseArgs(fs, args)
			if err != nil {
				return nil, usageErr("test: " + err.Error())
			}
			if len(rest) != 0 {
				return nil, usageErr("test: unexpected positional arguments")
			}
			if (*planOnly || *from != "" || *to != "" || source.p4.Root != "" ||
				source.p4.Port != "" || source.p4.User != "" || source.p4.Client != "") && !*affected {
				return nil, usageErr("test: revision and plan options require --affected")
			}
			if *affected && *rerun || (*affected || *rerun) && *filter != "" {
				return nil, usageErr("test: --affected, --rerun-failed and --filter are mutually exclusive")
			}
			if *failedRunID != "" && !*rerun {
				return nil, usageErr("test: --failed-run-id requires --rerun-failed")
			}
			if *nunit != "" && (*noWait || *planOnly) {
				return nil, usageErr("test: --nunit requires waiting for a test run")
			}
			if *noWait && *requireMatch {
				return nil, usageErr("test: --require-match requires waiting for the result")
			}
			switch *detail {
			case "full":
			case "failures", "summary":
				if *noWait {
					return nil, usageErr("test: --detail failures or summary requires waiting for the result")
				}
			default:
				return nil, usageErr("test: --detail must be full, failures, or summary")
			}
			if *testTimeout < 0 {
				return nil, usageErr("test: --test-timeout must be non-negative")
			}
			params := map[string]any{
				"mode":        *mode,
				"async_tests": true,
			}
			if *affected || *rerun {
				switch *mode = strings.ToLower(*mode); *mode {
				case "all", "editor", "editmode", "playmode", "play":
				default:
					return nil, usageErr("test: mode must be all, editor or playmode")
				}
			}
			if *affected {
				plan, err := affectedTestPlan(rt, source, *from, *to)
				if err != nil {
					return nil, err
				}
				if *planOnly {
					return plan, nil
				}
				reportAffectedTests(plan)
				if !plan.RunAll {
					if len(plan.Assemblies) == 0 {
						return noSelectedTests(*nunit, "no affected assemblies", *requireMatch)
					}
					params["exact_selection"] = map[string][]string{"assemblies": plan.Assemblies}
				}
			}
			if *rerun {
				selection, count, err := recentFailedTests(rt, *mode, *failedRunID)
				if err != nil {
					return nil, err
				}
				if count == 0 {
					return noSelectedTests(*nunit, "no failed tests in the selected modes", *requireMatch)
				}
				params["exact_selection"] = selection
			}
			if _, exact := params["exact_selection"]; exact {
				if err := requireExactTestSelection(rt); err != nil {
					return nil, err
				}
			}
			if err := rt.requireCleanTestScenes(); err != nil {
				return nil, err
			}
			if *filter != "" {
				params["filter"] = *filter
				params["filter_type"] = *filterType
			}
			if *testTimeout > 0 {
				params["timeout"] = *testTimeout
			}

			trigger, err := rt.execSimple("run_tests", params)
			if err != nil {
				return nil, err
			}
			if *noWait {
				return trigger, nil
			}

			var started struct {
				RunID string `json:"runId"`
			}
			_ = json.Unmarshal(protocol.UnwrapJSONString(trigger.Result), &started)
			statusParams := map[string]any{}
			if started.RunID != "" {
				statusParams["run_id"] = started.RunID
			}
			if *detail != "full" && *nunit == "" {
				statusParams["detail"] = *detail
			}
			result, err := rt.pollCommandWithParams("test_status", statusParams, protocol.PollConfig{
				TerminalOK:   []string{"completed", "no_tests", "timed_out"},
				TerminalFail: []string{"error", "interrupted"}, TerminalCancel: []string{"cancelled", "canceled"},
			})
			if err != nil {
				return nil, err
			}
			if result.Status == "timed_out" {
				failure := protocol.ErrTimeout("test run timed out")
				failure.Data = result.Body
				return nil, failure
			}

			if *nunit != "" {
				var report testRunReport
				if json.Unmarshal(result.Body, &report) != nil {
					return nil, protocol.ErrProtocol("invalid terminal result for NUnit XML")
				}
				if err := writeNUnit(*nunit, report); err != nil {
					return nil, err
				}
				if *detail != "full" {
					body, err := projectTestDetail(result.Body, *detail)
					if err != nil {
						return nil, err
					}
					result.Body = body
				}
			}
			// Completed async runs report outcomes in the summary counters.
			var final struct {
				Summary json.RawMessage `json:"summary"`
				Error   string          `json:"error"`
			}
			_ = json.Unmarshal(result.Body, &final)
			var summary struct {
				Total  *int `json:"total"`
				Failed int  `json:"failed"`
			}
			if result.Status == "completed" {
				if err := json.Unmarshal(final.Summary, &summary); err != nil || summary.Total == nil {
					return nil, protocol.ErrProtocol("completed test run did not return a test summary")
				}
			}
			if result.Status == "no_tests" || (*summary.Total == 0 && summary.Failed == 0) {
				if *requireMatch {
					return nil, protocol.ErrCommandFailed(&protocol.ExecResponse{
						Error: "filter matched no tests", Result: result.Body,
					})
				}
				if result.Status == "no_tests" {
					return map[string]any{"status": "no_tests"}, nil
				}
				return map[string]any{
					"status":  "completed",
					"message": "filter matched no tests",
					"summary": final.Summary,
				}, nil
			}
			if summary.Failed > 0 {
				return nil, &protocol.UpipeError{
					Code:    protocol.ExitCommandFailed,
					Kind:    "CommandFailed",
					Message: firstNonEmpty(final.Error, fmt.Sprintf("test run finished with %d failure(s)", summary.Failed)),
					Data:    json.RawMessage(result.Body),
				}
			}
			return json.RawMessage(result.Body), nil
		},
	}
}

// requireCleanTestScenes checks every open scene, including inactive additive
// scenes, before invoking the Test Runner, which can show a save dialog.
func (rt *Runtime) requireCleanTestScenes() error {
	response, err := rt.execSimple("list_open_scenes", nil)
	if err != nil {
		return err
	}
	var result struct {
		Scenes []struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			IsDirty bool   `json:"isDirty"`
		} `json:"scenes"`
	}
	snapshot := response.Result
	if err := json.Unmarshal(snapshot, &result); err != nil || result.Scenes == nil {
		return protocol.ErrProtocol("list_open_scenes did not return a scene list")
	}
	var dirty []string
	for _, scene := range result.Scenes {
		if scene.IsDirty {
			dirty = append(dirty, firstNonEmpty(scene.Path, scene.Name, "Untitled"))
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	return protocol.ErrCommandFailed(&protocol.ExecResponse{
		Error:  "save open scenes before running tests; unsaved changes in: " + strings.Join(dirty, ", "),
		Result: snapshot,
	})
}

// firstNonEmpty is re-declared locally to avoid importing protocol internals
// twice; protocol has its own copy.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
