package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// The per-request timeout must extend beyond the old hardcoded 120s client
// cap: a request with an explicit long Timeout (slow eval) must not be
// clipped. Regression for the "--timeout only reached the request body"
// defect where a 5-minute eval was killed at 120s and misreported as a
// connection error.
func TestDoRespectsLongRequestTimeout(t *testing.T) {
	// Server that takes 300ms — longer than the old 120ms-class defaults
	// would allow if we scaled the test down. We assert success with a
	// timeout ABOVE the handler delay and failure with one BELOW it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	tr := New()
	defer tr.Close()

	hostPort := srv.Listener.Addr().String()

	// Generous timeout: request completes.
	resp, err := tr.Do(protocol.Request{Method: "GET", Path: "/", HostPort: hostPort, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("long-timeout request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}

	// Timeout shorter than the handler delay: must fail (deadline exceeded).
	if _, err := tr.Do(protocol.Request{Method: "GET", Path: "/", HostPort: hostPort, Timeout: 50 * time.Millisecond}); err == nil {
		t.Fatal("short-timeout request unexpectedly succeeded")
	}

	// Zero timeout falls back to the transport default (120s) — must not
	// fail here (300ms handler) and must not panic with a zero context.
	if _, err := tr.Do(protocol.Request{Method: "GET", Path: "/", HostPort: hostPort}); err != nil {
		t.Fatalf("default-timeout request failed: %v", err)
	}
}

// A timeout above the documented 24h server ceiling is clamped, not rejected.
func TestDoClampsTimeoutToMax(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	tr := New()
	defer tr.Close()

	if _, err := tr.Do(protocol.Request{
		Method: "GET", Path: "/", HostPort: srv.Listener.Addr().String(),
		Timeout: 48 * time.Hour,
	}); err != nil {
		t.Fatalf("clamped request failed: %v", err)
	}
}
