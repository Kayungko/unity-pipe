package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUnwrapJSONString(t *testing.T) {
	double := json.RawMessage(`"{\"status\":\"up_to_date\",\"failed\":false}"`)
	got := UnwrapJSONString(double)
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(got, &probe); err != nil || probe.Status != "up_to_date" {
		t.Fatalf("unwrap failed: %s err=%v", string(got), err)
	}
	// Plain objects pass through untouched.
	plain := json.RawMessage(`{"status":"completed"}`)
	if string(UnwrapJSONString(plain)) != `{"status":"completed"}` {
		t.Fatal("plain object must pass through")
	}
	// Plain strings that are not JSON stay as-is.
	notJSON := json.RawMessage(`"hello"`)
	if string(UnwrapJSONString(notJSON)) != `"hello"` {
		t.Fatal("non-JSON string must pass through")
	}
}

func TestExtractStatus(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"{\"status\":\"compiling\"}"`, "compiling"}, // double-encoded
		{`{"status":"completed"}`, "completed"},
		{`{"state":"queued"}`, "queued"},
		{`{"success":true}`, ""},
	}
	for _, c := range cases {
		if got := ExtractStatus(json.RawMessage(c.in)); got != c.want {
			t.Fatalf("ExtractStatus(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPollTerminalAndDeadline(t *testing.T) {
	// Terminal on the second poll.
	steps := []string{"compiling", "completed"}
	i := 0
	res, err := Poll(func() (PollStep, error) {
		s := steps[i]
		i++
		return PollStep{Status: s, Body: json.RawMessage(`{"status":"` + s + `"}`)}, nil
	}, PollConfig{TerminalOK: []string{"completed"}, Interval: time.Millisecond},
		NopSleep, time.Now)
	if err != nil || res.Status != "completed" || res.Attempts != 2 {
		t.Fatalf("bad result: %+v err=%v", res, err)
	}

	// Deadline exceeded.
	_, err = Poll(func() (PollStep, error) {
		return PollStep{Status: "compiling"}, nil
	}, PollConfig{TerminalOK: []string{"completed"}, Interval: time.Millisecond, Deadline: 5 * time.Millisecond},
		NopSleep, fakeClock)
	ue := AsUpipeError(err)
	if ue.Code != ExitTimeout {
		t.Fatalf("want timeout, got %+v", ue)
	}

	// Non-retryable error surfaces immediately.
	_, err = Poll(func() (PollStep, error) {
		return PollStep{}, ErrAuth("nope")
	}, PollConfig{TerminalOK: []string{"completed"}}, NopSleep, time.Now)
	if AsUpipeError(err).Code != ExitAuth {
		t.Fatalf("want auth passthrough, got %v", err)
	}

	// Connectivity blips (typed ErrConnectionLost, as produced by the client)
	// are retried.
	blips := 0
	res, err = Poll(func() (PollStep, error) {
		blips++
		if blips < 3 {
			return PollStep{}, ErrConnectionLost("connection refused")
		}
		return PollStep{Status: "completed"}, nil
	}, PollConfig{TerminalOK: []string{"completed"}, Interval: time.Millisecond},
		NopSleep, time.Now)
	if err != nil || res.Attempts != 3 {
		t.Fatalf("blips must be retried: %+v %v", res, err)
	}
}

// fakeClock advances 10ms per call so the deadline test terminates instantly.
var fakeClockTicks int

func fakeClock() time.Time {
	fakeClockTicks++
	return time.Now().Add(time.Duration(fakeClockTicks) * 10 * time.Millisecond)
}
