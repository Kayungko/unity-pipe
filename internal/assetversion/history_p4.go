package assetversion

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func TraceP4Field(ctx context.Context, options P4Options, at *Version, name string, selector FieldSelector, limit int) (FieldHistoryResult, error) {
	return traceField(ctx, at, name, selector, limit,
		func(ctx context.Context, current *Version, name string) (historyStep, error) {
			return options.previousFileRevision(ctx, current, name)
		}, func(context.Context, *FieldHistoryChange) error { return nil })
}

func (p P4Options) previousFileRevision(ctx context.Context, current *Version, name string) (historyStep, error) {
	object := current.files[name].object
	depot, revision, ok := strings.Cut(object, "#")
	if !ok || !ValidChangelist(revision) {
		return historyStep{}, fmt.Errorf("missing P4 revision for %s", name)
	}
	records, err := p.records(ctx, "filelog", "-m", "1", object)
	if err != nil {
		return historyStep{}, err
	}
	for _, record := range records {
		if record.DepotFile != depot || record.FileRev != revision {
			continue
		}
		timestamp, err := strconv.ParseInt(record.FileTime, 10, 64)
		change, _ := strconv.ParseUint(record.FileChange, 10, 64)
		anchor, _ := strconv.ParseUint(current.Revision, 10, 64)
		if err != nil || timestamp <= 0 || change == 0 || change > anchor || record.FileUser == "" {
			return historyStep{}, fmt.Errorf("P4 returned incomplete history metadata for %s", name)
		}
		point := *current
		point.Revision = record.FileChange
		previous, err := PreviousP4(ctx, p, &point)
		if err != nil {
			return historyStep{}, err
		}
		return historyStep{
			before: previous,
			mark: FieldHistoryChange{
				Revision: record.FileChange, Author: record.FileUser, Time: time.Unix(timestamp, 0).UTC(),
			},
			mayIntroduce: record.FileAction == "add" || record.FileAction == "branch",
		}, nil
	}
	return historyStep{}, fmt.Errorf("P4 did not return the pinned file revision for %s", name)
}
