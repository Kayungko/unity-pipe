package assetversion

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (r AssetSummaryResult) Markdown() string {
	var out strings.Builder
	out.WriteString("# Unity asset change summary\n\n")
	fmt.Fprintf(&out, "%s %s → %s\n\n", inlineCode(r.To.Backend), inlineCode(r.From.Revision), inlineCode(r.To.Revision))
	if r.Under != "" {
		fmt.Fprintf(&out, "Scope: %s\n\n", inlineCode(r.Under))
	}

	fmt.Fprintf(&out, "## Newly invalid references (%d)\n\n", len(r.NewlyInvalid))
	if len(r.NewlyInvalid) == 0 {
		if r.Complete {
			out.WriteString("None.\n")
		} else {
			out.WriteString("None found in the scanned files.\n")
		}
	}
	for _, use := range r.NewlyInvalid {
		fmt.Fprintf(&out, "- %s · %s · %s %s → %s (%s)\n", inlineCode(use.AssetPath),
			inlineCode(use.Node.Path), inlineCode(use.Component.Type), inlineCode(referenceField(use)),
			inlineCode(use.Reference.Asset.GUID), inlineCode(use.Reference.Asset.Status))
	}

	if !r.Complete {
		out.WriteString("\n## Incomplete coverage\n\n")
		writeCoverage(&out, "Before", r.BeforeCoverage)
		writeCoverage(&out, "After", r.AfterCoverage)
	}

	fmt.Fprintf(&out, "\n## Asset changes (%d)\n\n", len(r.Changes))
	if len(r.Changes) == 0 {
		out.WriteString("None.\n")
	}
	for _, change := range r.Changes {
		location := inlineCode(change.Path)
		if change.FromPath != "" {
			location = inlineCode(change.FromPath) + " → " + location
		}
		fmt.Fprintf(&out, "- **%s** %s", change.Change, location)
		if change.MetaChanged && !change.AssetChanged {
			out.WriteString(" (.meta)")
		}
		out.WriteByte('\n')
		for _, node := range change.Nodes {
			writeNodeChange(&out, node)
		}
	}

	fmt.Fprintf(&out, "\n## Affected Prefabs and Scenes (%d)\n\n", len(r.Affected))
	if len(r.Affected) == 0 {
		out.WriteString("None.\n")
	}
	for _, entry := range r.Affected {
		fmt.Fprintf(&out, "- %s", inlineCode(entry.Path))
		if entry.Changed {
			out.WriteString(" (changed)")
		}
		out.WriteByte('\n')
		for _, use := range entry.Via {
			target := use.Reference.Asset.Path
			if target == "" {
				target = use.Reference.Asset.GUID
			}
			fmt.Fprintf(&out, "  - %s · %s %s → %s\n", inlineCode(use.Node.Path),
				inlineCode(use.Component.Type), inlineCode(referenceField(use)), inlineCode(target))
		}
	}
	return out.String()
}

func referenceField(use ReferenceUse) string {
	if use.Override != nil {
		return use.Override.PropertyPath
	}
	return use.Field
}

func writeCoverage(out *strings.Builder, side string, coverage SnapshotCoverage) {
	for _, file := range coverage.SkippedFiles {
		fmt.Fprintf(out, "- %s skipped %s (%s)\n", side, inlineCode(file.AssetPath), inlineCode(file.Reason))
	}
	for _, file := range coverage.InvalidMeta {
		fmt.Fprintf(out, "- %s has invalid metadata in %s\n", side, inlineCode(file))
	}
}

func writeNodeChange(out *strings.Builder, change NodeChange) {
	node := change.After
	if node == nil {
		node = change.Before
	}
	location := inlineCode(node.Path)
	if change.Before != nil && change.After != nil && change.Before.Path != change.After.Path {
		location = inlineCode(change.Before.Path) + " → " + location
	}
	fmt.Fprintf(out, "  - %s %s\n", change.Change, location)
	for _, component := range change.Components {
		item := component.After
		if item == nil {
			item = component.Before
		}
		fmt.Fprintf(out, "    - %s %s\n", component.Change, inlineCode(item.Type+" #"+item.FileID))
		// Added or removed components are already identified by type and ID.
		if component.Change != "modified" {
			continue
		}
		for _, field := range component.Fields {
			fmt.Fprintf(out, "      - %s (%s)\n", inlineCode(field.Path), fieldSummary(field))
		}
	}
}

func fieldSummary(field FieldChange) string {
	switch field.Change {
	case "added":
		return "added " + inlineCode(fieldPreview(field.After.Value))
	case "removed":
		return "removed " + inlineCode(fieldPreview(field.Before.Value))
	default:
		return inlineCode(fieldPreview(field.Before.Value)) + " → " + inlineCode(fieldPreview(field.After.Value))
	}
}

func fieldPreview(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "unavailable"
	}
	content := []rune(string(body))
	if len(content) > 120 {
		return string(content[:120]) + "…"
	}
	return string(content)
}

func inlineCode(value string) string {
	value = strings.NewReplacer("\r", "\\r", "\n", "\\n", "\x00", "\\0").Replace(value)
	fence := "`"
	for strings.Contains(value, fence) {
		fence += "`"
	}
	if len(fence) != 1 || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		return fence + " " + value + " " + fence
	}
	return fence + value + fence
}
