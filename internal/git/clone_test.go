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

func TestParseRemote(t *testing.T) {
	ok := []struct{ in, url, name string }{
		{"owner/repo", "https://github.com/owner/repo.git", "repo"},
		{" owner/re.po ", "https://github.com/owner/re.po.git", "re.po"},
		{"https://github.com/o/r.git", "https://github.com/o/r.git", "r"},
		{"https://example.com/o/r", "https://example.com/o/r", "r"},
		{"git@github.com:o/r.git", "git@github.com:o/r.git", "r"},
	}
	for _, c := range ok {
		url, name, err := ParseRemote(c.in)
		if err != nil || url != c.url || name != c.name {
			t.Errorf("ParseRemote(%q) = %q, %q, %v", c.in, url, name, err)
		}
	}
	for _, in := range []string{"", "  ", "a b/c", "owner/repo extra", "../x", "o/..", "../x/y", "http://x.com/o/r", "justname", "o/r/x", "https://x.com/", "-o/r", "git@x.com:"} {
		if _, _, err := ParseRemote(in); err == nil {
			t.Errorf("ParseRemote(%q) = nil error", in)
		}
	}
}

func TestCloneSuccess(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "r")
	var got []string
	run := func(_ context.Context, git string, args ...string) (string, error) {
		got = append([]string{git}, args...)
		return "", nil
	}
	if err := Clone(context.Background(), run, "https://x/o/r.git", dest); err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[1] != "clone" || got[2] != "--" || got[3] != "https://x/o/r.git" || got[4] != dest {
		t.Fatalf("args = %v", got)
	}
}

func TestCloneErrorReturnsStderr(t *testing.T) {
	run := func(context.Context, string, ...string) (string, error) {
		return "Cloning into 'r'...\nfatal: repository not found\n\n", errors.New("exit status 128")
	}
	err := Clone(context.Background(), run, "https://x/o/r.git", filepath.Join(t.TempDir(), "r"))
	if err == nil || err.Error() != "fatal: repository not found" {
		t.Fatalf("err = %v", err)
	}
	run = func(context.Context, string, ...string) (string, error) { return "", errors.New("boom") }
	err = Clone(context.Background(), run, "u", filepath.Join(t.TempDir(), "r"))
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}
}

func TestCloneDestExistsRefused(t *testing.T) {
	called := false
	run := func(context.Context, string, ...string) (string, error) { called = true; return "", nil }
	err := Clone(context.Background(), run, "u", t.TempDir())
	if !errors.Is(err, ErrDestExists) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestFindGitFallsBackToDirs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	p := filepath.Join(dir, "git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindGit([]string{t.TempDir(), dir})
	if err != nil || got != p {
		t.Fatalf("FindGit = %q, %v", got, err)
	}
	if _, err := FindGit([]string{t.TempDir()}); !errors.Is(err, ErrGitNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// TestCloneLocalBare clones a bare repository in a temporary folder with the
// real git program. No network is used.
func TestCloneLocalBare(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "src.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	dest := filepath.Join(root, "out")
	if err := Clone(context.Background(), nil, bare, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatal(err)
	}
	err := Clone(context.Background(), nil, filepath.Join(root, "missing.git"), filepath.Join(root, "out2"))
	if err == nil || err.Error() == "" {
		t.Fatalf("err = %v", err)
	}
}

// fakeGit writes a shell script named git in a temporary folder.
func fakeGit(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecCloneNoPrompt(t *testing.T) {
	g := fakeGit(t, `echo "prompt=$GIT_TERMINAL_PROMPT" >&2; exit 1`)
	stderr, err := ExecClone(context.Background(), g, "clone")
	if err == nil || !strings.Contains(stderr, "prompt=0") {
		t.Fatalf("stderr = %q, err = %v", stderr, err)
	}
}

// TestExecCloneCancel starts a git that leaves a child holding the stderr
// pipe. After cancel, ExecClone must return soon after WaitDelay.
func TestExecCloneCancel(t *testing.T) {
	g := fakeGit(t, `sleep 30 & sleep 30`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ExecClone(ctx, g, "clone")
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("err = nil after cancel")
		}
	case <-time.After(cloneWaitDelay + 3*time.Second):
		t.Fatal("ExecClone hangs after cancel")
	}
}

// TestExecCloneLingeringChild starts a git that exits well but leaves a child
// holding the stderr pipe. ExecClone must return after WaitDelay, with no error.
func TestExecCloneLingeringChild(t *testing.T) {
	g := fakeGit(t, `sleep 30 & exit 0`)
	start := time.Now()
	_, err := ExecClone(context.Background(), g, "clone")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > cloneWaitDelay+3*time.Second {
		t.Fatalf("took %v", d)
	}
}
