package assetversion

import (
	"context"
	"fmt"
	"path"
)

type SummaryChange struct {
	AssetChange
	FromPath string       `json:"fromPath,omitempty"`
	Nodes    []NodeChange `json:"nodes,omitempty"`
}

type AssetSummaryResult struct {
	From           Identity         `json:"from"`
	To             Identity         `json:"to"`
	Under          string           `json:"under,omitempty"`
	Complete       bool             `json:"complete"`
	BeforeCoverage SnapshotCoverage `json:"beforeCoverage"`
	AfterCoverage  SnapshotCoverage `json:"afterCoverage"`
	Changes        []SummaryChange  `json:"changes"`
	Affected       []AffectedAsset  `json:"affected"`
	NewlyInvalid   []ReferenceUse   `json:"newlyInvalidReferences"`
}

func Summarize(ctx context.Context, before, after *Version, under string) (AssetSummaryResult, error) {
	old, err := before.inventory(ctx)
	if err != nil {
		return AssetSummaryResult{}, err
	}
	current, err := after.inventory(ctx)
	if err != nil {
		return AssetSummaryResult{}, err
	}
	impact, err := impactWithInventory(ctx, before, after, under, old, current)
	if err != nil {
		return AssetSummaryResult{}, err
	}
	result := AssetSummaryResult{
		From: impact.From, To: impact.To, Under: under, Complete: impact.Complete,
		BeforeCoverage: impact.BeforeCoverage, AfterCoverage: impact.AfterCoverage,
		Changes: []SummaryChange{}, Affected: impact.Affected, NewlyInvalid: impact.NewlyInvalid,
	}
	skippedBefore := skippedPaths(impact.BeforeCoverage)
	skippedAfter := skippedPaths(impact.AfterCoverage)
	for _, change := range summaryChanges(impact.Changes, old.index, current.index) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		oldPath := change.Path
		if change.FromPath != "" {
			oldPath = change.FromPath
		}
		if !withinScope(change.Path, under) && (change.FromPath == "" || !withinScope(change.FromPath, under)) {
			continue
		}
		if change.AssetChanged && ValidUnityPath(change.Path) && path.Ext(oldPath) == path.Ext(change.Path) &&
			!skippedBefore[oldPath] && !skippedAfter[change.Path] &&
			withinScope(oldPath, under) && withinScope(change.Path, under) {
			oldDocs, err := loadSummaryDocuments(ctx, before, oldPath)
			if err != nil {
				return result, err
			}
			newDocs, err := loadSummaryDocuments(ctx, after, change.Path)
			if err != nil {
				return result, err
			}
			change.Nodes, err = compareDocuments(oldDocs, newDocs, old.index, current.index, oldPath, change.Path)
			if err != nil {
				return result, fmt.Errorf("%s: %w", change.Path, err)
			}
		}
		result.Changes = append(result.Changes, change)
	}
	return result, ctx.Err()
}

func skippedPaths(coverage SnapshotCoverage) map[string]bool {
	paths := make(map[string]bool, len(coverage.SkippedFiles))
	for _, skip := range coverage.SkippedFiles {
		paths[skip.AssetPath] = true
	}
	return paths
}

func summaryChanges(changes []AssetChange, old, current GuidIndex) []SummaryChange {
	byPath := make(map[string]AssetChange, len(changes))
	for _, change := range changes {
		byPath[change.Path] = change
	}
	moves := make(map[string]SummaryChange)
	fromPaths := make(map[string]bool)
	for _, added := range changes {
		if added.Change != "added" || added.AfterGUID == "" {
			continue
		}
		from, to := old.Resolve(added.AfterGUID), current.Resolve(added.AfterGUID)
		removed := byPath[from.Path]
		if from.Status != "resolved" || to.Status != "resolved" || to.Path != added.Path || removed.Change != "removed" {
			continue
		}
		added.Change = "moved"
		added.BeforeGUID = removed.BeforeGUID
		added.AssetChanged = added.AssetChanged || removed.AssetChanged
		added.MetaChanged = added.MetaChanged || removed.MetaChanged
		moves[added.Path] = SummaryChange{AssetChange: added, FromPath: from.Path}
		fromPaths[from.Path] = true
	}
	result := make([]SummaryChange, 0, len(changes))
	for _, change := range changes {
		if fromPaths[change.Path] {
			continue
		}
		if moved, ok := moves[change.Path]; ok {
			result = append(result, moved)
		} else {
			result = append(result, SummaryChange{AssetChange: change})
		}
	}
	return result
}

func loadSummaryDocuments(ctx context.Context, version *Version, file string) ([]document, error) {
	data, exists, err := version.readFile(ctx, file)
	if err != nil || !exists {
		return nil, err
	}
	docs, err := parseUnity(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return docs, nil
}
