package commands

import "testing"

// TestVersionCommandReportsAllFields locks the version envelope shape: agents
// script against these keys, so silently dropping one is a breaking change.
func TestVersionCommandReportsAllFields(t *testing.T) {
	oldVersion, oldCommit, oldBuildDate := Version, Commit, BuildDate
	Version, Commit, BuildDate = "v9.9.9", "abc1234", "2026-01-01T00:00:00Z"
	defer func() { Version, Commit, BuildDate = oldVersion, oldCommit, oldBuildDate }()

	cmd := VersionCommand()
	if cmd.Name != "version" {
		t.Fatalf("unexpected command name %q", cmd.Name)
	}
	if !cmd.NoRuntime {
		t.Fatal("version must not require a running editor instance")
	}
	res, err := cmd.Run(&Runtime{}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("Run should return a map, got %T", res)
	}
	want := map[string]any{
		"version":     "v9.9.9",
		"commit":      "abc1234",
		"buildDate":   "2026-01-01T00:00:00Z",
		"vendoredPkg": VendoredPkgRef,
		"pkgSource":   VendoredPkgSource,
	}
	for k, wantV := range want {
		gotV, present := m[k]
		if !present {
			t.Errorf("missing field %q in version output", k)
			continue
		}
		if gotV != wantV {
			t.Errorf("field %q = %v, want %v", k, gotV, wantV)
		}
	}
}

// TestVersionDefaultsAreUnknownPlaceholders locks the fallback values used
// when a plain `go build` (no -ldflags) produces the binary: the JSON output
// must still carry human-readable placeholders, never an empty string.
func TestVersionDefaultsAreUnknownPlaceholders(t *testing.T) {
	if Commit == "" {
		t.Error("Commit default must not be empty (breaks JSON consumers expecting a placeholder)")
	}
	if BuildDate == "" {
		t.Error("BuildDate default must not be empty (breaks JSON consumers expecting a placeholder)")
	}
}
