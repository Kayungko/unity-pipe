package assetversion

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// OpenGit resolves a project's directory within a Git tree, without reading
// asset bytes from the working tree.
func OpenGit(ctx context.Context, dir, revision string) (*Version, error) {
	commitBytes, err := run(ctx, dir, "git", "rev-parse", "--verify", "--end-of-options", revision+"^{commit}")
	if err != nil {
		return nil, err
	}
	commit := strings.TrimSpace(string(commitBytes))
	if len(commit) != 40 && len(commit) != 64 {
		return nil, fmt.Errorf("unexpected Git commit ID")
	}
	tree, err := run(ctx, dir, "git", "ls-tree", "-rz", commit, "--", "Assets", "Packages")
	if err != nil {
		return nil, err
	}
	paths := make(map[string]file)
	for _, line := range bytes.Split(bytes.TrimSuffix(tree, []byte{0}), []byte{0}) {
		if len(line) == 0 {
			continue
		}
		header, name, ok := strings.Cut(string(line), "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || !safeProjectPath(name) {
			return nil, fmt.Errorf("invalid Git tree entry")
		}
		if fields[1] == "blob" && fields[0] != "120000" {
			paths[name] = file{name, fields[2]}
		}
	}
	return &Version{
		Identity: Identity{"git", commit},
		files:    paths,
		read: func(ctx context.Context, refs []file, consume func(file, []byte) error) error {
			return readGitBlobs(ctx, dir, refs, consume)
		},
	}, nil
}

const maxFileBytes = 64 << 20

func readGitBlobs(ctx context.Context, dir string, refs []file, consume func(file, []byte) error) error {
	if len(refs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = dir
	var requests strings.Builder
	for _, ref := range refs {
		requests.WriteString(ref.object + "\n")
	}
	cmd.Stdin = strings.NewReader(requests.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReader(stdout)
	readErr := func() error {
		for _, ref := range refs {
			header, err := reader.ReadString('\n')
			fields := strings.Fields(header)
			if err != nil || len(fields) != 3 || fields[0] != ref.object || fields[1] != "blob" {
				return fmt.Errorf("Git could not read %s at this revision", ref.name)
			}
			size, err := strconv.Atoi(fields[2])
			if err != nil || size < 0 || size > maxFileBytes {
				return fmt.Errorf("unsupported Git blob size for %s", ref.name)
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(reader, data); err != nil {
				return err
			}
			if end, err := reader.ReadByte(); err != nil || end != '\n' {
				return fmt.Errorf("truncated Git blob %s", ref.name)
			}
			if err := consume(ref, data); err != nil {
				return err
			}
		}
		return nil
	}()
	if readErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return readErr
	}
	return waitErr
}
