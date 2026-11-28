package assetversion

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
)

const (
	conflictOurs        = "ours_only"
	conflictTheirs      = "theirs_only"
	conflictSame        = "same_change"
	conflictIndependent = "independent_changes"
	conflictDiverged    = "conflict"
)

type ConflictValues[T any] struct {
	Base   *T `json:"base"`
	Ours   *T `json:"ours"`
	Theirs *T `json:"theirs"`
}

type ConflictFinding struct {
	Classification string `json:"classification"`
	Reason         string `json:"reason,omitempty"`
}

type ConflictField struct {
	Path string `json:"path"`
	ConflictValues[FieldValue]
	ConflictFinding
}

type ConflictComponent struct {
	ConflictValues[Component]
	ConflictFinding
	Fields []ConflictField `json:"fields"`
}

type ConflictNode struct {
	ConflictValues[Node]
	ConflictFinding
	Components []ConflictComponent `json:"components"`
}

type ConflictSnapshot struct {
	Identity
	Exists bool   `json:"exists"`
	P4Root string `json:"p4Root,omitempty"`
}

type AssetConflictResult struct {
	Path   string           `json:"path"`
	Base   ConflictSnapshot `json:"base"`
	Ours   ConflictSnapshot `json:"ours"`
	Theirs ConflictSnapshot `json:"theirs"`
	ConflictFinding
	HasConflicts bool           `json:"hasConflicts"`
	Nodes        []ConflictNode `json:"nodes"`
}

type conflictTree struct {
	snapshot ConflictSnapshot
	index    GuidIndex
	nodes    map[string]*nodeGroup
	data     []byte
	docs     []document
}

// ExplainConflict compares three versioned text assets, without loading or
// writing any working copy. Objects and components are paired by type and ID.
func ExplainConflict(ctx context.Context, base, ours, theirs *Version, name string) (AssetConflictResult, error) {
	result := AssetConflictResult{Path: name, Nodes: []ConflictNode{}}
	if !ValidUnityPath(name) {
		return result, fmt.Errorf("expected a project-relative Prefab or Scene path")
	}
	var sides [3]conflictTree
	for i, version := range []*Version{base, ours, theirs} {
		side, err := readConflictTree(ctx, version, name)
		if err != nil {
			return result, err
		}
		sides[i] = side
	}
	return explainConflictTrees(ctx, sides, name)
}

func explainConflictTrees(ctx context.Context, sides [3]conflictTree, name string) (AssetConflictResult, error) {
	result := AssetConflictResult{Path: name, Nodes: []ConflictNode{}}
	result.Base, result.Ours, result.Theirs = sides[0].snapshot, sides[1].snapshot, sides[2].snapshot
	if !result.Base.Exists && !result.Ours.Exists && !result.Theirs.Exists {
		return result, fmt.Errorf("%s is absent in all three snapshots", name)
	}

	nodeKeys := unionKeys(sides[0].nodes, sides[1].nodes, sides[2].nodes)
	for _, key := range nodeKeys {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		node, err := compareConflictNode(sides, key)
		if err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		if node.Classification != "" {
			result.Nodes = append(result.Nodes, node)
			result.Classification = combineConflict(result.Classification, node.Classification)
		}
	}
	result.ConflictFinding = classifyStructure(result.Base.Exists, result.Ours.Exists, result.Theirs.Exists, result.Classification)
	result.HasConflicts = result.Classification == conflictDiverged
	if result.Classification == "" {
		result.Classification = "unchanged"
	}
	return result, ctx.Err()
}

func readConflictTree(ctx context.Context, version *Version, name string) (conflictTree, error) {
	result := conflictTree{snapshot: ConflictSnapshot{Identity: version.Identity}}
	data, exists, err := version.readFile(ctx, name)
	result.snapshot.Exists = exists
	if err != nil || !exists {
		return result, err
	}
	docs, err := parseUnity(data)
	if err != nil {
		return result, fmt.Errorf("%s at %s: %w", name, version.Revision, err)
	}
	result.data, result.docs = data, docs
	result.index, err = version.readGUIDs(ctx, func(string) error { return nil })
	if err != nil {
		return result, err
	}
	groups, err := groupUnity(docs, result.index)
	if err != nil {
		return result, fmt.Errorf("%s at %s: %w", name, version.Revision, err)
	}
	result.nodes = make(map[string]*nodeGroup, len(groups))
	for _, group := range groups {
		result.nodes[group.Kind+":"+group.FileID] = group
	}
	return result, nil
}

func compareConflictNode(sides [3]conflictTree, key string) (ConflictNode, error) {
	var nodes [3]*Node
	var components [3]map[string]document
	for i, side := range sides {
		if group := side.nodes[key]; group != nil {
			nodes[i] = &group.Node
			components[i] = make(map[string]document, len(group.components))
			for _, doc := range group.components {
				components[i][doc.kind+":"+doc.id] = doc
			}
		}
	}
	result := ConflictNode{ConflictValues: valuesOf(nodes), Components: []ConflictComponent{}}
	for _, key := range unionKeys(components[0], components[1], components[2]) {
		docs := [3]document{components[0][key], components[1][key], components[2][key]}
		item, err := compareConflictComponent(sides, docs)
		if err != nil {
			return result, fmt.Errorf("component %s: %w", key, err)
		}
		if item.Classification != "" {
			result.Components = append(result.Components, item)
			result.Classification = combineConflict(result.Classification, item.Classification)
		}
	}
	result.ConflictFinding = classifyStructure(nodes[0] != nil, nodes[1] != nil, nodes[2] != nil, result.Classification)
	return result, nil
}

