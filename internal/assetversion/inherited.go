package assetversion

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type PrefabSource struct {
	InstancePath string     `json:"instancePath"`
	Asset        Resolution `json:"asset"`
	Issues       []string   `json:"issues,omitempty"`
}

type PrefabSourceChange struct {
	InstanceIDs []string      `json:"instanceIds"`
	Before      *PrefabSource `json:"before,omitempty"`
	After       *PrefabSource `json:"after,omitempty"`
	Nodes       []NodeChange  `json:"nodes"`
}

type InheritedDiff struct {
	Sources []PrefabSourceChange `json:"sources"`
}

type inheritedNode struct {
	Node
	name       string
	ordinal    int
	parent     *inheritedNode
	components []*inheritedRecord
}

type inheritedRecord struct {
	Component
	node    *inheritedNode
	fields  map[string]any
	unknown []string
	removed bool
}

type prefabExpansion struct {
	objects map[string]*inheritedRecord
	nodes   []*inheritedNode
	root    *inheritedNode
}

type prefabOccurrence struct {
	PrefabSource
	ids       []string
	expansion *prefabExpansion
	parent    *inheritedNode
	name      string
}

type prefabReader struct {
	ctx     context.Context
	version *Version
	index   GuidIndex
	docs    map[string][]document
	stack   map[string]bool
	sources []*prefabOccurrence
}

func (r *prefabReader) expand(docs []document, ids []string) (*prefabExpansion, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	expansion := &prefabExpansion{objects: make(map[string]*inheritedRecord)}
	var instances []document
	var stripped []document
	for _, doc := range docs {
		if doc.kind == "PrefabInstance" {
			instances = append(instances, doc)
			continue
		}
		if doc.stripped {
			stripped = append(stripped, doc)
			continue
		}
		if doc.kind == "GameObject" {
			node := &inheritedNode{Node: Node{FileID: doc.id, Kind: doc.kind}, name: text(doc.body, "m_Name")}
			expansion.nodes = append(expansion.nodes, node)
			expansion.objects[doc.id] = &inheritedRecord{node: node}
		}
	}
	children := make(map[string]*prefabOccurrence)
	for _, instance := range instances {
		source := r.index.Resolve(text(field(instance.body, "m_SourcePrefab"), "guid"))
		modifications := field(instance.body, "m_Modification")
		occurrence := &prefabOccurrence{
			PrefabSource: PrefabSource{Asset: source},
			ids:          append(slices.Clone(ids), instance.id),
			name:         unresolvedInstanceName(modifications, source.GUID),
		}
		children[instance.id] = occurrence
		r.sources = append(r.sources, occurrence)
		if source.Status != "resolved" {
			continue
		}
		if !ValidUnityPath(source.Path) || !strings.HasSuffix(source.Path, ".prefab") {
			occurrence.Issues = append(occurrence.Issues, "source is not a text Prefab path")
			continue
		}
		if r.stack[source.GUID] || len(ids) >= 127 {
			occurrence.Issues = append(occurrence.Issues, "cyclic or excessively deep Prefab graph")
			continue
		}
		childDocs, err := r.readSource(source.Path)
		if err != nil {
			return nil, err
		}
		r.stack[source.GUID] = true
		child, err := r.expand(childDocs, occurrence.ids)
		delete(r.stack, source.GUID)
		if err != nil {
			return nil, err
		}
		occurrence.expansion = child
		occurrence.apply(modifications, r.index)
		for id, record := range child.objects {
			projected, err := projectFileID(instance.id, id)
			if err != nil {
				return nil, err
			}
			if previous := expansion.objects[projected]; previous != nil && previous != record {
				return nil, fmt.Errorf("ambiguous Prefab object ID %s", projected)
			}
			expansion.objects[projected] = record
		}
	}
	for _, doc := range stripped {
		occurrence := children[localID(doc.body, "m_PrefabInstance")]
		if occurrence == nil || occurrence.expansion == nil {
			continue
		}
		sourceObject := field(doc.body, "m_CorrespondingSourceObject")
		if text(sourceObject, "guid") != occurrence.Asset.GUID {
			continue
		}
		if record := occurrence.expansion.objects[text(sourceObject, "fileID")]; record != nil {
			expansion.objects[doc.id] = record
		}
	}
	for _, doc := range docs {
		if doc.stripped || doc.kind == "PrefabInstance" {
			continue
		}
		owner := localID(doc.body, "m_GameObject")
		if doc.kind == "GameObject" {
			owner = doc.id
		}
		node := expansion.objects[owner]
		if node == nil {
			continue
		}
		fields := make(map[string]any)
		if err := flatten(doc.body, "", r.index, fields); err != nil {
			return nil, err
		}
		for _, key := range []string{"/m_CorrespondingSourceObject", "/m_PrefabInstance", "/m_PrefabAsset", "/m_GameObject", "/m_Father"} {
			delete(fields, key)
		}
		for key := range fields {
			if key == "/m_Children" || key == "/m_Component" || strings.HasPrefix(key, "/m_Children/") || strings.HasPrefix(key, "/m_Component/") {
				delete(fields, key)
			}
		}
		record := &inheritedRecord{Component: Component{FileID: doc.id, Type: doc.kind}, node: node.node, fields: fields}
		if expansion.objects[doc.id] != nil && doc.kind != "GameObject" {
			return nil, fmt.Errorf("ambiguous Prefab component ID %s", doc.id)
		}
		expansion.objects[doc.id] = record
		node.node.components = append(node.node.components, record)
	}
	for _, doc := range docs {
		if doc.stripped || (doc.kind != "Transform" && doc.kind != "RectTransform") {
			continue
		}
		if current, parent := expansion.objects[doc.id], expansion.objects[localID(doc.body, "m_Father")]; current != nil && parent != nil {
			current.node.parent = parent.node
		}
	}
	for _, instance := range instances {
		occurrence := children[instance.id]
		parent := expansion.objects[localID(field(instance.body, "m_Modification"), "m_TransformParent")]
		if parent != nil {
			occurrence.parent = parent.node
		}
		if occurrence.expansion == nil || occurrence.expansion.root == nil {
			continue
		}
		root := occurrence.expansion.root
		root.parent = occurrence.parent
		if root.parent == nil && expansion.root == nil {
			expansion.root = root // A variant can consist of a root instance.
		}
	}
	for _, node := range expansion.nodes {
		if node.parent == nil {
			expansion.root = node
			break
		}
	}
	return expansion, nil
}

