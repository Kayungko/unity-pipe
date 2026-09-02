package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditForwardsCategoriesAndOutput(t *testing.T) {
	ft := okExecTransport()
	if _, err := AuditCommand().Run(newCaptureRuntime(ft), []string{
		"--no-wait", "--categories", "Code,Texture", "--output", "Temp/a.csv",
	}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	cmd, params, _, _ := execBody(t, ft)
	if cmd != "audit" {
		t.Fatalf("command = %q, want audit", cmd)
	}
	if params["categories"] != "Code,Texture" {
		t.Fatalf("categories = %v", params["categories"])
	}
	if params["output"] != "Temp/a.csv" {
		t.Fatalf("output = %v", params["output"])
	}
}

func TestAuditOmitsEmptyParams(t *testing.T) {
	ft := okExecTransport()
	if _, err := AuditCommand().Run(newCaptureRuntime(ft), []string{"--no-wait"}); err != nil {
		t.Fatalf("audit: %v", err)
	}
	_, params, _, _ := execBody(t, ft)
	raw, _ := json.Marshal(params)
	if _, ok := params["categories"]; ok {
		t.Fatalf("empty categories leaked: %s", raw)
	}
	if _, ok := params["output"]; ok {
		t.Fatalf("empty output leaked: %s", raw)
	}
}

func TestAuditSynopsisDocumentsFlags(t *testing.T) {
	syn := AuditCommand().Synopsis
	for _, flag := range []string{"--categories", "--output", "--no-wait"} {
		if !strings.Contains(syn, flag) {
			t.Errorf("synopsis missing %s: %q", flag, syn)
		}
	}
}
