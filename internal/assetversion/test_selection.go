package assetversion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
)

// TestImpact describes the assembly filter derived from two project snapshots.
// RunAll is a conservative fallback when the inputs cannot prove a narrower scope.
type TestImpact struct {
	From       Identity `json:"from"`
	To         Identity `json:"to"`
	Changed    []string `json:"changed"`
	Assemblies []string `json:"assemblies"`
	RunAll     bool     `json:"runAll"`
	Reasons    []string `json:"reasons,omitempty"`
}

type assemblyDefinition struct {
	Name       string
	References []string
}

type assemblyReference struct {
	Reference string
}

type assemblyGraph struct {
	names   map[string]bool
	defs    map[string]assemblyDefinition
	folders map[string]string
	reverse map[string][]string
}

// TestSelection uses both revisions to handle deleted scripts, moved scripts
// and references removed in the new revision. Only names in the newer
// snapshot are selected for execution.
func TestSelection(ctx context.Context, before, after *Version) (TestImpact, error) {
	plan := TestImpact{From: before.Identity, To: after.Identity, Changed: []string{}, Assemblies: []string{}}
	paths := make(map[string]bool, len(before.files)+len(after.files))
	for name := range before.files {
		paths[name] = true
	}
	for name := range after.files {
		paths[name] = true
	}
	names := make([]string, 0, len(paths))
	for name := range paths {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if before.files[name] == after.files[name] {
			continue
		}
		plan.Changed = append(plan.Changed, name)
		base := strings.TrimSuffix(name, ".meta")
		switch strings.ToLower(path.Ext(base)) {
		case ".cs", ".asmdef", ".asmref":
		default:
			// A folder's Unity metadata doesn't change its scripts' assembly
			// ownership. Metadata of an actual non-code asset can affect tests.
			if base == name || paths[base] {
				plan.Reasons = append(plan.Reasons, "cannot scope changes to "+name)
			}
		}
	}
	if len(plan.Changed) == 0 {
		return plan, nil
	}
	fallback := func(reason string) (TestImpact, error) {
		if reason != "" {
			plan.Reasons = append(plan.Reasons, reason)
		}
		plan.RunAll = true
		return plan, nil
	}
	if len(plan.Reasons) > 0 {
		return fallback("")
	}

	oldGraph, err := buildAssemblyGraph(ctx, before, names)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return fallback("baseline: " + err.Error())
	}
	newGraph, err := buildAssemblyGraph(ctx, after, names)
	if err != nil {
		if ctx.Err() != nil {
			return plan, ctx.Err()
		}
		return fallback("target: " + err.Error())
	}

	seeds := map[string]bool{}
	addOwners := func(v *Version, graph assemblyGraph, name string) bool {
		if _, exists := v.files[name]; !exists {
			return true
		}
		owner := graph.owner(name)
		if owner == "" {
			return false
		}
		seeds[owner] = true
		return true
	}
	for _, name := range plan.Changed {
		base := strings.TrimSuffix(name, ".meta")
		switch strings.ToLower(path.Ext(base)) {
		case ".cs", ".asmdef", ".asmref":
			if !addOwners(before, oldGraph, base) || !addOwners(after, newGraph, base) {
				return fallback("cannot resolve assembly ownership for " + base)
			}
		}
	}
	// Adding/removing a definition or asmref can reassign unchanged scripts.
	for _, name := range names {
		if strings.ToLower(path.Ext(name)) != ".cs" {
			continue
		}
		if _, exists := before.files[name]; !exists {
			continue
		}
		if _, exists := after.files[name]; !exists {
			continue
		}
		if oldGraph.owner(name) != newGraph.owner(name) &&
			(!addOwners(before, oldGraph, name) || !addOwners(after, newGraph, name)) {
			return fallback("assembly ownership changed for " + name)
		}
	}

	queue := make([]string, 0, len(seeds))
	for assembly := range seeds {
		queue = append(queue, assembly)
	}
	for i := 0; i < len(queue); i++ {
		name := queue[i]
		for _, graph := range []assemblyGraph{oldGraph, newGraph} {
			for _, dependent := range graph.reverse[name] {
				if !seeds[dependent] {
					seeds[dependent] = true
					queue = append(queue, dependent)
				}
			}
		}
	}
	for name := range seeds {
		if newGraph.names[name] {
			plan.Assemblies = append(plan.Assemblies, name)
		}
	}
	slices.Sort(plan.Assemblies)
	return plan, nil
}

