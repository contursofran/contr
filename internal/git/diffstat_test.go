package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	out   map[string]string
	err   error
	calls []string
}

func (f *fakeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, dir+": "+strings.Join(args, " "))
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.out[args[0]]), nil
}

func TestParseNumstatSums(t *testing.T) {
	got, err := ParseNumstat("3\t1\ta.go\n10\t0\tb/c.go\n\n")
	if err != nil || got != (DiffStat{Added: 13, Removed: 1}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestParseNumstatBinaryCountsZero(t *testing.T) {
	got, err := ParseNumstat("-\t-\timg.png\n2\t2\ta.go\n")
	if err != nil || got != (DiffStat{Added: 2, Removed: 2}) {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestParseNumstatBadLine(t *testing.T) {
	for _, in := range []string{"junk\n", "x\ty\tf\n"} {
		if _, err := ParseNumstat(in); err == nil {
			t.Errorf("%q: want an error", in)
		}
	}
}

func TestDiffUsesTheMergeBase(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"merge-base": "abc123\n", "diff": "4\t2\tf\n"}}
	got, err := Diff(context.Background(), r, "/w", "main")
	if err != nil || got != (DiffStat{4, 2}) {
		t.Fatalf("got %+v, %v", got, err)
	}
	want := []string{"/w: merge-base HEAD main", "/w: diff --numstat abc123"}
	if strings.Join(r.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls %q, want %q", r.calls, want)
	}
}

func TestDiffError(t *testing.T) {
	boom := errors.New("boom")
	if _, err := Diff(context.Background(), &fakeRunner{err: boom}, "/w", "main"); !errors.Is(err, boom) {
		t.Errorf("err %v, want boom", err)
	}
}

func TestDiffEmptyMergeBase(t *testing.T) {
	if _, err := Diff(context.Background(), &fakeRunner{}, "/w", "main"); err == nil {
		t.Error("want an error")
	}
}

type blockRunner struct{}

func (blockRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDiffTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Diff(ctx, blockRunner{}, "/w", "main")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err %v, want a deadline error", err)
	}
	if time.Since(start) > time.Second {
		t.Error("the timeout did not stop the run")
	}
}

func TestStatsLeavesOutFailedWorktrees(t *testing.T) {
	scan := Scan{Repos: map[string]RepoInfo{"c": {MainBranch: "main",
		Worktrees: []WorktreeEntry{{Root: "/r", Name: "r"}}}}}
	if got := Stats(context.Background(), &fakeRunner{err: errors.New("x")}, scan); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
	r := &fakeRunner{out: map[string]string{"merge-base": "a", "diff": "1\t1\tf\n"}}
	if got := Stats(context.Background(), r, scan); got["/r"] != (DiffStat{1, 1}) {
		t.Errorf("got %v", got)
	}
}

func TestStatsDiffsAgainstTheDefaultBranch(t *testing.T) {
	scan := Scan{Repos: map[string]RepoInfo{"c": {MainBranch: "feature", DefaultBranch: "origin/main",
		Worktrees: []WorktreeEntry{{Root: "/r", Name: "r"}}}}}
	r := &fakeRunner{out: map[string]string{"merge-base": "a", "diff": ""}}
	Stats(context.Background(), r, scan)
	if len(r.calls) == 0 || !strings.Contains(r.calls[0], "origin/main") {
		t.Errorf("calls %v, want the default branch as base", r.calls)
	}
}

func TestExecRunnerOnAScratchRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is missing")
	}
	dir := tmp(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "1\n2\n3\n")
	run("add", ".")
	run("commit", "-q", "-m", "one")
	run("switch", "-q", "-c", "work")
	write(t, filepath.Join(dir, "b.txt"), "x\ny\n")
	run("add", ".")
	run("commit", "-q", "-m", "two")
	write(t, filepath.Join(dir, "a.txt"), "1\n3\n4\n5\n") // uncommitted: +2 −1
	got, err := Diff(context.Background(), ExecRunner{}, dir, "main")
	if err != nil || got != (DiffStat{Added: 4, Removed: 1}) {
		t.Errorf("got %+v, %v; want +4 −1", got, err)
	}
	if _, err := Diff(context.Background(), ExecRunner{}, dir, "nope"); err == nil {
		t.Error("an unknown base should fail")
	}
}
