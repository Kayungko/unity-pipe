package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func TestBatchSourceAcceptsOnlyItsOwnEditor(t *testing.T) {
	current := &protocol.InstanceDescriptor{Pid: 42, Mode: "batchmode", EvalToken: "fixture"}
	editor := batchEditor{process: &os.Process{Pid: 42}, done: make(chan struct{})}
	before := editor.ownedSource(func() (*protocol.InstanceDescriptor, error) { return current, nil })
	if got, err := before(); got != current || err != nil {
		t.Fatalf("owned descriptor rejected: %v", err)
	}
	current.Pid = 43
	if _, err := before(); err == nil {
		t.Fatal("accepted another Editor's descriptor")
	}
	current.Pid, current.Mode = 42, "editor"
	if _, err := before(); err == nil {
		t.Fatal("accepted an interactive Editor")
	}
	current.Mode = "batchmode"
	close(editor.done)
	if _, err := before(); err == nil {
		t.Fatal("accepted a descriptor after its owned process exited")
	}
}

func TestBatchLogNeverOverwritesAnExistingFile(t *testing.T) {
	project := t.TempDir()
	target := filepath.Join(project, "preserved.txt")
	old := []byte("keep")
	if err := os.WriteFile(target, old, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveLog(project, target); err == nil {
		t.Fatal("reserved an existing file as the Editor log")
	}
	if saved, err := os.ReadFile(target); string(saved) != string(old) || err != nil {
		t.Fatalf("existing file modified: %q %v", saved, err)
	}
	fresh, err := reserveLog(project, "")
	if err != nil || filepath.Dir(fresh) != filepath.Join(project, "Logs") {
		t.Fatalf("default log location %s: %v", fresh, err)
	}
}
