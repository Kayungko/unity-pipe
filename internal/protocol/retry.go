package protocol

import "time"

// ReloadLadder is the backoff schedule used while an editor is going through
// a domain reload: during that window the port and eval token are rewritten
// and connections are dropped. Every attempt must re-read the port file.
var ReloadLadder = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
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
