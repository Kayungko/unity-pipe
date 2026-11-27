package assetversion

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

type SummaryChange struct {
	AssetChange
	FromPath string       `json:"fromPath,omitempty"`
	Nodes    []NodeChange `json:"nodes,omitempty"`
}

type AssetReferenceChanges struct {
	Path  string       `json:"path"`
	Nodes []NodeChange `json:"nodes"`
}

type AssetSummaryResult struct {
	From             Identity                `json:"from"`
	To               Identity                `json:"to"`
	Under            string                  `json:"under,omitempty"`
	Complete         bool                    `json:"complete"`
	BeforeCoverage   SnapshotCoverage        `json:"beforeCoverage"`
	AfterCoverage    SnapshotCoverage        `json:"afterCoverage"`
	Changes          []SummaryChange         `json:"changes"`
	Affected         []AffectedAsset         `json:"affected"`
	NewlyInvalid     []ReferenceUse          `json:"newlyInvalidReferences"`
	ReferenceChanges []AssetReferenceChanges `json:"referenceChanges,omitempty"`
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
		if change.AssetChanged && ValidDiffPath(change.Path) && path.Ext(oldPath) == path.Ext(change.Path) &&
			!skippedBefore[oldPath] && !skippedAfter[change.Path] &&
			withinScope(oldPath, under) && withinScope(change.Path, under) {
			oldDocs, oldSkip, err := loadSummaryDocuments(ctx, before, oldPath)
			if err != nil {
				return result, err
			}
			newDocs, newSkip, err := loadSummaryDocuments(ctx, after, change.Path)
			if err != nil {
				return result, err
			}
			if oldSkip != "" || newSkip != "" {
				result.addSkipped(oldPath, oldSkip, change.Path, newSkip)
			} else {
				change.Nodes, err = compareDocuments(
					documentSide{version: before, index: old.index, path: oldPath, docs: oldDocs},
					documentSide{version: after, index: current.index, path: change.Path, docs: newDocs})
				if err != nil {
					return result, err
				}
			}
		}
		result.Changes = append(result.Changes, change)
	}
	result.ReferenceChanges, err = summaryReferenceChanges(ctx, before, after, old.index, current.index, under)
	return result, err
}

func (r *AssetSummaryResult) addSkipped(beforePath, beforeReason, afterPath, afterReason string) {
	r.Complete = false
	if beforeReason != "" {
		r.BeforeCoverage.SkippedFiles = append(r.BeforeCoverage.SkippedFiles, SkippedReferenceAsset{beforePath, beforeReason})
	}
	if afterReason != "" {
		r.AfterCoverage.SkippedFiles = append(r.AfterCoverage.SkippedFiles, SkippedReferenceAsset{afterPath, afterReason})
	}
}

var serializedGUID = regexp.MustCompile(`(?i)\bguid:[ \t]*([0-9a-f]{32})\b`)

// An asset's bytes may be unchanged when a referenced asset moves or is
// deleted. Look for those links separately from the list of edited files.
func summaryReferenceChanges(ctx context.Context, before, after *Version, old, current GuidIndex, under string) ([]AssetReferenceChanges, error) {
	changed := make(map[string]bool)
	for _, index := range []GuidIndex{old, current} {
		for guid := range index {
			if !reflect.DeepEqual(old.Resolve(guid), current.Resolve(guid)) {
				changed[guid] = true
			}
		}
	}
	if len(changed) == 0 {
		return nil, ctx.Err()
	}
	var files []file
	for name, ref := range after.files {
		if isSerializedAsset(name) && ValidDiffPath(name) && withinScope(name, under) &&
			!ignoredUnityPath(name) && ref == before.files[name] {
			files = append(files, ref)
		}
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.name, b.name) })
	var result []AssetReferenceChanges
	err := after.read(ctx, files, func(ref file, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !slices.ContainsFunc(serializedGUID.FindAllSubmatch(data, -1), func(match [][]byte) bool {
			return changed[strings.ToLower(string(match[1]))]
		}) {
			return nil
		}
		docs, skip, err := parseDiffDocuments(data, ref.name)
		if err != nil {
			return fmt.Errorf("%s: %w", ref.name, err)
		}
		if skip != "" {
			return nil
		}
		nodes, err := compareDocuments(
			documentSide{version: before, index: old, path: ref.name, docs: docs},
			documentSide{version: after, index: current, path: ref.name, docs: docs})
		if err != nil {
			return err
		}
		if len(nodes) > 0 {
			result = append(result, AssetReferenceChanges{Path: ref.name, Nodes: nodes})
		}
		return nil
	})
	if err != nil {
		return result, err
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

func loadSummaryDocuments(ctx context.Context, version *Version, file string) ([]document, string, error) {
	data, exists, err := version.readFile(ctx, file)
	if err != nil || !exists {
		return nil, "", err
	}
	docs, skip, err := parseDiffDocuments(data, file)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", file, err)
	}
	return docs, skip, nil
}
