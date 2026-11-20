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
		Synopsis: "run tests and wait: upipe test [--mode editor|playmode|all] [--filter F] [--require-match] [--no-wait]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe test")
			mode := fs.String("mode", "all", "all | editor | playmode")
			filter := fs.String("filter", "", "test name filter (case-insensitive partial match)")
			filterType := fs.String("filter-type", "testName", "testName | assembly | category")
			noWait := fs.Bool("no-wait", false, "submit async and return immediately")
			requireMatch := fs.Bool("require-match", false, "fail if the completed run matched zero tests")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("test: " + err.Error())
			}
			if *noWait && *requireMatch {
				return nil, usageErr("test: --require-match requires waiting for the result")
			}
			if err := rt.requireCleanTestScenes(); err != nil {
				return nil, err
			}

			params := map[string]any{
				"mode":        *mode,
				"async_tests": true,
			}
			if *filter != "" {
				params["filter"] = *filter
				params["filter_type"] = *filterType
			}

			trigger, err := rt.execSimple("run_tests", params)
			if err != nil {
				return nil, err
			}
			if *noWait {
				return trigger, nil
			}

			result, err := rt.pollCommand("test_status", protocol.PollConfig{
				TerminalOK:   []string{"completed", "no_tests"},
				TerminalFail: []string{"error", "cancelled"},
			})
			if err != nil {
				return nil, err
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
