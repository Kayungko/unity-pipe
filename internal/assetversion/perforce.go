package assetversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

type P4Options struct {
	Root   string
	Script string
	Port   string
	User   string
	Client string
}

func ValidP4Root(root string) bool {
	return strings.HasPrefix(root, "//") && safeProjectPath(root[2:]) &&
		strings.Contains(root[2:], "/") && !strings.Contains(root, "...") && !strings.ContainsAny(root, "@#*")
}

func ValidChangelist(value string) bool {
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number != 0 && strconv.FormatUint(number, 10) == value
}

type p4Record struct {
	DepotFile  string  `json:"depotFile"`
	Dir        string  `json:"dir"`
	Rev        string  `json:"rev"`
	Change     string  `json:"change"`
	Severity   int     `json:"severity"`
	Generic    int     `json:"generic"`
	Data       *string `json:"data"`
	FileRev    string  `json:"rev0"`
	FileChange string  `json:"change0"`
	FileUser   string  `json:"user0"`
	FileTime   string  `json:"time0"`
	FileAction string  `json:"action0"`
}

func (p P4Options) raw(ctx context.Context, args ...string) (string, error) {
	// The P4 Suite bridge owns authentication, charset handling and argv
	// encoding. This reader issues only read operations through it.
	base := []string{p.Script, "raw", "--port", p.Port, "--user", p.User, "--client", p.Client}
	data, err := run(ctx, "", "node", append(base, args...)...)
	var reply struct {
		OK     bool   `json:"ok"`
		Stdout string `json:"stdout"`
	}
	if decode := json.Unmarshal(data, &reply); decode != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("could not read the P4 Suite response")
	}
	if err != nil || !reply.OK {
		return "", fmt.Errorf("P4 %s could not read the requested revision; check the selected port, user, client and permissions", args[2])
	}
	return reply.Stdout, nil
}

func (p P4Options) records(ctx context.Context, args ...string) ([]p4Record, error) {
	output, err := p.raw(ctx, append([]string{"-Mj", "-ztag"}, args...)...)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	result := make([]p4Record, 0)
	for {
		var record p4Record
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, fmt.Errorf("incomplete tagged P4 %s response", args[0])
		}
		if record.Severity != 0 {
			if record.Severity == 2 && record.Generic == 17 {
				continue // empty directory or no files at this revision
			}
			return nil, fmt.Errorf("P4 %s returned diagnostic %d/%d", args[0], record.Severity, record.Generic)
		}
		result = append(result, record)
	}
}

// OpenP4 selects the depot tree as of a submitted changelist, including
// unchanged files at their last revision at or before that changelist.
func OpenP4(ctx context.Context, options P4Options, change string) (*Version, error) {
	if !ValidP4Root(options.Root) || !ValidChangelist(change) {
		return nil, fmt.Errorf("invalid P4 depot root or changelist")
	}
	changes, err := options.records(ctx, "changes", "-s", "submitted", "-m", "1", "@"+change+",@"+change)
	if err != nil {
		return nil, err
	}
	if len(changes) != 1 || changes[0].Change != change {
		return nil, fmt.Errorf("P4 changelist %s is not visible as submitted", change)
	}

	paths := make(map[string]file)
	add := func(record p4Record) error {
		depot := record.DepotFile
		rel, ok := strings.CutPrefix(depot, options.Root+"/")
		if !ok || !ValidChangelist(record.Rev) {
			return fmt.Errorf("unexpected P4 file listing")
		}
		name, err := url.PathUnescape(rel)
		if err != nil || !safeProjectPath(name) || strings.ContainsRune(name, '�') {
			return fmt.Errorf("invalid or undecodable P4 asset path")
		}
		paths[name] = file{name, depot + "#" + record.Rev}
		return nil
	}
	for _, root := range []string{"Assets", "Packages"} {
		if err := options.listFiles(ctx, options.Root+"/"+root, "@"+change, true, add); err != nil {
			return nil, err
		}
	}
	return &Version{
		Identity: Identity{"p4", change},
		files:    paths,
		read:     options.read,
	}, nil
}

// PreviousP4 uses the previous visible submitted change across the server.
func PreviousP4(ctx context.Context, options P4Options, after *Version) (*Version, error) {
	change, err := strconv.ParseUint(after.Revision, 10, 64)
	if err != nil || change == 0 {
		return nil, fmt.Errorf("invalid submitted P4 changelist")
	}
	if change == 1 {
		return emptyVersion("p4", after.read), nil
	}
	records, err := options.records(ctx, "changes", "-s", "submitted", "-m", "1", "@1,@"+strconv.FormatUint(change-1, 10))
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return emptyVersion("p4", after.read), nil
	}
	if len(records) != 1 || !ValidChangelist(records[0].Change) {
		return nil, fmt.Errorf("unexpected P4 baseline changelist")
	}
	return OpenP4(ctx, options, records[0].Change)
}

