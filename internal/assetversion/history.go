package assetversion

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type FieldSelector struct {
	Node        string `json:"node,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
	Component   string `json:"component,omitempty"`
	ComponentID string `json:"componentId,omitempty"`
	Field       string `json:"field"`
}

type FieldHistoryChange struct {
	Revision     string      `json:"revision"`
	Author       string      `json:"author"`
	Time         time.Time   `json:"time"`
	Path         string      `json:"path"`
	PreviousPath string      `json:"previousPath,omitempty"`
	Change       string      `json:"change"`
	Before       *FieldValue `json:"before,omitempty"`
	After        FieldValue  `json:"after"`
}

type FieldHistoryResult struct {
	At         Identity            `json:"at"`
	Path       string              `json:"path"`
	GUID       string              `json:"guid,omitempty"`
	Selector   FieldSelector       `json:"selector"`
	Status     string              `json:"status"`
	Reason     string              `json:"reason,omitempty"`
	Candidates []string            `json:"candidates,omitempty"`
	Node       *Node               `json:"node,omitempty"`
	Component  *Component          `json:"component,omitempty"`
	Value      *FieldValue         `json:"value,omitempty"`
	LastChange *FieldHistoryChange `json:"lastChange,omitempty"`
	Examined   int                 `json:"examined"`
}

func (s FieldSelector) Valid() bool {
	if (s.Node == "") == (s.NodeID == "") || (s.Component == "") == (s.ComponentID == "") ||
		!strings.HasPrefix(s.Field, "/") || (s.Node != "" && !strings.HasPrefix(s.Node, "/")) {
		return false
	}
	for _, id := range []string{s.NodeID, s.ComponentID} {
		if id != "" {
			number, err := strconv.ParseInt(id, 10, 64)
			if err != nil || strconv.FormatInt(number, 10) != id {
				return false
			}
		}
	}
	return !strings.Contains(strings.NewReplacer("~0", "", "~1", "").Replace(s.Field), "~")
}

const MaxFieldHistoryLimit = 10000

type historyStep struct {
	before       *Version
	mark         FieldHistoryChange
	mayIntroduce bool
}

type historyWalker func(context.Context, *Version, string) (historyStep, error)
type historyWitness func(context.Context, *FieldHistoryChange) error

func traceField(ctx context.Context, at *Version, name string, selector FieldSelector, limit int,
	step historyWalker, witness historyWitness) (FieldHistoryResult, error) {
	result := FieldHistoryResult{At: at.Identity, Path: name, Selector: selector}
	if !ValidUnityPath(name) || !selector.Valid() || limit < 1 || limit > MaxFieldHistoryLimit {
		return result, fmt.Errorf("invalid asset field history input")
	}
	docs, present, skip, err := readHistoryAsset(ctx, at, name)
	if err != nil {
		return result, err
	}
	if !present {
		result.Status = "asset_not_found"
		return result, nil
	}
	if skip != "" {
		return result.insufficient(skip), nil
	}
	guid, err := historyGUID(ctx, at, name)
	if err != nil {
		return result, err
	}
	result.GUID = guid
	index := GuidIndex(nil)
	for _, doc := range docs {
		if doc.kind != "PrefabInstance" {
			continue
		}
		// Match the node paths used by asset-diff for nested Prefab records.
		index, err = at.readGUIDs(ctx, func(string) error { return nil })
		if err != nil {
			return result, err
		}
		break
	}
	groups, err := groupUnity(docs, index)
	if err != nil {
		return result, fmt.Errorf("%s: %w", name, err)
	}
	component := result.selectComponent(groups)
	if component.body == nil {
		return result, nil
	}
	value, err := historyValue(component, selector.Field)
	if err != nil {
		return result, err
	}
	if value == nil {
		result.Status, result.Reason = "field_not_found", "property_not_serialized"
		return result, nil
	}
	result.Value = value

	current := at
	for result.Examined < limit {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		next, err := step(ctx, current, name)
		if errors.Is(err, errGitParentUnavailable) {
			return result.insufficient("shallow_history"), nil
		}
		if err != nil {
			return result, err
		}
		result.Examined++
		oldName, reason, err := previousHistoryPath(ctx, current, next.before, name, guid)
		if err != nil {
			return result, err
		}
		if reason != "" {
			return result.insufficient(reason), nil
		}
		if oldName == "" && !next.mayIntroduce {
			return result.insufficient("previous_asset_unavailable"), nil
		}
		var previous *FieldValue
		if oldName != "" {
			if current.files[name].object == next.before.files[oldName].object {
				previous = value
			} else {
				oldDocs, _, skip, err := readHistoryAsset(ctx, next.before, oldName)
				if err != nil {
					return result, err
				}
				if skip != "" {
					return result.insufficient(skip), nil
				}
				for _, old := range oldDocs {
					if old.id == component.id && old.kind == component.kind {
						previous, err = historyValue(old, selector.Field)
						if err != nil {
							return result, err
						}
						break
					}
				}
			}
		}
		if previous == nil || !reflect.DeepEqual(previous.Value, value.Value) {
			mark := next.mark
			mark.Path, mark.Before, mark.After = name, previous, *value
			if oldName != "" && name != oldName {
				mark.PreviousPath = oldName
			}
			mark.Change = changeType(previous != nil, true)
			if err := witness(ctx, &mark); err != nil {
				return result, err
			}
			result.Status, result.LastChange = "found", &mark
			return result, nil
		}
		current, name, value = next.before, oldName, previous
	}
	return result.insufficient("revision_limit"), nil
}

func (r FieldHistoryResult) insufficient(reason string) FieldHistoryResult {
	r.Status, r.Reason = "history_insufficient", reason
	return r
}

func (r *FieldHistoryResult) selectComponent(groups []*nodeGroup) document {
	var selected *nodeGroup
	for _, group := range groups {
		if (r.Selector.Node != "" && group.Path == r.Selector.Node) ||
			(r.Selector.NodeID != "" && group.FileID == r.Selector.NodeID) {
			selected = group
			r.Candidates = append(r.Candidates, group.FileID)
		}
	}
	if len(r.Candidates) > 1 {
		r.Status, r.Reason = "ambiguous", "multiple_nodes"
		return document{}
	}
	r.Candidates = nil
	if selected == nil {
		r.Status, r.Reason = "field_not_found", "node_not_found"
		return document{}
	}
	r.Node = &selected.Node
	var component document
	for _, doc := range selected.components {
		if (r.Selector.Component != "" && doc.kind == r.Selector.Component) ||
			(r.Selector.ComponentID != "" && doc.id == r.Selector.ComponentID) {
			component = doc
			r.Candidates = append(r.Candidates, doc.id)
		}
	}
	if len(r.Candidates) > 1 {
		r.Status, r.Reason = "ambiguous", "multiple_components"
		return document{}
	}
	r.Candidates = nil
	if component.body == nil {
		r.Status, r.Reason = "field_not_found", "component_not_found"
		return document{}
	}
	r.Component = &Component{FileID: component.id, Type: component.kind}
	return component
}

func readHistoryAsset(ctx context.Context, version *Version, name string) ([]document, bool, string, error) {
	data, exists, err := version.readFile(ctx, name)
	if err != nil || !exists {
		return nil, exists, "", err
	}
	if skip := skipReferenceFile(data); skip != "" {
		return nil, true, skip, nil
	}
	docs, err := parseUnity(data)
	if err != nil {
		return nil, true, "", fmt.Errorf("%s: %w", name, err)
	}
	return docs, true, "", nil
}

func historyValue(doc document, pointer string) (*FieldValue, error) {
	fields := make(map[string]any)
	if err := flatten(doc.body, "", nil, fields); err != nil {
		return nil, err
	}
	value, exists := fields[pointer]
	if !exists {
		return nil, nil
	}
	return &FieldValue{Value: value}, nil
}

func historyGUID(ctx context.Context, version *Version, name string) (string, error) {
	data, _, err := version.readFile(ctx, name+".meta")
	if err != nil {
		return "", err
	}
	match := metaGUID.FindSubmatch(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	if match == nil {
		return "", nil
	}
	return strings.ToLower(string(match[1])), nil
}

func previousHistoryPath(ctx context.Context, current, before *Version, name, guid string) (string, string, error) {
	if before.Revision == "empty" {
		return "", "", nil
	}
	_, samePath := before.files[name]
	if samePath {
		if guid == "" || current.files[name+".meta"].object == before.files[name+".meta"].object {
			return name, "", nil
		}
		priorGUID, err := historyGUID(ctx, before, name)
		if err != nil {
			return "", "", err
		}
		if priorGUID == guid {
			return name, "", nil
		}
		if priorGUID == "" {
			return "", "previous_asset_identity_unknown", nil
		}
	}
	if guid == "" {
		return "", "asset_guid_unavailable", nil
	}
	index, err := before.readGUIDs(ctx, func(string) error { return nil })
	if err != nil {
		return "", "", err
	}
	prior := index.Resolve(guid)
	switch prior.Status {
	case "ambiguous":
		return "", "ambiguous_previous_asset_guid", nil
	case "missing_asset":
		return "", "previous_asset_unavailable", nil
	case "resolved":
		if _, exists := before.files[prior.Path]; !exists || !ValidUnityPath(prior.Path) || path.Ext(prior.Path) != path.Ext(name) {
			return "", "previous_asset_unavailable", nil
		}
		return prior.Path, "", nil
	}
	return "", "", nil
}