func (r *prefabReader) readSource(name string) ([]document, error) {
	if docs, ok := r.docs[name]; ok {
		return docs, nil
	}
	data, exists, err := r.version.readFile(r.ctx, name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("missing Prefab source %s", name)
	}
	docs, err := parseUnity(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	r.docs[name] = docs
	return docs, nil
}

// A saved Prefab addresses a nested object with the source file ID XORed
// with the instance file ID. Explicit stripped-object aliases are collected too.
func projectFileID(instance, source string) (string, error) {
	a, err := strconv.ParseInt(instance, 10, 64)
	if err != nil {
		return "", err
	}
	b, err := strconv.ParseInt(source, 10, 64)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(a^b, 10), nil
}

func unresolvedInstanceName(modifications *yaml.Node, guid string) string {
	var name string
	for _, override := range items(field(modifications, "m_Modifications")) {
		if text(override, "propertyPath") != "m_Name" || text(field(override, "target"), "guid") != guid {
			continue
		}
		if name != "" {
			return "PrefabInstance"
		}
		name = text(override, "value")
	}
	if name == "" {
		return "PrefabInstance"
	}
	return name
}

func (p *prefabOccurrence) apply(modifications *yaml.Node, index GuidIndex) {
	lookup := func(target *yaml.Node) *inheritedRecord {
		if guid := text(target, "guid"); guid != "" && guid != p.Asset.GUID {
			return nil
		}
		return p.expansion.objects[text(target, "fileID")]
	}
	for _, key := range []string{"m_RemovedComponents", "m_RemovedGameObjects"} {
		for _, target := range items(field(modifications, key)) {
			if record := lookup(target); record != nil {
				record.removed = true
			}
		}
	}
	for _, override := range items(field(modifications, "m_Modifications")) {
		target := field(override, "target")
		record := lookup(target)
		property := text(override, "propertyPath")
		if record == nil {
			p.Issues = append(p.Issues, fmt.Sprintf("unresolved override target %s:%s (%s)", text(target, "guid"), text(target, "fileID"), property))
			continue
		}
		key := propertyPointer(property)
		if !record.apply(key, override, index) {
			if strings.HasSuffix(property, ".Array.size") {
				key = propertyPointer(strings.TrimSuffix(property, ".Array.size"))
			}
			record.unknown = append(record.unknown, key)
			p.Issues = append(p.Issues, fmt.Sprintf("unsupported override %s (%s)", property, record.FileID))
		}
	}
}

func propertyPointer(property string) string {
	if property == "" {
		return ""
	}
	// Unity uses dotted paths for fields and Array.data[n] for element overrides.
	p := strings.ReplaceAll(property, ".Array.data[", ".")
	p = strings.ReplaceAll(p, "]", "")
	parts := strings.Split(p, ".")
	for i := range parts {
		parts[i] = pointer(parts[i])
	}
	return "/" + strings.Join(parts, "/")
}

func (r *inheritedRecord) apply(key string, override *yaml.Node, index GuidIndex) bool {
	old, exists := r.fields[key]
	object := field(override, "objectReference")
	value := field(override, "value")
	_, isReference := old.(Reference)
	if value == nil || !exists {
		return false
	}
	if object != nil && (text(object, "fileID") != "0" || isReference) {
		updated := make(map[string]any, 1)
		if err := flatten(object, key, index, updated); err != nil {
			return false
		}
		r.fields[key] = updated[key]
		return true
	}
	switch old.(type) {
	case json.Number:
		if _, err := strconv.ParseFloat(value.Value, 64); err != nil || !json.Valid([]byte(value.Value)) {
			return false
		}
		r.fields[key] = json.Number(value.Value)
	case bool:
		r.fields[key] = value.Value == "1" || strings.EqualFold(value.Value, "true")
	default:
		r.fields[key] = value.Value
	}
	if key == "/m_Name" && r.Type == "GameObject" {
		r.node.name = value.Value
	}
	return true
}

func (n *inheritedNode) visiblePath() (string, bool) {
	var segments []string
	for node := n; node != nil; node = node.parent {
		if len(segments) >= 1024 {
			return "", false
		}
		for _, record := range node.components {
			if record.Type == "GameObject" && record.removed {
				return "", false
			}
		}
		segment := pointer(node.name)
		if node.ordinal > 0 {
			segment += "[" + strconv.Itoa(node.ordinal) + "]"
		}
		segments = append(segments, segment)
	}
	for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
		segments[i], segments[j] = segments[j], segments[i]
	}
	return "/" + strings.Join(segments, "/"), true
}

