package assetversion

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
)

type IntegrityIssue struct {
	Kind       string        `json:"kind"`
	Path       string        `json:"path,omitempty"`
	GUID       string        `json:"guid,omitempty"`
	Candidates []string      `json:"candidates,omitempty"`
	Use        *ReferenceUse `json:"use,omitempty"`
}

type IntegrityResult struct {
	Version      Identity                `json:"version"`
	Under        string                  `json:"under,omitempty"`
	Complete     bool                    `json:"complete"`
	ScannedFiles int                     `json:"scannedFiles"`
	SkippedFiles []SkippedReferenceAsset `json:"skippedFiles"`
	Issues       []IntegrityIssue        `json:"issues"`
}

type assetInventory struct {
	index       GuidIndex
	guidByPath  map[string]string
	invalidMeta []string
}

func (v *Version) inventory(ctx context.Context) (assetInventory, error) {
	inventory := assetInventory{guidByPath: make(map[string]string)}
	var err error
	inventory.index, err = v.readGUIDs(ctx, func(name string) error {
		inventory.invalidMeta = append(inventory.invalidMeta, name)
		return nil
	})
	for guid, entries := range inventory.index {
		for _, entry := range entries {
			inventory.guidByPath[entry.name] = guid
		}
	}
	return inventory, err
}

func (v *Version) CheckIntegrity(ctx context.Context, under string) (IntegrityResult, error) {
	result := IntegrityResult{
		Version: v.Identity, Under: under, Issues: []IntegrityIssue{},
	}
	inventory, err := v.inventory(ctx)
	if err != nil {
		return result, err
	}
	for _, name := range inventory.invalidMeta {
		if withinScope(strings.TrimSuffix(name, ".meta"), under) {
			result.Issues = append(result.Issues, IntegrityIssue{Kind: "invalid_meta", Path: name})
		}
	}
	for _, guid := range sortedKeys(inventory.index) {
		entries := inventory.index[guid]
		if len(entries) > 1 && slices.ContainsFunc(entries, func(entry metadata) bool {
			return withinScope(entry.name, under)
		}) {
			issue := IntegrityIssue{Kind: "duplicate_guid", GUID: guid}
			for _, entry := range entries {
				issue.Candidates = append(issue.Candidates, entry.name)
			}
			result.Issues = append(result.Issues, issue)
		}
		for _, entry := range entries {
			if !entry.exists && withinScope(entry.name, under) {
				result.Issues = append(result.Issues, IntegrityIssue{Kind: "orphan_meta", Path: entry.name + ".meta", GUID: guid})
			}
		}
	}

	// Package manifests and package tooling need not have Unity metadata.
	// Report absent sidecars for importable Assets files and their folders.
	missing := make(map[string]bool)
	for name := range v.files {
		if !strings.HasPrefix(name, "Assets/") || strings.HasSuffix(name, ".meta") || ignoredUnityPath(name) {
			continue
		}
		for asset := name; asset != "Assets"; asset = path.Dir(asset) {
			if _, exists := v.files[asset+".meta"]; !exists && withinScope(asset, under) {
				missing[asset] = true
			}
		}
	}
	for _, name := range sortedKeys(missing) {
		result.Issues = append(result.Issues, IntegrityIssue{Kind: "missing_meta", Path: name})
	}

	scan, err := v.scanAllReferences(ctx, inventory.index, under)
	if err != nil {
		return result, err
	}
	result.ScannedFiles, result.SkippedFiles = scan.scanned, scan.skipped
	result.Complete = len(scan.skipped) == 0
	for _, use := range scan.uses {
		if invalidReference(*use.Reference.Asset) {
			result.Issues = append(result.Issues, IntegrityIssue{
				Kind: "invalid_reference", Path: use.AssetPath, GUID: use.Reference.Asset.GUID, Use: &use,
			})
		}
	}
	return result, ctx.Err()
}

func ignoredUnityPath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, "~") ||
			strings.EqualFold(part, "cvs") || strings.HasSuffix(strings.ToLower(part), ".tmp") {
			return true
		}
	}
	return false
}

func invalidReference(resolution Resolution) bool {
	switch resolution.Status {
	case "unresolved", "missing_asset", "ambiguous":
		return true
	}
	return false
}

type referenceScan struct {
	uses    []ReferenceUse
	skipped []SkippedReferenceAsset
	scanned int
}

func (v *Version) scanAllReferences(ctx context.Context, index GuidIndex, under string) (referenceScan, error) {
	scan := referenceScan{skipped: []SkippedReferenceAsset{}}
	err := v.read(ctx, v.referenceFiles(under), func(ref file, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if reason := skipReferenceFile(data); reason != "" {
			scan.skipped = append(scan.skipped, SkippedReferenceAsset{AssetPath: ref.name, Reason: reason})
			return nil
		}
		scan.scanned++
		uses, err := scanReferences(data, ref.name, "", index)
		if err != nil {
			return fmt.Errorf("%s: %w", ref.name, err)
		}
		scan.uses = append(scan.uses, uses...)
		return nil
	})
	return scan, err
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}
