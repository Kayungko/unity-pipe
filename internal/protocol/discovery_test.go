package protocol

import (
	"testing"
	"time"
)

const samplePortFile = `{
  "pid": 57004,
  "port": 7802,
  "projectPath": "E:\\P4\\SGAME_o_jiarongke_6099",
  "projectName": "SGAME_o_jiarongke_6099",
  "unityVersion": "2022.3.23f1",
  "mode": "editor",
  "startedAt": "2026-08-25T06:54:03.9777151Z",
  "lastHeartbeat": "2026-08-25T06:54:03.9777151Z",
  "evalToken": "redacted="
}`

func TestParseDescriptorValid(t *testing.T) {
	d, err := ParseDescriptor([]byte(samplePortFile))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Port != 7802 || d.Pid != 57004 || d.UnityVersion != "2022.3.23f1" || d.Mode != "editor" {
		t.Fatalf("bad decode: %+v", d)
	}
	if d.HostPort() != "127.0.0.1:7802" {
		t.Fatalf("bad HostPort: %s", d.HostPort())
	}
}

func TestParseDescriptorCorrupt(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"port": 0}`),     // missing port
		[]byte(`not json`),        // invalid json
		[]byte(`{"port": "abc"}`), // wrong type
	}
	for _, c := range cases {
		if _, err := ParseDescriptor(c); err == nil {
			t.Fatalf("expected error for %s", c)
		}
	}
}

func TestParseDescriptorToleratesUnknownFields(t *testing.T) {
	if _, err := ParseDescriptor([]byte(`{"port": 1, "futureField": 42}`)); err != nil {
		t.Fatalf("unknown field must be tolerated: %v", err)
	}
}

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	fresh := &InstanceDescriptor{LastHeartbeat: now.Add(-5 * time.Second)}
	stale := &InstanceDescriptor{LastHeartbeat: now.Add(-31 * time.Second)}
	if IsStale(fresh, now) {
		t.Fatal("fresh descriptor flagged stale")
	}
	if !IsStale(stale, now) {
		t.Fatal("stale descriptor not detected")
	}
}

func TestFindProjectRoot(t *testing.T) {
	existing := map[string]bool{
		`E:\proj`: true,
	}
	root, ok := FindProjectRoot(`E:\proj\Assets\Scripts`, func(dir string) bool { return existing[dir] })
	if !ok || root != `E:\proj` {
		t.Fatalf("want E:\\proj, got %q ok=%v", root, ok)
	}
	if _, ok := FindProjectRoot(`C:\elsewhere`, func(string) bool { return false }); ok {
		t.Fatal("expected not found")
	}
}