func inheritedSources(ctx context.Context, version *Version, index GuidIndex, docs []document) ([]*prefabOccurrence, error) {
	r := prefabReader{ctx: ctx, version: version, index: index, docs: make(map[string][]document), stack: make(map[string]bool)}
	expansion, err := r.expand(docs, nil)
	if err != nil {
		return nil, err
	}
	siblings := make(map[*inheritedNode]map[string][]*inheritedNode)
	collect := func(nodes []*inheritedNode) {
		for _, node := range nodes {
			if siblings[node.parent] == nil {
				siblings[node.parent] = make(map[string][]*inheritedNode)
			}
			names := siblings[node.parent]
			names[node.name] = append(names[node.name], node)
		}
	}
	collect(expansion.nodes)
	for _, source := range r.sources {
		if source.expansion != nil {
			collect(source.expansion.nodes)
		}
	}
	for _, names := range siblings {
		for _, nodes := range names {
			if len(nodes) > 1 {
				for i, node := range nodes {
					node.ordinal = i + 1
				}
			}
		}
	}
	for _, source := range r.sources {
		if source.expansion != nil && source.expansion.root != nil {
			source.InstancePath, _ = source.expansion.root.visiblePath()
		} else {
			parent := ""
			if source.parent != nil {
				parent, _ = source.parent.visiblePath()
			}
			source.InstancePath = parent + "/" + pointer(source.name)
		}
	}
	return r.sources, nil
}

