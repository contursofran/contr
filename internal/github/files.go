package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// File is one file of a pull request with its part of the diff.
type File struct {
	Path, OldPath        string // OldPath is empty unless the file was renamed
	Additions, Deletions int
	// Patch is the unified diff of the file, with its "diff --git" header.
	// It is empty for a binary file. The package does not parse it: the
	// view reads it with editor.ParsePatch.
	Patch string
}

// Files returns the changed files of pull request n in the order of gh, each
// with its counts and its part of the diff.
func (c Client) Files(ctx context.Context, n int) ([]File, error) {
	num := strconv.Itoa(n)
	out, err := c.run(ctx, "pr", "view", num, "--json", "files")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Files []struct {
			Path      string `json:"path"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("gh: read pr %d files: %w", n, err)
	}
	diff, err := c.run(ctx, "pr", "diff", num)
	if err != nil {
		return nil, err
	}
	blocks := splitDiff(string(diff))
	files := make([]File, 0, len(raw.Files))
	for _, r := range raw.Files {
		f := File{Path: r.Path, Additions: r.Additions, Deletions: r.Deletions}
		for i, b := range blocks {
			if b.used || !strings.HasSuffix(b.header, " b/"+r.Path) {
				continue
			}
			blocks[i].used = true
			f.OldPath = b.oldPath
			if strings.Contains(b.text, "\n@@") {
				f.Patch = b.text
			}
			break
		}
		files = append(files, f)
	}
	return files, nil
}

type diffBlock struct {
	header  string // the text after "diff --git "
	oldPath string // from "rename from", else empty
	text    string
	used    bool
}

// splitDiff cuts a git diff at each "diff --git" header line.
func splitDiff(diff string) []diffBlock {
	var blocks []diffBlock
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		b := diffBlock{header: strings.TrimPrefix(cur[0], "diff --git "), text: strings.Join(cur, "\n") + "\n"}
		for _, l := range cur[1:] {
			if strings.HasPrefix(l, "@@") {
				break
			}
			if p, ok := strings.CutPrefix(l, "rename from "); ok {
				b.oldPath = p
			}
		}
		blocks = append(blocks, b)
		cur = nil
	}
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		if strings.HasPrefix(l, "diff --git ") {
			flush()
		}
		if len(cur) > 0 || strings.HasPrefix(l, "diff --git ") {
			cur = append(cur, l)
		}
	}
	flush()
	return blocks
}
