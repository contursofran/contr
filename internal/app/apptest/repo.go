package apptest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a scratch git repository in a temporary folder. Dir is its folder.
type Repo struct {
	t   *testing.T
	Dir string
}

// NewRepo makes a repository with branch main in a temporary folder.
func NewRepo(t *testing.T) *Repo {
	t.Helper()
	s := &Repo{t: t, Dir: t.TempDir()}
	s.Git("init", "-b", "main")
	s.Commit("base", map[string]string{"base.txt": "base\n"})
	return s
}

// Git runs git in the repository and gives its output. It fails the test on
// an error.
func (s *Repo) Git(args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", s.Dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Commit writes files and commits all changes with message msg.
func (s *Repo) Commit(msg string, files map[string]string) {
	s.t.Helper()
	for p, text := range files {
		if err := os.WriteFile(filepath.Join(s.Dir, p), []byte(text), 0o644); err != nil {
			s.t.Fatal(err)
		}
	}
	s.Git("add", "-A")
	s.Git("commit", "-m", msg)
}

// BranchWithTwoCommits makes branch b: commit one adds a.txt, commit two adds
// b.txt.
func (s *Repo) BranchWithTwoCommits() {
	s.Git("checkout", "-b", "b")
	s.Commit("one", map[string]string{"a.txt": "a1\n"})
	s.Commit("two", map[string]string{"b.txt": "b1\nb2\n"})
}
