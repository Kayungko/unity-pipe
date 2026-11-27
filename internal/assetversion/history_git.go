package assetversion

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func TraceGitField(ctx context.Context, dir string, at *Version, name string, selector FieldSelector, limit int) (FieldHistoryResult, error) {
	return traceField(ctx, at, name, selector, limit,
		func(ctx context.Context, current *Version, _ string) (historyStep, error) {
			previous, err := PreviousGit(ctx, dir, current)
			return historyStep{before: previous, mark: FieldHistoryChange{Revision: current.Revision}, mayIntroduce: true}, err
		},
		func(ctx context.Context, mark *FieldHistoryChange) error {
			output, err := run(ctx, dir, "git", "show", "-s", "--encoding=UTF-8",
				"--format=%an%x00%aI", mark.Revision)
			if err != nil {
				return err
			}
			author, timestamp, ok := strings.Cut(strings.TrimSuffix(string(output), "\n"), "\x00")
			when, err := time.Parse(time.RFC3339, strings.TrimSpace(timestamp))
			if !ok || author == "" || err != nil {
				return fmt.Errorf("Git did not return valid author metadata for %s", mark.Revision)
			}
			mark.Author, mark.Time = author, when.UTC()
			return nil
		})
}
