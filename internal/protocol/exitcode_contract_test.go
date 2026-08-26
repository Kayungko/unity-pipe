package protocol

import "testing"

// TestExitCodeContract snapshots the exit-code contract documented in the
// unity-pipeline skill (§0). Agents branch on these numeric codes without
// parsing text — any change here is a breaking protocol change for every
// downstream consumer and must be versioned deliberately, never casually.
func TestExitCodeContract(t *testing.T) {
	want := map[string]int{
		"ExitOK":             0,  // success
		"ExitUsage":          2,  // command-line usage error (argparse convention)
		"ExitNoInstance":     10, // no running editor instance discovered
		"ExitConnectionLost": 11, // unreachable after the domain-reload retry ladder
		"ExitAuth":           12, // 401/403 — token invalid, editor restart likely
		"ExitBusy":           13, // 503 settling / 429 job queue full (retryable)
		"ExitCommandFailed":  20, // server reported ok=false (incl. failing tests)
		"ExitTimeout":        21, // client-side or server-side timeout
		"ExitJobCancelled":   22, // job was cancelled before completion
		"ExitProtocol":       30, // endpoint missing/unknown — package version mismatch
		"ExitInternal":       50, // unexpected internal error
	}

	// Values must stay distinct (branching logic would silently collapse).
	seen := map[int]string{}
	for name, code := range want {
		if other, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", name, other, code)
		}
		seen[code] = name
	}

	// Compare against the real constants.
	got := map[string]int{
		"ExitOK":             ExitOK,
		"ExitUsage":          ExitUsage,
		"ExitNoInstance":     ExitNoInstance,
		"ExitConnectionLost": ExitConnectionLost,
		"ExitAuth":           ExitAuth,
		"ExitBusy":           ExitBusy,
		"ExitCommandFailed":  ExitCommandFailed,
		"ExitTimeout":        ExitTimeout,
		"ExitJobCancelled":   ExitJobCancelled,
		"ExitProtocol":       ExitProtocol,
		"ExitInternal":       ExitInternal,
	}
	for name, code := range want {
		if got[name] != code {
			t.Errorf("exit-code contract drift: %s should be %d, is %d — this is a breaking "+
				"change for agents that branch on numeric codes (skill §0)", name, code, got[name])
		}
	}
}
