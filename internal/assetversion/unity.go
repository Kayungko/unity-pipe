package assetversion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

var documentHeader = regexp.MustCompile(`(?m)^---[ \t]+!u!([0-9]+)[ \t]+&(-?[0-9]+)([ \t]+stripped)?[ \t]*\r?$`)
var pointerEscapes = strings.NewReplacer("~", "~0", "/", "~1")

type document struct {
	id, kind string
	body     *yaml.Node
	stripped bool
}

func parseUnity(data []byte) ([]document, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	headers := documentHeader.FindAllSubmatchIndex(data, -1)
	if len(headers) == 0 {
		return nil, fmt.Errorf("expected Unity text YAML; binary and Git LFS pointers are unsupported")
	}
	result := make([]document, 0, len(headers))
	ids := make(map[string]bool)
	for i, header := range headers {
		end := len(data)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		id := string(data[header[4]:header[5]])
		var root yaml.Node
		if err := yaml.Unmarshal(data[header[1]:end], &root); err != nil {
			return nil, fmt.Errorf("read Unity object %s: %w", id, err)
		}
		if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode ||
			len(root.Content[0].Content) != 2 || ids[id] {
			return nil, fmt.Errorf("invalid Unity object %s", id)
		}
		ids[id] = true
		object := root.Content[0].Content
		result = append(result, document{id: id, kind: object[0].Value, body: object[1], stripped: header[6] != -1})
	}
	return result, nil
}

