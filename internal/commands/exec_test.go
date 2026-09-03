package commands

import "testing"

// TestMultiFlagCollectsAndStringifies locks multiFlag's flag.Value contract:
// repeated --set k=v flags accumulate, and String() renders them comma-joined
// (used by flag.PrintDefaults()/usage rendering).
func TestMultiFlagCollectsAndStringifies(t *testing.T) {
	var m multiFlag
	if got := m.String(); got != "" {
		t.Fatalf("empty multiFlag.String() = %q, want empty", got)
	}
	if err := m.Set("a=1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Set("b=2"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(m) != 2 || m[0] != "a=1" || m[1] != "b=2" {
		t.Fatalf("unexpected accumulated values: %v", m)
	}
	if got, want := m.String(), "a=1,b=2"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

// TestCoerceScalar locks the CLI->JSON type-inference contract that --set
// relies on: booleans/null/numbers get their JSON type, everything else
// stays a string (so a version string like "1.0" doesn't silently become a
// float and lose its trailing zero, etc — only bare-parseable numbers coerce).
func TestCoerceScalar(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"true", true},
		{"false", false},
		{"null", nil},
		{"42", int64(42)},
		{"-7", int64(-7)},
		{"3.14", 3.14},
		{"hello", "hello"},
		{"", ""},
		{"TRUE", "TRUE"}, // case-sensitive: only exact "true"/"false"/"null"
		{"1.0", 1.0},
		{"0x1A", "0x1A"}, // ParseInt base 10 rejects hex; stays a string
	}
	for _, c := range cases {
		got := coerceScalar(c.in)
		if got != c.want {
			t.Errorf("coerceScalar(%q) = %#v (%T), want %#v (%T)", c.in, got, got, c.want, c.want)
		}
	}
}
