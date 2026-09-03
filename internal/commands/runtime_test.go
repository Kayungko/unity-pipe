package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// fakeTransport is a minimal protocol.Transport double for testing Close().
type fakeTransport struct{ closed bool }

func (f *fakeTransport) Do(req protocol.Request) (protocol.Response, error) {
	return protocol.Response{}, nil
}
func (f *fakeTransport) Close() error {
	f.closed = true
	return nil
}

func TestRuntimeCloseClosesTransport(t *testing.T) {
	ft := &fakeTransport{}
	rt := &Runtime{Transport: ft}
	rt.Close()
	if !ft.closed {
		t.Fatal("Runtime.Close() must close the underlying transport")
	}
}

func TestRuntimeCloseNilTransportDoesNotPanic(t *testing.T) {
	rt := &Runtime{}
	rt.Close() // must not panic
}

func TestPortFileSourceReadsAndParses(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "Library", "Pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"pid":1,"port":7802,"projectPath":"` + filepath.ToSlash(project) +
		`","projectName":"Fake","unityVersion":"2022.3.23f1","mode":"editor","evalToken":"t"}`
	if err := os.WriteFile(protocol.ProjectPortFile(project), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	source := PortFileSource(project)
	d, err := source()
	if err != nil {
		t.Fatalf("source(): %v", err)
	}
	if d.Port != 7802 || d.ProjectName != "Fake" {
		t.Fatalf("unexpected descriptor: %+v", d)
	}
}

func TestPortFileSourceMissingFileErrors(t *testing.T) {
	project := t.TempDir() // no port file written
	source := PortFileSource(project)
	if _, err := source(); err == nil {
		t.Fatal("expected an error for a missing port file")
	}
}

func TestResolveProjectPathExplicitWins(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveProjectPath(dir)
	if err != nil {
		t.Fatalf("ResolveProjectPath: %v", err)
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Fatalf("got %q, want absolute %q", got, abs)
	}
}

func TestResolveProjectPathFindsFromCwd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Library", "Pipeline"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protocol.ProjectPortFile(root), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "Assets", "Scripts")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveProjectPath("")
	if err != nil {
		t.Fatalf("ResolveProjectPath: %v", err)
	}
	rootAbs, _ := filepath.Abs(root)
	gotAbs, _ := filepath.Abs(got)
	if !strings.EqualFold(gotAbs, rootAbs) {
		t.Fatalf("got %q, want %q", gotAbs, rootAbs)
	}
}

func TestResolveProjectPathNoInstanceFound(t *testing.T) {
	// A directory with no ancestor holding a port file.
	dir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveProjectPath(""); err == nil {
		t.Fatal("expected an error when no project with a port file is found")
	}
}

func TestPublicDescriptorStripsSecrets(t *testing.T) {
	d := &protocol.InstanceDescriptor{
		Pid: 123, Port: 7802, ProjectPath: `E:\p`, ProjectName: "Fake",
		UnityVersion: "2022.3.23f1", Mode: "editor",
		StartedAt: time.Now(), LastHeartbeat: time.Now(),
		EvalToken: "super-secret-token",
	}
	m := PublicDescriptor(d)
	if _, present := m["evalToken"]; present {
		t.Fatal("PublicDescriptor must not leak evalToken")
	}
	for _, k := range []string{"pid", "port", "projectPath", "projectName", "unityVersion", "mode", "startedAt", "lastHeartbeat"} {
		if _, present := m[k]; !present {
			t.Errorf("PublicDescriptor missing field %q", k)
		}
	}
}
