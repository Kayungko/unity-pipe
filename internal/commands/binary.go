package commands

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Kayungko/unity-pipe/internal/protocol"
)

// Keep in sync with PipelineCommandResult.MaxBinaryBytes. The base64-encoded
// input and ordinary command parameters must fit the server's 1 MiB body cap.
const maxBinaryBytes = 512 * 1024

// BinaryOutput instructs the CLI renderer to write bytes without decoration.
type BinaryOutput []byte

func requireBinaryStdin(client *protocol.PipelineClient, name string) error {
	raw, err := client.CommandCatalog("flat", "", name, true)
	if err != nil {
		return err
	}
	var body struct {
		Commands []struct {
			Name               string
			AcceptsBinaryStdin bool
		}
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return protocol.ErrProtocol("invalid command catalog for binary stdin")
	}
	for _, command := range body.Commands {
		if command.Name == name && command.AcceptsBinaryStdin {
			return nil
		}
	}
	return usageErr("exec: binary stdin is unavailable for " + name)
}

func readBinaryStdin(r io.Reader) ([]byte, error) {
	input, err := io.ReadAll(io.LimitReader(r, maxBinaryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read binary stdin: %w", err)
	}
	if len(input) > maxBinaryBytes {
		return nil, usageErr(fmt.Sprintf("binary stdin exceeds the %d-byte limit", maxBinaryBytes))
	}
	return input, nil
}

func decodeBinaryOutput(result json.RawMessage) (BinaryOutput, error) {
	var output struct {
		Stdout *string `json:"stdoutBase64"`
	}
	if len(result) == 0 || !json.Valid(result) {
		return nil, protocol.ErrProtocol("invalid binary command result")
	}
	if err := json.Unmarshal(result, &output); err != nil || output.Stdout == nil {
		return nil, protocol.ErrCommandFailed(&protocol.ExecResponse{Error: "command did not return binary stdout"})
	}
	if len(*output.Stdout) > base64.StdEncoding.EncodedLen(maxBinaryBytes) {
		return nil, protocol.ErrProtocol("binary stdout exceeds the size limit")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(*output.Stdout)
	if err != nil || len(data) > maxBinaryBytes {
		return nil, protocol.ErrProtocol("invalid or oversized binary stdout")
	}
	return BinaryOutput(data), nil
}
