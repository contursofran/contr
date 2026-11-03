package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DiffTimeout is the time that one git command may run (D52).
const DiffTimeout = 5 * time.Second

// DiffStat counts the lines that a worktree changed against the merge base
// with the default branch, uncommitted changes included.
type DiffStat struct {
	Added   int
	Removed int
}

// Runner runs git in dir and gives its output. Tests use a fake.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// ExecRunner runs the `git` command.
type ExecRunner struct{}

// Run runs `git args...` in dir. It stops when ctx ends.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Diff counts the changed lines of the worktree at dir against the merge base
// of HEAD and base. It blocks: call it only from a goroutine. One timeout
// covers both git commands.
func Diff(ctx context.Context, r Runner, dir, base string) (DiffStat, error) {
	ctx, cancel := context.WithTimeout(ctx, DiffTimeout)
	defer cancel()
	out, err := r.Run(ctx, dir, "merge-base", "HEAD", base)
	if err != nil {
		return DiffStat{}, err
	}
	mb := strings.TrimSpace(string(out))
	if mb == "" {
		return DiffStat{}, errors.New("git: empty merge base")
	}
	out, err = r.Run(ctx, dir, "diff", "--numstat", mb)
	if err != nil {
		return DiffStat{}, err
	}
	return ParseNumstat(string(out))
}

// ParseNumstat sums the `added<TAB>removed<TAB>path` lines of
// `git diff --numstat`. A binary file (`-` for both counts) counts as zero.
func ParseNumstat(text string) (DiffStat, error) {
	var s DiffStat
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			return DiffStat{}, fmt.Errorf("git: bad numstat line %q", line)
		}
		if f[0] == "-" && f[1] == "-" {
			continue
		}
		a, errA := strconv.Atoi(f[0])
		d, errD := strconv.Atoi(f[1])
		if errA != nil || errD != nil {
			return DiffStat{}, fmt.Errorf("git: bad numstat line %q", line)
		}
		s.Added += a
		s.Removed += d
	}
	return s, nil
}

// Stats gives the diff stat of every worktree in scan, keyed by root. A
// worktree whose diff fails is left out and the error is logged, so its row
// shows no stats. It blocks: call it only from a goroutine.
func Stats(ctx context.Context, r Runner, scan Scan) map[string]DiffStat {
	out := map[string]DiffStat{}
	for _, repo := range scan.Repos {
		if repo.MainBranch == "" {
			continue
		}
		for _, wt := range repo.Worktrees {
			if ctx.Err() != nil {
				return out
			}
			s, err := Diff(ctx, r, wt.Root, repo.MainBranch)
			if err != nil {
				log.Printf("git diff stats for %s: %v", wt.Name, err)
				continue
			}
			out[wt.Root] = s
		}
	}
	return out
}
