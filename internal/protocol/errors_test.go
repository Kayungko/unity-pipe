package protocol

import (
	"errors"
	"testing"
	"time"
)

// TestErrNoInstance locks the exit code / kind contract agents branch on.
func TestErrNoInstance(t *testing.T) {
	ue := ErrNoInstance("no editor running")
	if ue.Code != ExitNoInstance {
		t.Fatalf("Code = %d, want ExitNoInstance (%d)", ue.Code, ExitNoInstance)
	}
	if ue.Kind != "NoInstance" {
		t.Fatalf("Kind = %q, want NoInstance", ue.Kind)
	}
	if ue.Message != "no editor running" {
		t.Fatalf("Message = %q", ue.Message)
	}
	if ue.Retryable {
		t.Fatal("NoInstance must not be marked retryable — the caller must act (start the editor), not just wait")
	}
}

// TestAsUpipeErrorWrapsArbitraryError locks the fallback path: a plain error
// (not already an *UpipeError) becomes ExitInternal, preserving its message.
func TestAsUpipeErrorWrapsArbitraryError(t *testing.T) {
	plain := errors.New("boom")
	ue := AsUpipeError(plain)
	if ue.Code != ExitInternal {
		t.Fatalf("Code = %d, want ExitInternal (%d)", ue.Code, ExitInternal)
	}
	if ue.Kind != "Internal" {
		t.Fatalf("Kind = %q, want Internal", ue.Kind)
	}
	if ue.Message != "boom" {
		t.Fatalf("Message = %q, want %q", ue.Message, "boom")
	}
}

func TestAsUpipeErrorNilIsNil(t *testing.T) {
	if AsUpipeError(nil) != nil {
		t.Fatal("AsUpipeError(nil) must return nil")
	}
}

func TestAsUpipeErrorPassesThroughExisting(t *testing.T) {
	orig := ErrAuth("token expired")
	got := AsUpipeError(orig)
	if got != orig {
		t.Fatal("AsUpipeError should return the same *UpipeError instance unchanged, not rewrap it")
	}
}

// TestNopSleep locks that the no-op Sleep implementation used by tests
// (fakeTransport clients wire Sleep: NopSleep) never actually blocks and
// never panics, regardless of the duration passed.
func TestNopSleep(t *testing.T) {
	start := time.Now()
	NopSleep(time.Hour) // must return essentially immediately
	if time.Since(start) > time.Second {
		t.Fatal("NopSleep should not actually sleep")
	}
	NopSleep(0)
	NopSleep(-1)
}
