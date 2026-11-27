package commands

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

func TestBinaryRoundTripAndLimit(t *testing.T) {
	for _, data := range [][]byte{{}, {0, 13, 10, 255}, bytes.Repeat([]byte{0, 255}, maxBinaryBytes/2)} {
		input, err := readBinaryStdin(bytes.NewReader(data))
		if err != nil || input == nil || !bytes.Equal(input, data) {
			t.Fatalf("stdin length %d: %v", len(data), err)
		}
		wire, err := json.Marshal(map[string][]byte{"stdoutBase64": input})
		if err != nil {
			t.Fatal(err)
		}
		output, err := decodeBinaryOutput(wire)
		if err != nil || !bytes.Equal(output, data) {
			t.Fatalf("stdout length %d: %v", len(data), err)
		}
	}
	if _, err := readBinaryStdin(bytes.NewReader(make([]byte, maxBinaryBytes+1))); err == nil || protocol.AsUpipeError(err).Code != protocol.ExitUsage {
		t.Fatalf("oversized stdin: %v", err)
	}
}

func TestBinaryOutputRejectsIncompleteOrInvalidReply(t *testing.T) {
	cases := map[string]int{
		"{}":                         protocol.ExitCommandFailed,
		"{\"stdoutBase64\":null}":    protocol.ExitCommandFailed,
		"{\"stdoutBase64\":\"!\"}":   protocol.ExitProtocol,
		"{\"stdoutBase64\":\"AA==\"": protocol.ExitProtocol,
	}
	for body, code := range cases {
		output, err := decodeBinaryOutput(json.RawMessage(body))
		if output != nil || err == nil || protocol.AsUpipeError(err).Code != code {
			t.Errorf("%s: expected exit %d without output, got %q, %v", body, code, output, err)
		}
	}
}
