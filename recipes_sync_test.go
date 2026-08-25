// recipes_sync_test.go keeps the distributed copy of RECIPES.md inside the
// unity-pipeline skill in lockstep with the canonical docs/RECIPES.md.
//
// The skill must be self-contained (upipe plugin install copies only
// harness/skills/*), so the recipe file ships as a bundled reference — but a
// second copy drifts silently. This test fails the build the moment someone
// edits docs/RECIPES.md without re-bundling:
//
//	cp docs/RECIPES.md harness/skills/unity-pipeline/references/RECIPES.md
package lint_test

import (
	"bytes"
	"os"
	"testing"
)

func TestRecipesInSync(t *testing.T) {
	canonical, err := os.ReadFile("docs/RECIPES.md")
	if err != nil {
		t.Fatalf("canonical docs/RECIPES.md missing: %v", err)
	}
	bundled, err := os.ReadFile("harness/skills/unity-pipeline/references/RECIPES.md")
	if err != nil {
		t.Fatalf("bundled skill reference missing: %v", err)
	}
	if !bytes.Equal(canonical, bundled) {
		t.Errorf("docs/RECIPES.md and harness/skills/unity-pipeline/references/RECIPES.md differ — re-bundle with:\n" +
			"  cp docs/RECIPES.md harness/skills/unity-pipeline/references/RECIPES.md")
	}
}