// Bound each response to fit the bridge's subprocess buffer. Only descend
// into the versioned directories on larger trees.
func (p P4Options) listFiles(ctx context.Context, dir, at string, recursive bool, add func(p4Record) error) error {
	const limit = 512
	pattern := "*"
	if recursive {
		pattern = "..."
	}
	files, err := p.records(ctx, "files", "-e", "-m", strconv.Itoa(limit), dir+"/"+pattern+at)
	if err != nil {
		return err
	}
	if len(files) < limit {
		for _, item := range files {
			if err := add(item); err != nil {
				return err
			}
		}
		return nil
	}
	if !recursive {
		return fmt.Errorf("P4 directory %s reaches the per-directory listing limit of %d", dir, limit)
	}
	if err := p.listFiles(ctx, dir, at, false, add); err != nil {
		return err
	}
	directories, err := p.records(ctx, "dirs", dir+"/*"+at)
	if err != nil {
		return err
	}
	if len(directories) == 0 {
		return fmt.Errorf("incomplete P4 directory listing for %s", dir)
	}
	for _, child := range directories {
		if !strings.HasPrefix(child.Dir, dir+"/") || strings.Contains(strings.TrimPrefix(child.Dir, dir+"/"), "/") {
			return fmt.Errorf("unexpected P4 child directory")
		}
		if err := p.listFiles(ctx, child.Dir, at, true, add); err != nil {
			return err
		}
	}
	return nil
}

func (p P4Options) read(ctx context.Context, refs []file, consume func(file, []byte) error) error {
	for from := 0; from < len(refs); {
		// The bridge can safely route one trailing Unicode path via stdin.
		// Batch ASCII requests so index creation does not spawn per meta file.
		to := from + 1
		isMeta := strings.HasSuffix(refs[from].name, ".meta")
		if isMeta && ascii(refs[from].object) {
			for to < len(refs) && to-from < 24 && strings.HasSuffix(refs[to].name, ".meta") && ascii(refs[to].object) {
				to++
			}
		}
		var err error
		if isMeta {
			err = p.readMetadata(ctx, refs[from:to], consume)
		} else {
			err = p.readAsset(ctx, refs[from], consume)
		}
		if err != nil {
			return err
		}
		from = to
	}
	return nil
}

func ascii(value string) bool {
	return len(value) == utf8.RuneCountInString(value) && utf8.ValidString(value)
}

func (p P4Options) readMetadata(ctx context.Context, batch []file, consume func(file, []byte) error) error {
	args := []string{"print", "-k"}
	for _, ref := range batch {
		args = append(args, ref.object)
	}
	records, err := p.records(ctx, args...)
	if err != nil {
		// Large sprite importer metadata can exceed the tagged reply limit.
		// Retry the read via the P4 file-output mode.
		for _, ref := range batch {
			if err := p.readAsset(ctx, ref, consume); err != nil {
				return err
			}
		}
		return nil
	}
	current := ""
	found := make(map[string][]byte)
	for _, record := range records {
		if record.DepotFile != "" {
			current = record.DepotFile + "#" + record.Rev
		} else if record.Data != nil && current != "" {
			if *record.Data == "" {
				if found[current] == nil {
					return fmt.Errorf("empty P4 metadata file")
				}
				current = ""
			} else {
				found[current] = append(found[current], *record.Data...)
			}
		}
	}
	if current != "" || len(found) != len(batch) {
		return fmt.Errorf("P4 did not return all requested metadata revisions")
	}
	for _, ref := range batch {
		data, exists := found[ref.object]
		if !exists {
			return fmt.Errorf("P4 metadata missing for %s", ref.name)
		}
		if err := consume(ref, data); err != nil {
			return err
		}
	}
	return nil
}

func (p P4Options) readAsset(ctx context.Context, ref file, consume func(file, []byte) error) error {
	output, err := os.CreateTemp("", "upipe-asset-*")
	if err != nil {
		return err
	}
	name := output.Name()
	defer os.Remove(name)
	if err := output.Close(); err != nil {
		return err
	}
	if _, err := p.raw(ctx, "-Mj", "-ztag", "print", "-q", "-k", "-o", name, ref.object); err != nil {
		return err
	}
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	if info.Size() > maxFileBytes {
		return fmt.Errorf("unsupported P4 file size for %s", ref.name)
	}
	body, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	return consume(ref, body)
}