func compareConflictComponent(sides [3]conflictTree, docs [3]document) (ConflictComponent, error) {
	var components [3]*Component
	var fields [3]map[string]any
	for i, doc := range docs {
		if doc.body == nil {
			continue
		}
		components[i] = &Component{FileID: doc.id, Type: doc.kind}
		fields[i] = make(map[string]any)
		if err := flatten(doc.body, "", sides[i].index, fields[i]); err != nil {
			return ConflictComponent{}, err
		}
	}
	result := ConflictComponent{ConflictValues: valuesOf(components), Fields: []ConflictField{}}
	for _, key := range unionKeys(fields[0], fields[1], fields[2]) {
		values := ConflictValues[FieldValue]{
			Base: lookupConflictField(fields[0], key), Ours: lookupConflictField(fields[1], key),
			Theirs: lookupConflictField(fields[2], key),
		}
		finding := classifyField(values)
		if finding.Classification != "" {
			result.Fields = append(result.Fields, ConflictField{Path: key, ConflictValues: values, ConflictFinding: finding})
			result.Classification = combineConflict(result.Classification, finding.Classification)
		}
	}
	result.ConflictFinding = classifyStructure(components[0] != nil, components[1] != nil, components[2] != nil, result.Classification)
	return result, nil
}

func unionKeys[T any](groups ...map[string]T) []string {
	keys := make(map[string]bool)
	for _, group := range groups {
		for key := range group {
			keys[key] = true
		}
	}
	return sortedKeys(keys)
}

func valuesOf[T any](values [3]*T) ConflictValues[T] {
	return ConflictValues[T]{Base: values[0], Ours: values[1], Theirs: values[2]}
}

func lookupConflictField(fields map[string]any, key string) *FieldValue {
	value, exists := fields[key]
	if !exists {
		return nil
	}
	return &FieldValue{Value: value}
}

func classifyField(values ConflictValues[FieldValue]) ConflictFinding {
	ours := !equalConflictValue(values.Base, values.Ours)
	theirs := !equalConflictValue(values.Base, values.Theirs)
	same := equalConflictValue(values.Ours, values.Theirs)
	result := ConflictFinding{Classification: classifyChanges(ours, theirs, same)}
	if result.Classification == conflictDiverged {
		result.Reason = "different_values"
		if values.Ours == nil || values.Theirs == nil {
			result.Reason = "delete_modify"
		}
	}
	return result
}

func equalConflictValue(a, b *FieldValue) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if left, ok := a.Value.(Reference); ok {
		right, sameType := b.Value.(Reference)
		guid := func(reference Reference) string {
			if reference.Asset != nil {
				return reference.Asset.GUID
			}
			return ""
		}
		// The serialized reference is unchanged when its target moves or
		// becomes unresolvable in the snapshot.
		return sameType && left.FileID == right.FileID && left.Type == right.Type && guid(left) == guid(right)
	}
	if reflect.DeepEqual(a.Value, b.Value) {
		return true
	}
	left, leftNumber := a.Value.(json.Number)
	right, rightNumber := b.Value.(json.Number)
	if leftNumber && rightNumber {
		// Compare exact numeric values without losing large integer precision.
		leftValue, leftOK := new(big.Rat).SetString(left.String())
		rightValue, rightOK := new(big.Rat).SetString(right.String())
		return leftOK && rightOK && leftValue.Cmp(rightValue) == 0
	}
	return false
}

func classifyChanges(ours, theirs, same bool) string {
	switch {
	case ours && theirs:
		if same {
			return conflictSame
		}
		return conflictDiverged
	case ours:
		return conflictOurs
	case theirs:
		return conflictTheirs
	default:
		return ""
	}
}

func combineConflict(previous, next string) string {
	switch {
	case previous == "":
		return next
	case next == "" || next == previous:
		return previous
	case previous == conflictDiverged || next == conflictDiverged:
		return conflictDiverged
	default:
		return conflictIndependent
	}
}

func classifyStructure(base, ours, theirs bool, contents string) ConflictFinding {
	presence := classifyChanges(base != ours, base != theirs, ours == theirs)
	combined := combineConflict(presence, contents)
	switch {
	case base && ours != theirs && combined != presence:
		return ConflictFinding{Classification: conflictDiverged, Reason: "delete_modify"}
	case !base && ours && theirs && combined != conflictSame:
		return ConflictFinding{Classification: conflictDiverged, Reason: "concurrent_addition"}
	default:
		return ConflictFinding{Classification: combined}
	}
}
