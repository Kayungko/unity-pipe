package protocol

import (
	"fmt"
	"strings"
	"time"
)

// DefaultReloadLadder is the built-in backoff schedule used while an editor
// is going through a domain reload: during that window the port and eval
// token are rewritten and connections are dropped. Every attempt must
// re-read the port file. It is the fallback ReloadLadder resets to.
var DefaultReloadLadder = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// ReloadLadder is the backoff schedule currently in effect. It starts as a
// copy of DefaultReloadLadder; SetReloadLadder overrides it (e.g. from the
// UPIPE_RELOAD_LADDER environment variable, wired by the cli layer — this
// package stays IO-free and never reads the environment itself).
var ReloadLadder = append([]time.Duration(nil), DefaultReloadLadder...)

// SetReloadLadder replaces the active backoff schedule. A nil or empty slice
// resets it to DefaultReloadLadder. Callers own validation (ParseLadder
// rejects non-positive/unparseable entries before this is called).
func SetReloadLadder(ladder []time.Duration) {
	if len(ladder) == 0 {
		ReloadLadder = append([]time.Duration(nil), DefaultReloadLadder...)
		return
	}
	ReloadLadder = append([]time.Duration(nil), ladder...)
}

// ParseLadder parses a comma-separated list of Go durations (e.g.
// "200ms,500ms,1s,2s,5s,10s,30s") into a backoff ladder. Pure parsing logic —
// no environment access — so it is unit-testable and reusable by any caller
// that wires an override in (the cli layer reads UPIPE_RELOAD_LADDER and
// calls this). Every entry must parse as a positive duration; the first
// failure aborts with a descriptive error and parses nothing partially.
func ParseLadder(s string) ([]time.Duration, error) {
	parts := strings.Split(s, ",")
	out := make([]time.Duration, 0, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("entry %d is empty", i+1)
		}
		d, err := time.ParseDuration(p)
		if err != nil {
			return nil, fmt.Errorf("entry %d (%q): %w", i+1, p, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("entry %d (%q): must be positive", i+1, p)
		}
		out = append(out, d)
	}
	return out, nil
}

// Backoff returns the delay before retry attempt n (0-based). Attempts beyond
// the ladder keep the last (longest) delay.
func Backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(ReloadLadder) {
		attempt = len(ReloadLadder) - 1
	}
	return ReloadLadder[attempt]
}

// SleepFunc abstracts sleeping so tests can run ladders instantly.
type SleepFunc func(time.Duration)

// NopSleep does nothing (tests).
func NopSleep(time.Duration) {}
