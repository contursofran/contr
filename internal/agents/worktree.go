package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	gitpkg "github.com/contursofran/contr/internal/git"
)

// EnvMain holds the main checkout of a worker's worktree while the setup
// command runs, so the command can copy files from it.
const EnvMain = "CONTR_MAIN"

// setupName is $0 of the setup wrapper. command() finds the agent after it.
const setupName = "contr-setup"

// setupWrapper runs the setup command ($1) in the worktree, then starts the
// agent (the words after it). A failed setup ends the terminal with the
// setup's exit code, so the parent gets a note and the output stays readable.
const setupWrapper = `/bin/sh -c "$1" || { s=$?; echo "contr: worktree setup failed (exit $s)" >&2; exit $s; }; shift; exec "$@"`

// git runs git in a folder and returns its trimmed standard output.
type git func(ctx context.Context, dir string, args ...string) (string, error)

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	// -C, not the runner's folder, so a missing dir gives git's own message.
	out, err := gitpkg.ExecRunner{}.Run(ctx, "", append([]string{"-C", dir}, args...)...)
	if err != nil {
		var ge *gitpkg.Error
		if !errors.As(err, &ge) {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		if ge.Stderr == "" {
			return "", fmt.Errorf("git %s: %w", args[0], ge.Err)
		}
		return "", fmt.Errorf("git %s: %s", args[0], ge.Stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeDir returns dir as an absolute path to a folder. hc resolves a
// relative cwd from its own folder, not from the caller's.
func worktreeDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("agents: worktree %s: %w", dir, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("agents: worktree %s: %w", abs, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("agents: worktree %s is not a folder", abs)
	}
	return abs, nil
}

// mainCheckout returns the main checkout of the git worktree dir.
func (m *Manager) mainCheckout(ctx context.Context, dir string) (string, error) {
	common, err := m.git(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.Base(common) != ".git" {
		return "", fmt.Errorf("agents: %s has no main checkout (git folder %s)", dir, common)
	}
	return filepath.Dir(common), nil
}

// setupFor returns the setup command of the project that dir is a worktree
// of, and its main checkout. The main checkout itself gets no setup: the
// owner prepares it. A folder outside git gets none either.
func (m *Manager) setupFor(ctx context.Context, dir string) (cmd, main string) {
	if len(m.cfg.Setup) == 0 || dir == "" {
		return "", ""
	}
	main, err := m.mainCheckout(ctx, dir)
	if err != nil || samePath(main, dir) {
		return "", ""
	}
	for k, v := range m.cfg.Setup {
		if samePath(expandHome(k), main) {
			return v, main
		}
	}
	return "", ""
}

// withSetup returns argv started after the setup command.
func withSetup(setup string, argv []string) []string {
	return append([]string{"/bin/sh", "-c", setupWrapper, setupName, setup}, argv...)
}

// ReviewWorktree makes a detached worktree at the head of pull request pr of
// the repository at repo (any of its checkouts), and returns its path:
// <main checkout>-wt/review-<pr>. An existing review worktree moves to the
// new head. The head comes from the origin remote's refs/pull/<pr>/head.
func (m *Manager) ReviewWorktree(ctx context.Context, repo string, pr int) (string, error) {
	if pr <= 0 {
		return "", errors.New("agents: a pull request number is positive")
	}
	main, err := m.mainCheckout(ctx, repo)
	if err != nil {
		return "", err
	}
	ref := "refs/pull/" + strconv.Itoa(pr) + "/head"
	line, err := m.git(ctx, main, "ls-remote", "origin", ref)
	if err != nil {
		return "", err
	}
	sha, _, _ := strings.Cut(line, "\t")
	if sha == "" {
		return "", fmt.Errorf("agents: origin has no pull request %d", pr)
	}
	if _, err := m.git(ctx, main, "fetch", "--quiet", "origin", ref); err != nil {
		return "", err
	}
	dir := filepath.Join(main+"-wt", "review-"+strconv.Itoa(pr))
	if _, err := os.Stat(dir); err == nil {
		_, err = m.git(ctx, dir, "checkout", "--quiet", "--detach", sha)
		return dir, err
	}
	_, err = m.git(ctx, main, "worktree", "add", "--quiet", "--detach", dir, sha)
	return dir, err
}

// expandHome replaces a leading ~/ with the home folder.
func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// samePath tells if a and b name the same folder, after symbolic links.
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