func field(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func text(node *yaml.Node, key string) string {
	if value := field(node, key); value != nil && value.Kind == yaml.ScalarNode {
		return value.Value
	}
	return ""
}

func items(node *yaml.Node) []*yaml.Node {
	if node != nil && node.Kind == yaml.SequenceNode {
		return node.Content
	}
	return nil
}

func localID(node *yaml.Node, key string) string { return text(field(node, key), "fileID") }

type Node struct {
	FileID string `json:"fileId"`
	Path   string `json:"path"`
	Kind   string `json:"kind"`
}

type Component struct {
	FileID string `json:"fileId"`
	Type   string `json:"type"`
}

type nodeGroup struct {
	Node
	name, parent string
	components   []document
}

func pointer(value string) string {
	return pointerEscapes.Replace(value)
}

func groupUnity(docs []document, index GuidIndex) ([]*nodeGroup, error) {
	nodes := make(map[string]*nodeGroup)
	ordered := make([]*nodeGroup, 0)
	byTransform := make(map[string]*nodeGroup)
	for _, doc := range docs {
		var name string
		switch doc.kind {
		case "GameObject":
			name = text(doc.body, "m_Name")
		case "PrefabInstance":
			source := index.Resolve(text(field(doc.body, "m_SourcePrefab"), "guid"))
			name = "PrefabInstance"
			if source.Path != "" {
				name = strings.TrimSuffix(path.Base(source.Path), ".prefab")
			}
		default:
			continue
		}
		group := &nodeGroup{Node: Node{FileID: doc.id, Kind: doc.kind}, name: name}
		nodes[doc.id] = group
		ordered = append(ordered, group)
	}
	children := make(map[string][]string)
	for _, doc := range docs {
		if doc.kind != "Transform" && doc.kind != "RectTransform" {
			continue
		}
		group := nodes[localID(doc.body, "m_GameObject")]
		if group == nil {
			group = nodes[localID(doc.body, "m_PrefabInstance")]
		}
		if group == nil {
			continue
		}
		byTransform[doc.id] = group
		if group.Kind != "PrefabInstance" {
			group.parent = localID(doc.body, "m_Father")
		}
		for _, child := range items(field(doc.body, "m_Children")) {
			children[doc.id] = append(children[doc.id], text(child, "fileID"))
		}
	}
	for _, doc := range docs {
		group := nodes[doc.id]
		if group != nil && group.Kind == "PrefabInstance" {
			group.parent = localID(field(doc.body, "m_Modification"), "m_TransformParent")
		}
		if group == nil {
			group = nodes[localID(doc.body, "m_GameObject")]
		}
		if group == nil {
			group = nodes[localID(doc.body, "m_PrefabInstance")]
		}
		if group == nil {
			// Scene settings and other records without a GameObject can still
			// contain serializable fields and references.
			group = &nodeGroup{Node: Node{FileID: doc.id, Kind: doc.kind}, name: doc.kind}
			ordered = append(ordered, group)
		}
		group.components = append(group.components, doc)
	}

	siblings := make(map[string][]*nodeGroup)
	assigned := make(map[string]bool)
	for parent, ids := range children {
		for _, id := range ids {
			if child := byTransform[id]; child != nil && child.parent == parent && !assigned[child.FileID] {
				siblings[parent] = append(siblings[parent], child)
				assigned[child.FileID] = true
			}
		}
	}
	for _, doc := range docs {
		if doc.kind != "SceneRoots" {
			continue
		}
		for _, root := range items(field(doc.body, "m_Roots")) {
			group := byTransform[text(root, "fileID")]
			if group != nil && !assigned[group.FileID] {
				group.parent = ""
				siblings[""] = append(siblings[""], group)
				assigned[group.FileID] = true
			}
		}
	}
	for _, group := range ordered {
		if byTransform[group.parent] == nil {
			group.parent = ""
		}
		if !assigned[group.FileID] {
			siblings[group.parent] = append(siblings[group.parent], group)
		}
	}
	for _, groups := range siblings {
		totals, positions := make(map[string]int), make(map[string]int)
		for _, group := range groups {
			totals[group.name]++
		}
		for _, group := range groups {
			name := pointer(group.name)
			if totals[group.name] > 1 {
				positions[group.name]++
				name += fmt.Sprintf("[%d]", positions[group.name])
			}
			group.Path = "/" + name
		}
	}
	merged := make(map[string]bool)
	visiting := make(map[string]bool)
	var buildPath func(*nodeGroup) error
	buildPath = func(group *nodeGroup) error {
		if merged[group.FileID] {
			return nil
		}
		if visiting[group.FileID] {
			return fmt.Errorf("cyclic Unity hierarchy at %s", group.FileID)
		}
		visiting[group.FileID] = true
		if parent := byTransform[group.parent]; parent != nil {
			if err := buildPath(parent); err != nil {
				return err
			}
			group.Path = parent.Path + group.Path
		}
		delete(visiting, group.FileID)
		merged[group.FileID] = true
		return nil
	}
	for _, group := range ordered {
		if err := buildPath(group); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

type Reference struct {
	FileID      string      `json:"fileId"`
	Type        string      `json:"type,omitempty"`
	Asset       *Resolution `json:"asset,omitempty"`
	InstanceIDs []string    `json:"instanceIds,omitempty"`
}

func scalar(value *yaml.Node) any {
	switch value.Tag {
	case "!!null":
		return nil
	case "!!bool":
		return strings.EqualFold(value.Value, "true")
	case "!!int", "!!float":
		if json.Valid([]byte(value.Value)) {
			return json.Number(value.Value)
		}
	}
	return value.Value
}

func flatten(node *yaml.Node, prefix string, index GuidIndex, out map[string]any) error {
	return flattenWithArrays(node, prefix, index, out, nil)
}

func flattenWithArrays(node *yaml.Node, prefix string, index GuidIndex, out map[string]any, lengths map[string]int) error {
	if _, exists := out[prefix]; exists {
		return fmt.Errorf("duplicate Unity field %s", prefix)
	}
	switch node.Kind {
	case yaml.ScalarNode:
		out[prefix] = scalar(node)
	case yaml.MappingNode:
		id, guid := text(node, "fileID"), text(node, "guid")
		if id != "" && (len(node.Content) == 2 || ValidGUID(guid)) {
			ref := Reference{FileID: id, Type: text(node, "type")}
			if guid != "" {
				asset := index.Resolve(guid)
				ref.Asset = &asset
			}
			out[prefix] = ref
			return nil
		}
		if len(node.Content) == 0 {
			out[prefix] = map[string]any{}
		}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("unsupported Unity field key at %s", prefix)
			}
			if err := flattenWithArrays(node.Content[i+1], prefix+"/"+pointer(key.Value), index, out, lengths); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		if lengths != nil {
			lengths[prefix] = len(node.Content)
		}
		if len(node.Content) == 0 {
			out[prefix] = []any{}
		}
		for i, item := range node.Content {
			key := strconv.Itoa(i)
			if prefix == "/m_Modification/m_Modifications" {
				target := field(item, "target")
				if property := text(item, "propertyPath"); property != "" {
					key = text(target, "guid") + ":" + text(target, "fileID") + ":" + property
				}
			}
			if err := flattenWithArrays(item, prefix+"/"+pointer(key), index, out, lengths); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported Unity field at %s", prefix)
	}
	return nil
}

type FieldValue struct {
	Value any `json:"value"`
}

type FieldChange struct {
	Path   string      `json:"path"`
	Change string      `json:"change"`
	Before *FieldValue `json:"before,omitempty"`
	After  *FieldValue `json:"after,omitempty"`
}

type ComponentChange struct {
	Change string        `json:"change"`
	Before *Component    `json:"before,omitempty"`
	After  *Component    `json:"after,omitempty"`
	Fields []FieldChange `json:"fields"`
}

type NodeChange struct {
	Change     string            `json:"change"`
	Before     *Node             `json:"before,omitempty"`
	After      *Node             `json:"after,omitempty"`
	Components []ComponentChange `json:"components"`
}

type DiffResult struct {
	From       Identity       `json:"from"`
	To         Identity       `json:"to"`
	FromPath   string         `json:"fromPath"`
	ToPath     string         `json:"toPath"`
	FromExists bool           `json:"fromExists"`
	ToExists   bool           `json:"toExists"`
	Nodes      []NodeChange   `json:"nodes"`
	Inherited  *InheritedDiff `json:"inherited,omitempty"`
}

func ValidUnityPath(name string) bool {
	return safeProjectPath(name) && (strings.HasPrefix(name, "Assets/") || strings.HasPrefix(name, "Packages/")) &&
		(path.Ext(name) == ".prefab" || path.Ext(name) == ".unity")
}

func Diff(ctx context.Context, before, after *Version, oldPath, newPath string) (DiffResult, error) {
	return diff(ctx, before, after, oldPath, newPath, false)
}

func DiffWithInheritance(ctx context.Context, before, after *Version, oldPath, newPath string) (DiffResult, error) {
	return diff(ctx, before, after, oldPath, newPath, true)
}

func diff(ctx context.Context, before, after *Version, oldPath, newPath string, includeInherited bool) (DiffResult, error) {
	result := DiffResult{From: before.Identity, To: after.Identity, FromPath: oldPath, ToPath: newPath, Nodes: []NodeChange{}}
	oldData, oldExists, err := before.readFile(ctx, oldPath)
	if err != nil {
		return result, err
	}
	newData, newExists, err := after.readFile(ctx, newPath)
	if err != nil {
		return result, err
	}
	if !oldExists && !newExists {
		return result, fmt.Errorf("asset is absent in both selected versions")
	}
	result.FromExists, result.ToExists = oldExists, newExists
	oldIndex, err := before.GUIDs(ctx)
	if err != nil {
		return result, err
	}
	newIndex, err := after.GUIDs(ctx)
	if err != nil {
		return result, err
	}
	load := func(data []byte, exists bool) ([]document, error) {
		if !exists {
			return nil, nil
		}
		return parseUnity(data)
	}
	oldDocs, err := load(oldData, oldExists)
	if err != nil {
		return result, fmt.Errorf("%s: %w", oldPath, err)
	}
	newDocs, err := load(newData, newExists)
	if err != nil {
		return result, fmt.Errorf("%s: %w", newPath, err)
	}
	oldGroups, err := groupUnity(oldDocs, oldIndex)
	if err != nil {
		return result, fmt.Errorf("%s: %w", oldPath, err)
	}
	newGroups, err := groupUnity(newDocs, newIndex)
	if err != nil {
		return result, fmt.Errorf("%s: %w", newPath, err)
	}
	for _, pair := range pairing(oldGroups, newGroups, func(group *nodeGroup) string {
		return group.Kind + ":" + group.FileID
	}) {
		old, new := pair.before, pair.after
		diff := NodeChange{Change: changeType(old != nil, new != nil), Components: []ComponentChange{}}
		var left, right []document
		if old != nil {
			diff.Before, left = &old.Node, old.components
		}
		if new != nil {
			diff.After, right = &new.Node, new.components
		}
		for _, component := range pairing(left, right, func(doc document) string { return doc.kind + ":" + doc.id }) {
			result, err := diffComponent(component.before, component.after, oldIndex, newIndex)
			if err != nil {
				return DiffResult{}, err
			}
			if result.Change != "modified" || len(result.Fields) != 0 {
				diff.Components = append(diff.Components, result)
			}
		}
		if diff.Change != "modified" || len(diff.Components) != 0 || old.Path != new.Path {
			result.Nodes = append(result.Nodes, diff)
		}
	}
	if includeInherited {
		oldSources, err := inheritedSources(ctx, before, oldIndex, oldDocs)
		if err != nil {
			return result, fmt.Errorf("%s: %w", oldPath, err)
		}
		newSources, err := inheritedSources(ctx, after, newIndex, newDocs)
		if err != nil {
			return result, fmt.Errorf("%s: %w", newPath, err)
		}
		result.Inherited = compareInherited(oldSources, newSources)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

type pair[T any] struct{ before, after T }

// Unity local file IDs are scoped to one asset and survive regular editing,
// reordering, and renames. Pair by ID and type to retain equal-name siblings.
func pairing[T any](old, new []T, key func(T) string) []pair[T] {
	after := make(map[string]T)
	for _, item := range new {
		after[key(item)] = item
	}
	result := make([]pair[T], 0, len(old)+len(new))
	for _, item := range old {
		id := key(item)
		result = append(result, pair[T]{item, after[id]})
		delete(after, id)
	}
	for _, item := range new {
		if _, exists := after[key(item)]; exists {
			result = append(result, pair[T]{after: item})
		}
	}
	return result
}

func changeType(before, after bool) string {
	if !before {
		return "added"
	}
	if !after {
		return "removed"
	}
	return "modified"
}

func diffComponent(before, after document, a, b GuidIndex) (ComponentChange, error) {
	result := ComponentChange{Change: changeType(before.body != nil, after.body != nil), Fields: []FieldChange{}}
	oldFields, newFields := make(map[string]any), make(map[string]any)
	if before.body != nil {
		result.Before = &Component{FileID: before.id, Type: before.kind}
		if err := flatten(before.body, "", a, oldFields); err != nil {
			return result, err
		}
	}
	if after.body != nil {
		result.After = &Component{FileID: after.id, Type: after.kind}
		if err := flatten(after.body, "", b, newFields); err != nil {
			return result, err
		}
	}
	result.Fields = compareFields(oldFields, newFields)
	return result, nil
}

func compareFields(oldFields, newFields map[string]any) []FieldChange {
	result := []FieldChange{}
	keys := make([]string, 0, len(oldFields)+len(newFields))
	for field := range oldFields {
		keys = append(keys, field)
	}
	for field := range newFields {
		keys = append(keys, field)
	}
	slices.Sort(keys)
	for _, field := range slices.Compact(keys) {
		old, left := oldFields[field]
		new, right := newFields[field]
		if left && right && reflect.DeepEqual(old, new) {
			continue
		}
		change := FieldChange{Path: field, Change: changeType(left, right)}
		if left {
			change.Before = &FieldValue{old}
		}
		if right {
			change.After = &FieldValue{new}
		}
		result = append(result, change)
	}
	return result
}
