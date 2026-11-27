package assetversion

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"unicode/utf8"
)

func isSerializedAsset(name string) bool {
	return path.Ext(name) == ".asset"
}

// ValidDiffPath extends the two structural reports without changing the
// Prefab/Scene scope of the reference graph and other snapshot readers.
func ValidDiffPath(name string) bool {
	return ValidUnityPath(name) || isSerializedAsset(name) && unityAssetPath(name)
}

func parseDiffDocuments(data []byte, name string) ([]document, string, error) {
	if isSerializedAsset(name) {
		body := bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if reason := skipReferenceFile(body); reason != "" {
			if reason == "not_unity_text" && (bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body)) {
				reason = "binary"
			}
			return nil, reason, nil
		}
	}
	docs, err := parseUnity(data)
	return docs, "", err
}

func assetSkipMessage(reason string) string {
	switch reason {
	case "binary":
		return "binary Unity serialization; field comparison requires text YAML"
	case "git_lfs_pointer":
		return "Git LFS pointer; the asset contents are unavailable in this snapshot"
	default:
		return "expected Unity text YAML"
	}
}

func assetObjects(docs []document) []*nodeGroup {
	names := make(map[string]int)
	for _, doc := range docs {
		names[assetObjectName(doc)]++
	}
	occurrences := make(map[string]int)
	result := make([]*nodeGroup, 0, len(docs))
	for _, doc := range docs {
		name := assetObjectName(doc)
		label := "/" + pointer(name)
		if names[name] > 1 {
			occurrences[name]++
			label += "[" + strconv.Itoa(occurrences[name]) + "]"
		}
		result = append(result, &nodeGroup{
			Node: Node{FileID: doc.id, Path: label, Kind: doc.kind}, components: []document{doc},
		})
	}
	return result
}

func assetObjectName(doc document) string {
	if name := text(doc.body, "m_Name"); name != "" {
		return name
	}
	return doc.kind
}

type ReferenceTarget struct {
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Path         string `json:"path,omitempty"`
	PreviousPath string `json:"previousPath,omitempty"`
	ObjectName   string `json:"objectName,omitempty"`
	ObjectType   string `json:"objectType,omitempty"`
}

type documentSide struct {
	version *Version
	index   GuidIndex
	path    string
	docs    []document
	locals  map[string]document
}

func (side *documentSide) groups() ([]*nodeGroup, error) {
	if !isSerializedAsset(side.path) {
		return groupUnity(side.docs, side.index)
	}
	side.locals = make(map[string]document, len(side.docs))
	for _, doc := range side.docs {
		side.locals[doc.id] = doc
	}
	return assetObjects(side.docs), nil
}

func (side documentSide) fields(doc document, opposite documentSide, after bool) (map[string]any, error) {
	fields := make(map[string]any)
	if doc.body == nil {
		return fields, nil
	}
	if err := flatten(doc.body, "", side.index, fields); err != nil {
		return nil, fmt.Errorf("%s #%s: %w", side.path, doc.id, err)
	}
	if side.locals != nil {
		for name, value := range fields {
			if ref, ok := value.(Reference); ok {
				ref.Target = side.target(ref, opposite, after)
				fields[name] = ref
			}
		}
	}
	return fields, nil
}

func (side documentSide) target(ref Reference, opposite documentSide, after bool) *ReferenceTarget {
	if ref.FileID == "0" || ref.Asset != nil && ref.Asset.Status == "null" {
		return &ReferenceTarget{Kind: "null", Status: "null"}
	}
	if ref.Asset == nil || ref.Asset.Status == "resolved" && ref.Asset.Path == side.path {
		target := &ReferenceTarget{Kind: "local", Status: "unresolved"}
		if doc, ok := side.locals[ref.FileID]; ok {
			target.Status, target.ObjectName, target.ObjectType = "resolved", assetObjectName(doc), doc.kind
		}
		return target
	}
	asset := ref.Asset
	if asset.Status == "builtin" {
		return &ReferenceTarget{Kind: "builtin", Status: "builtin"}
	}
	target := &ReferenceTarget{Kind: "asset", Status: asset.Status, Path: asset.Path}
	if !after {
		return target
	}
	previous := opposite.index.Resolve(asset.GUID)
	if previous.Status != "resolved" {
		return target
	}
	switch {
	case asset.Status == "resolved" && asset.Path != previous.Path:
		target.Status, target.PreviousPath = "moved", previous.Path
	case asset.Status == "unresolved" || asset.Status == "missing_asset":
		_, stillPresent := side.version.files[previous.Path]
		if !stillPresent {
			target.Status, target.Path = "deleted", previous.Path
		}
	}
	return target
}