func (g assemblyGraph) owner(name string) string {
	if def, ok := g.defs[name]; ok {
		return def.Name
	}
	for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
		if owner, exists := g.folders[directory]; exists {
			return owner
		}
	}
	return ""
}

func buildAssemblyGraph(ctx context.Context, version *Version, names []string) (assemblyGraph, error) {
	g := assemblyGraph{
		names: map[string]bool{}, defs: map[string]assemblyDefinition{},
		folders: map[string]string{}, reverse: map[string][]string{},
	}
	var refs []file
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.HasSuffix(lower, ".asmdef") || strings.HasSuffix(lower, ".asmref") ||
			strings.HasSuffix(lower, ".asmdef.meta") {
			if ref, ok := version.files[name]; ok {
				refs = append(refs, ref)
			}
		}
	}
	metas := map[string]string{}
	guids := map[string]string{}
	asmrefs := map[string]assemblyReference{}
	err := version.read(ctx, refs, func(ref file, data []byte) error {
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		switch strings.ToLower(path.Ext(ref.name)) {
		case ".meta":
			guid := metaGUID.FindSubmatch(data)
			if guid == nil {
				return fmt.Errorf("invalid assembly GUID in %s", ref.name)
			}
			metas[strings.TrimSuffix(ref.name, ".meta")] = strings.ToLower(string(guid[1]))
		case ".asmdef":
			var def assemblyDefinition
			if json.Unmarshal(data, &def) != nil || strings.TrimSpace(def.Name) == "" || g.names[def.Name] {
				return fmt.Errorf("invalid or duplicate assembly definition at %s", ref.name)
			}
			g.names[def.Name], g.defs[ref.name] = true, def
		case ".asmref":
			var asmref assemblyReference
			if json.Unmarshal(data, &asmref) != nil || strings.TrimSpace(asmref.Reference) == "" {
				return fmt.Errorf("invalid assembly reference at %s", ref.name)
			}
			asmrefs[ref.name] = asmref
		}
		return nil
	})
	if err != nil {
		return g, err
	}
	setFolder := func(name, assembly string) error {
		dir := path.Dir(name)
		if _, exists := g.folders[dir]; exists {
			return fmt.Errorf("multiple assembly definitions or references in %s", dir)
		}
		g.folders[dir] = assembly
		return nil
	}
	for _, name := range names {
		def, exists := g.defs[name]
		if !exists {
			continue
		}
		if guid := metas[name]; guid != "" {
			if _, duplicate := guids[guid]; duplicate {
				return g, fmt.Errorf("duplicate assembly GUID in %s", name)
			}
			guids[guid] = def.Name
		}
		if err := setFolder(name, def.Name); err != nil {
			return g, err
		}
	}
	resolve := func(ref string) string {
		if strings.HasPrefix(strings.ToUpper(ref), "GUID:") {
			return guids[strings.ToLower(ref[5:])]
		}
		return ref
	}
	for _, name := range names {
		if asmref, exists := asmrefs[name]; exists {
			target := resolve(asmref.Reference)
			if !g.names[target] {
				return g, fmt.Errorf("unresolved assembly reference in %s", name)
			}
			if err := setFolder(name, target); err != nil {
				return g, err
			}
		}
		if def, exists := g.defs[name]; exists {
			for _, ref := range def.References {
				target := resolve(ref)
				if target == "" {
					return g, fmt.Errorf("unresolved assembly dependency in %s", name)
				}
				g.reverse[target] = append(g.reverse[target], def.Name)
			}
		}
	}
	return g, nil
}