func compareInherited(before, after []*prefabOccurrence) *InheritedDiff {
	out := &InheritedDiff{Sources: []PrefabSourceChange{}}
	for _, pair := range pairing(before, after, func(p *prefabOccurrence) string { return strings.Join(p.ids, "/") }) {
		left, right := pair.before, pair.after
		change := PrefabSourceChange{Nodes: []NodeChange{}}
		if left != nil {
			change.InstanceIDs, change.Before = left.ids, &left.PrefabSource
		}
		if right != nil {
			change.InstanceIDs, change.After = right.ids, &right.PrefabSource
		}
		if left != nil && right != nil && left.expansion != nil && right.expansion != nil && left.Asset.GUID == right.Asset.GUID {
			change.Nodes = compareInheritedNodes(left.expansion.nodes, right.expansion.nodes)
		}
		if !reflect.DeepEqual(change.Before, change.After) || len(change.Nodes) != 0 ||
			(change.Before != nil && (change.Before.Asset.Status != "resolved" || len(change.Before.Issues) != 0)) {
			out.Sources = append(out.Sources, change)
		}
	}
	return out
}

func compareInheritedNodes(before, after []*inheritedNode) []NodeChange {
	visible := func(nodes []*inheritedNode) []*inheritedNode {
		var result []*inheritedNode
		for _, node := range nodes {
			if path, ok := node.visiblePath(); ok {
				node.Path = path
				result = append(result, node)
			}
		}
		return result
	}
	var changes = []NodeChange{}
	for _, pair := range pairing(visible(before), visible(after), func(n *inheritedNode) string { return n.FileID }) {
		left, right := pair.before, pair.after
		change := NodeChange{Change: changeType(left != nil, right != nil), Components: []ComponentChange{}}
		var old, new []*inheritedRecord
		if left != nil {
			change.Before, old = &left.Node, left.components
		}
		if right != nil {
			change.After, new = &right.Node, right.components
		}
		for _, component := range pairing(old, new, func(r *inheritedRecord) string { return r.Type + ":" + r.FileID }) {
			a, b := component.before, component.after
			var delta ComponentChange
			var leftFields, rightFields map[string]any
			if a != nil && !a.removed {
				delta.Before, leftFields = &a.Component, a.fields
			}
			if b != nil && !b.removed {
				delta.After, rightFields = &b.Component, b.fields
			}
			if delta.Before == nil && delta.After == nil {
				continue
			}
			delta.Change = changeType(delta.Before != nil, delta.After != nil)
			delta.Fields = compareFields(leftFields, rightFields)
			var unknown []string
			if a != nil {
				unknown = append(unknown, a.unknown...)
			}
			if b != nil {
				unknown = append(unknown, b.unknown...)
			}
			delta.Fields = slices.DeleteFunc(delta.Fields, func(change FieldChange) bool {
				return slices.ContainsFunc(unknown, func(key string) bool {
					return key == "" || change.Path == key || strings.HasPrefix(change.Path, key+"/")
				})
			})
			if delta.Change != "modified" || len(delta.Fields) != 0 {
				change.Components = append(change.Components, delta)
			}
		}
		if change.Change != "modified" || len(change.Components) != 0 || left.Path != right.Path {
			changes = append(changes, change)
		}
	}
	return changes
}
