package assetversion

import (
	"context"
	"strings"
)

type AssetChange struct {
	Path         string `json:"path"`
	Change       string `json:"change"`
	BeforeGUID   string `json:"beforeGuid,omitempty"`
	AfterGUID    string `json:"afterGuid,omitempty"`
	AssetChanged bool   `json:"assetChanged"`
	MetaChanged  bool   `json:"metaChanged"`
}

type AffectedAsset struct {
	Path    string         `json:"path"`
	Changed bool           `json:"changed"`
	Via     []ReferenceUse `json:"via"`
}

type SnapshotCoverage struct {
	ScannedFiles int                     `json:"scannedFiles"`
	SkippedFiles []SkippedReferenceAsset `json:"skippedFiles"`
	InvalidMeta  []string                `json:"invalidMeta,omitempty"`
}

type AssetImpactResult struct {
	From           Identity         `json:"from"`
	To             Identity         `json:"to"`
	Under          string           `json:"under,omitempty"`
	Complete       bool             `json:"complete"`
	BeforeCoverage SnapshotCoverage `json:"beforeCoverage"`
	AfterCoverage  SnapshotCoverage `json:"afterCoverage"`
	Changes        []AssetChange    `json:"changes"`
	Affected       []AffectedAsset  `json:"affected"`
	NewlyInvalid   []ReferenceUse   `json:"newlyInvalidReferences"`
}

func Impact(ctx context.Context, before, after *Version, under string) (AssetImpactResult, error) {
	result := AssetImpactResult{
		From: before.Identity, To: after.Identity, Under: under,
		Changes: []AssetChange{}, Affected: []AffectedAsset{}, NewlyInvalid: []ReferenceUse{},
	}
	old, err := before.inventory(ctx)
	if err != nil {
		return result, err
	}
	current, err := after.inventory(ctx)
	if err != nil {
		return result, err
	}
	oldScan, err := before.scanAllReferences(ctx, old.index, under)
	if err != nil {
		return result, err
	}
	newScan, err := after.scanAllReferences(ctx, current.index, under)
	if err != nil {
		return result, err
	}
	result.BeforeCoverage = SnapshotCoverage{oldScan.scanned, oldScan.skipped, old.invalidMeta}
	result.AfterCoverage = SnapshotCoverage{newScan.scanned, newScan.skipped, current.invalidMeta}
	result.Complete = len(oldScan.skipped)+len(newScan.skipped)+len(old.invalidMeta)+len(current.invalidMeta) == 0

	changed := changedAssetPaths(before, after)
	affected := make(map[string]*AffectedAsset)
	seeds := make(map[string]bool)
	for _, name := range sortedKeys(changed) {
		change := changed[name]
		change.BeforeGUID, change.AfterGUID = old.guidByPath[name], current.guidByPath[name]
		result.Changes = append(result.Changes, change)
		for _, guid := range []string{change.BeforeGUID, change.AfterGUID} {
			if guid != "" {
				seeds[guid] = true
			}
		}
		if _, exists := after.files[name]; exists && ValidUnityPath(name) && withinScope(name, under) {
			affected[name] = &AffectedAsset{Path: name, Changed: true, Via: []ReferenceUse{}}
		}
	}

	reverse := make(map[string][]ReferenceUse)
	previousInvalid := make(map[referenceIdentity]bool)
	unreadableBefore := make(map[string]bool)
	for _, skip := range oldScan.skipped {
		unreadableBefore[skip.AssetPath] = true
	}
	for _, use := range oldScan.uses {
		if invalidReference(*use.Reference.Asset) {
			previousInvalid[referenceKey(use, use.AssetPath)] = true
		}
	}
	for _, use := range newScan.uses {
		guid := use.Reference.Asset.GUID
		reverse[guid] = append(reverse[guid], use)
		previousPath := use.AssetPath
		if guid := current.guidByPath[use.AssetPath]; len(current.index[guid]) == 1 {
			if prior := old.index.Resolve(guid); prior.Status == "resolved" {
				previousPath = prior.Path
			}
		}
		if invalidReference(*use.Reference.Asset) && !unreadableBefore[previousPath] &&
			!previousInvalid[referenceKey(use, previousPath)] {
			result.NewlyInvalid = append(result.NewlyInvalid, use)
		}
	}
	// Follow the reverse graph so a change to a nested Prefab can reach
	// the containing Prefab and the scenes which instantiate it.
	queue := sortedKeys(seeds)
	for cursor := 0; cursor < len(queue); cursor++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		for _, use := range reverse[queue[cursor]] {
			entry := affected[use.AssetPath]
			if entry == nil {
				entry = &AffectedAsset{Path: use.AssetPath, Via: []ReferenceUse{}}
				affected[entry.Path] = entry
			}
			entry.Via = append(entry.Via, use)
			guid := current.guidByPath[entry.Path]
			if guid != "" && !seeds[guid] {
				seeds[guid] = true
				queue = append(queue, guid)
			}
		}
	}
	for _, name := range sortedKeys(affected) {
		result.Affected = append(result.Affected, *affected[name])
	}
	return result, ctx.Err()
}

func changedAssetPaths(before, after *Version) map[string]AssetChange {
	paths := make(map[string]bool)
	for name := range before.files {
		paths[name] = true
	}
	for name := range after.files {
		paths[name] = true
	}
	changed := make(map[string]AssetChange)
	for name := range paths {
		if before.files[name] == after.files[name] || ignoredUnityPath(name) {
			continue
		}
		asset, meta := strings.CutSuffix(name, ".meta")
		item := changed[asset]
		item.Path = asset
		if meta {
			item.MetaChanged = true
		} else {
			item.AssetChanged = true
		}
		oldExists := before.files[asset].name != "" || before.files[asset+".meta"].name != ""
		newExists := after.files[asset].name != "" || after.files[asset+".meta"].name != ""
		item.Change = changeType(oldExists, newExists)
		changed[asset] = item
	}
	return changed
}

type referenceIdentity struct {
	consumer string
	object   string
	kind     string
	field    string
	guid     string
	fileID   string
}

func referenceKey(use ReferenceUse, consumer string) referenceIdentity {
	return referenceIdentity{consumer, use.Component.FileID, use.Component.Type, use.Field, use.Reference.Asset.GUID, use.Reference.FileID}
}
