package agents

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// gitEnv makes git in the test ignore the user's git config.
func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.test")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// newRepo makes a repository with one commit in a new folder named main.
func newRepo(t *testing.T) string {
	t.Helper()
	gitEnv(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, main, "init", "--quiet", "-b", "main")
	mustGit(t, main, "commit", "--quiet", "--allow-empty", "-m", "one")
	return main
}

func runFake() *fakeCore {
	return &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(p map[string]any) (any, error) { return term("t1", "w", "live", nil, nil), nil },
	}}
}

func TestSpawnMakesTheWorktreeAbsoluteAndChecksIt(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	f := runFake()
	m := New(f, DefaultConfig())
	if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: "builder", Worktree: "wt"}); err != nil {
		t.Fatal(err)
	}
	p := f.last().Params
	if want := filepath.Join(dir, "wt"); p["cwd"] != want || p["labels"].(map[string]any)[LabelWorktree] != want {
		t.Errorf("cwd %v labels %v, want %s", p["cwd"], p["labels"], want)
	}

	f.calls = nil
	if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: "builder", Worktree: "missing"}); !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), filepath.Join(dir, "missing")) {
		t.Errorf("missing folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: "builder", Worktree: "file"}); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("plain file: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("hc got calls for a bad worktree: %v", f.calls)
	}
}

func TestSpawnRunsTheProjectSetupInANewWorktree(t *testing.T) {
	main := newRepo(t)
	wt := main + "-wt/b-1"
	mustGit(t, main, "worktree", "add", "--quiet", "-b", "b-1", wt)
	other := newRepo(t)

	cfg := DefaultConfig()
	cfg.Setup = map[string]string{main: "cp -R \"$CONTR_MAIN/.husky\" ."}
	f := runFake()
	m := New(f, cfg)
	if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: "builder", Worktree: wt, Parent: "t0", Task: "go"}); err != nil {
		t.Fatal(err)
	}
	p := f.last().Params
	argv := p["argv"].([]any)
	want := []any{"/bin/sh", "-c", setupWrapper, setupName, cfg.Setup[main], "claude"}
	if !reflect.DeepEqual(argv[:6], want) || argv[len(argv)-1] != "go" {
		t.Errorf("argv = %q", argv)
	}
	if env := p["env"].(map[string]any); env[EnvMain] != main || env[EnvCoordinator] != "t0" {
		t.Errorf("env = %v", env)
	}

	for _, dir := range []string{main, other} {
		if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: "builder", Worktree: dir}); err != nil {
			t.Fatal(err)
		}
		if p := f.last().Params; p["argv"].([]any)[0] != "claude" || p["env"] != nil {
			t.Errorf("%s: argv %v env %v, want no setup", dir, p["argv"], p["env"])
		}
	}
}

// The wrapper runs the setup in the worktree, then the agent; a failed
// setup ends with its exit code and does not start the agent.
func TestSetupWrapperRunsSetupThenTheAgent(t *testing.T) {
	dir := t.TempDir()
	run := func(setup string) (string, int) {
		argv := withSetup(setup, []string{"/bin/sh", "-c", `echo "agent $1"`, "sh", "a b"})
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(out), exit.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	if out, code := run("touch ready; echo set"); code != 0 || out != "set\nagent a b\n" {
		t.Errorf("ok setup: %d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ready")); err != nil {
		t.Errorf("setup did not run in the folder: %v", err)
	}
	out, code := run("echo broken; exit 3")
	if code != 3 || !strings.Contains(out, "broken\ncontr: worktree setup failed (exit 3)") || strings.Contains(out, "agent") {
		t.Errorf("failed setup: %d %q", code, out)
	}
}

func TestCommandSkipsTheSetupWrapper(t *testing.T) {
	tm := terminal{Argv: withSetup("x", []string{"/usr/bin/claude", "--agent", "a"})}
	if got := tm.command(); got != "claude" {
		t.Errorf("command = %q", got)
	}
}

func TestLoadConfigReadsSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"setup":{"~/code/app":"pnpm install --offline"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Setup["~/code/app"] != "pnpm install --offline" || len(cfg.Profiles) == 0 {
		t.Errorf("cfg = %+v", cfg)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := expandHome("~/code/app"); got != filepath.Join(home, "code/app") {
		t.Errorf("expandHome = %q", got)
	}
}

func TestReviewWorktree(t *testing.T) {
	origin := newRepo(t)
	head := func(msg string) string {
		mustGit(t, origin, "commit", "--quiet", "--allow-empty", "-m", msg)
		mustGit(t, origin, "update-ref", "refs/pull/7/head", "HEAD")
		return mustGit(t, origin, "rev-parse", "HEAD")
	}
	sha := head("pr")
	main := filepath.Join(filepath.Dir(origin), "clone")
	mustGit(t, filepath.Dir(origin), "clone", "--quiet", origin, main)

	m := New(&fakeCore{}, DefaultConfig())
	ctx := context.Background()
	dir, err := m.ReviewWorktree(ctx, main, 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := main + "-wt/review-7"; dir != want {
		t.Errorf("dir %s, want %s", dir, want)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != sha {
		t.Errorf("head %s, want %s", got, sha)
	}

	// A second review of the same PR moves the worktree to the new head.
	sha = head("fix")
	if dir, err = m.ReviewWorktree(ctx, dir, 7); err != nil {
		t.Fatal(err)
	}
	if got := mustGit(t, dir, "rev-parse", "HEAD"); got != sha {
		t.Errorf("head after a fix %s, want %s", got, sha)
	}

	if _, err := m.ReviewWorktree(ctx, main, 8); err == nil || !strings.Contains(err.Error(), "no pull request 8") {
		t.Errorf("missing PR: %v", err)
	}
	if _, err := m.ReviewWorktree(ctx, t.TempDir(), 7); err == nil {
		t.Error("not a repository: no error")
	}
	if _, err := m.ReviewWorktree(ctx, main, 0); err == nil {
		t.Error("PR 0: no error")
	}
}
