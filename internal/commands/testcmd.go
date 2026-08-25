package commands

import (
	"encoding/json"
	"fmt"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// TestCommand runs Unity tests: run_tests in async mode, then polls
// test_status until a terminal state. A completed run with failing tests is
// still exit code 20 — the summary lives in error.data.
func TestCommand() SubCommand {
	return SubCommand{
		Name:     "test",
		Aliases:  []string{"tests"},
		Synopsis: "run tests and wait: upipe test [--mode editor|playmode|all] [--filter F] [--no-wait]",
		Run: func(rt *Runtime, args []string) (any, error) {
			fs := newFlagSet("upipe test")
			mode := fs.String("mode", "all", "all | editor | playmode")
			filter := fs.String("filter", "", "test name filter (case-insensitive partial match)")
			filterType := fs.String("filter-type", "testName", "testName | assembly | category")
			noWait := fs.Bool("no-wait", false, "submit async and return immediately")
			attachShared(fs, rt.Opts)
			if _, err := parseArgs(fs, args); err != nil {
				return nil, usageErr("test: " + err.Error())
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

			// The terminal body of a completed async run is the raw test
			// status file JSON — it carries NO top-level "success" field
			// (only status/duration/summary/results), so the outcome MUST be
			// judged by the summary counters, never by a parsed success flag
			// (which would always read as false).
			var final struct {
				Summary json.RawMessage `json:"summary"`
				Results json.RawMessage `json:"results"`
				Error   string          `json:"error"`
			}
			_ = json.Unmarshal(result.Body, &final)
			var summary struct {
				Total  int `json:"total"`
				Passed int `json:"passed"`
				Failed int `json:"failed"`
			}
			_ = json.Unmarshal(final.Summary, &summary)
			if result.Status == "no_tests" {
				return map[string]any{"status": "no_tests"}, nil
			}
			// A completed run whose filter matched nothing is not a failure
			// (known server behavior).
			if summary.Total == 0 && summary.Failed == 0 {
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
