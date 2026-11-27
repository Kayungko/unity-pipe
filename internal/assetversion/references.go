package assetversion

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type ReferenceOverride struct {
	TargetGUID   string `json:"targetGuid"`
	TargetFileID string `json:"targetFileId"`
	PropertyPath string `json:"propertyPath"`
}

type ReferenceUse struct {
	AssetPath string             `json:"assetPath"`
	Node      Node               `json:"node"`
	Component Component          `json:"component"`
	Field     string             `json:"field"`
	Reference Reference          `json:"reference"`
	Override  *ReferenceOverride `json:"override,omitempty"`
}

type SkippedReferenceAsset struct {
	AssetPath string `json:"assetPath"`
	Reason    string `json:"reason"`
}

type ReferenceSearchResult struct {
	Version      Identity                `json:"version"`
	Target       Resolution              `json:"target"`
	Under        string                  `json:"under,omitempty"`
	ScannedFiles int                     `json:"scannedFiles"`
	Complete     bool                    `json:"complete"`
	SkippedFiles []SkippedReferenceAsset `json:"skippedFiles"`
	Matches      []ReferenceUse          `json:"matches"`
}

func ValidReferenceScope(scope string) bool {
	return scope == "" || scope == "Assets" || scope == "Packages" ||
		(safeProjectPath(scope) && (strings.HasPrefix(scope, "Assets/") || strings.HasPrefix(scope, "Packages/")))
}

// FindReferences searches persisted object references in one fixed snapshot.
func (v *Version) FindReferences(ctx context.Context, guid, under string) (ReferenceSearchResult, error) {
	result := ReferenceSearchResult{
		Version: v.Identity, Under: under, Complete: true,
		SkippedFiles: []SkippedReferenceAsset{}, Matches: []ReferenceUse{},
	}
	if !ValidGUID(guid) || !ValidReferenceScope(under) {
		return result, fmt.Errorf("invalid reference search input")
	}
	index, err := v.GUIDs(ctx)
	if err != nil {
		return result, err
	}
	result.Target = index.Resolve(guid)

	pattern := regexp.MustCompile("(?i)" + guid)
	err = v.read(ctx, v.referenceFiles(under), func(ref file, data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if reason := skipReferenceFile(data); reason != "" {
			result.Complete = false
			result.SkippedFiles = append(result.SkippedFiles, SkippedReferenceAsset{AssetPath: ref.name, Reason: reason})
			return nil
		}
		result.ScannedFiles++
		if !pattern.Match(data) {
			return nil
		}
		matches, err := scanReferences(data, ref.name, result.Target.GUID, index)
		if err != nil {
			return fmt.Errorf("%s: %w", ref.name, err)
		}
		result.Matches = append(result.Matches, matches...)
		return nil
	})
	if err != nil {
		return result, err
	}
	return result, ctx.Err()
}

func withinScope(name, under string) bool {
	return under == "" || name == under || strings.HasPrefix(name, under+"/")
}

func (v *Version) referenceFiles(under string) []file {
	var files []file
	for name, ref := range v.files {
		if ValidUnityPath(name) && withinScope(name, under) {
			files = append(files, ref)
		}
	}
	slices.SortFunc(files, func(a, b file) int { return strings.Compare(a.name, b.name) })
	return files
}

func skipReferenceFile(data []byte) string {
	if documentHeader.Match(data) {
		return ""
	}
	if bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1")) {
		return "git_lfs_pointer"
	}
	return "not_unity_text"
}

func scanReferences(data []byte, name, guid string, index GuidIndex) ([]ReferenceUse, error) {
	docs, err := parseUnity(data)
	if err != nil {
		return nil, err
	}
	nodes, err := groupUnity(docs, index)
	if err != nil {
		return nil, err
	}
	var uses []ReferenceUse
	for _, node := range nodes {
		for _, doc := range node.components {
			fields := make(map[string]any)
			if err := flatten(doc.body, "", index, fields); err != nil {
				return nil, err
			}
			var matched []string
			for key, value := range fields {
				ref, ok := value.(Reference)
				if ok && ref.Asset != nil && (guid == "" || ref.Asset.GUID == guid) && ref.FileID != "0" && !referenceBookkeeping(doc.kind, key) {
					matched = append(matched, key)
				}
			}
			slices.Sort(matched)
			overrides := referenceOverrides(doc)
			for _, key := range matched {
				uses = append(uses, ReferenceUse{
					AssetPath: name, Node: node.Node, Component: Component{FileID: doc.id, Type: doc.kind},
					Field: key, Reference: fields[key].(Reference), Override: overrides[key],
				})
			}
		}
	}
	return uses, nil
}

func referenceBookkeeping(kind, field string) bool {
	switch field {
	case "/m_CorrespondingSourceObject", "/m_PrefabInstance", "/m_PrefabAsset":
		return true
	}
	if kind != "PrefabInstance" || !strings.HasPrefix(field, "/m_Modification/") {
		return false
	}
	return !strings.HasPrefix(field, "/m_Modification/m_Modifications/") || !strings.HasSuffix(field, "/objectReference")
}

func referenceOverrides(doc document) map[string]*ReferenceOverride {
	if doc.kind != "PrefabInstance" {
		return nil
	}
	out := make(map[string]*ReferenceOverride)
	for _, modification := range items(field(field(doc.body, "m_Modification"), "m_Modifications")) {
		key := modificationKey(modification)
		if key == "" {
			continue
		}
		target := field(modification, "target")
		out["/m_Modification/m_Modifications/"+pointer(key)+"/objectReference"] = &ReferenceOverride{
			TargetGUID: text(target, "guid"), TargetFileID: text(target, "fileID"), PropertyPath: text(modification, "propertyPath"),
		}
	}
	return out
}
