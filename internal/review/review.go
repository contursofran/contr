// Package review reads a git repository for the Review view: the branches
// that are ahead of the base branch, the files and commits of each one, and
// the text of a file before and after a change.
//
// Every method runs git through a runner and only reads. Nothing is cached,
// so call them from a goroutine, not from the UI thread. Each method stops
// its git when its context ends.
package review

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/contursofran/contr/internal/git"
)

// Repo is a git repository and the branch that the others are compared with.
type Repo struct {
	Dir  string // the repository folder
	Base string // for example "main"
	// Git runs git. Nil runs the `git` command (git.ExecRunner).
	Git git.InputRunner
}

// Status is the change of a file.
type Status string

// The file statuses.
const (
	Added    Status = "A"
	Modified Status = "M"
	Deleted  Status = "D"
)

// Branch is a branch with its size against the base.
type Branch struct {
	Name    string
	Ahead   int // commits in Base..Name
	Added   int
	Removed int
}

// File is a file that a branch or a commit changes.
type File struct {
	Path    string
	Status  Status
	Added   int
	Removed int
	Binary  bool
}

// Commit is one commit of a branch, with its size against its parent.
type Commit struct {
	SHA     string // full
	Short   string
	Subject string
	Added   int
	Removed int
}

func (r Repo) runner() git.InputRunner {
	if r.Git == nil {
		return git.ExecRunner{}
	}
	return r.Git
}

func (r Repo) git(ctx context.Context, args ...string) (string, error) {
	return r.gitInput(ctx, nil, args...)
}

// gitInput runs git in Dir with stdin as its standard input. Paths in the
// output are not quoted. The error names args without the global options.
func (r Repo) gitInput(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	full := append([]string{"-C", r.Dir, "-c", "core.quotepath=false"}, args...)
	out, err := r.runner().RunInput(ctx, "", stdin, full...)
	if err != nil {
		var ge *git.Error
		if errors.As(err, &ge) {
			return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), ge.Err, ge.Stderr)
		}
		return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// Branches lists every local branch except the base, sorted by name. It does
// not filter by size: a branch with no commits ahead is in the list.
func (r Repo) Branches(ctx context.Context) ([]Branch, error) {
	out, err := r.git(ctx, "for-each-ref", "--format=%(refname:short)", "--sort=refname", "refs/heads")
	if err != nil {
		return nil, err
	}
	var list []Branch
	for _, name := range lines(out) {
		if name == r.Base {
			continue
		}
		b := Branch{Name: name}
		n, err := r.git(ctx, "rev-list", "--count", r.Base+".."+name)
		if err != nil {
			return nil, err
		}
		if b.Ahead, err = strconv.Atoi(strings.TrimSpace(n)); err != nil {
			return nil, fmt.Errorf("review: commit count of %s: %w", name, err)
		}
		files, err := r.Files(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			b.Added += f.Added
			b.Removed += f.Removed
		}
		list = append(list, b)
	}
	return list, nil
}

// Current is the branch that Dir has checked out, "" on a detached HEAD or
// when git fails.
func (r Repo) Current(ctx context.Context) string {
	out, err := r.git(ctx, "branch", "--show-current")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Files lists the files that branch changes against the merge base with the
// base, like `git diff base...branch`. A rename shows as a delete and an add.
func (r Repo) Files(ctx context.Context, branch string) ([]File, error) {
	return r.filesFrom(ctx, []string{"diff", "--no-renames"}, r.Base+"..."+branch)
}

// CommitFiles lists the files that one commit changes against its parent. A
// root commit compares with the empty tree.
func (r Repo) CommitFiles(ctx context.Context, sha string) ([]File, error) {
	return r.filesFrom(ctx, []string{"diff-tree", "--root", "-r", "--no-commit-id", "--no-renames"}, sha)
}

// Commits lists the commits in Base..branch, oldest first. Merge commits are
// left out: they change nothing that their parents did not.
func (r Repo) Commits(ctx context.Context, branch string) ([]Commit, error) {
	out, err := r.git(ctx, "log", "--reverse", "--no-merges", "--format=%H%x09%h%x09%s", r.Base+".."+branch)
	if err != nil {
		return nil, err
	}
	var list []Commit
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		c := Commit{SHA: f[0], Short: f[1], Subject: f[2]}
		files, err := r.CommitFiles(ctx, c.SHA)
		if err != nil {
			return nil, err
		}
		for _, x := range files {
			c.Added += x.Added
			c.Removed += x.Removed
		}
		list = append(list, c)
	}
	return list, nil
}

// Texts returns the text of path before and after a change. For the whole
// branch (sha is empty) it is the file at the merge base of the base and the
// branch, and at the branch tip. For a commit it is the file at the parent
// (the empty tree for a root commit) and at the commit. A side where the
// path does not exist gives "".
func (r Repo) Texts(ctx context.Context, branch, sha, path string) (before, after string, err error) {
	var from, to string
	if sha == "" {
		mb, err := r.git(ctx, "merge-base", r.Base, branch)
		if err != nil {
			return "", "", err
		}
		from, to = strings.TrimSpace(mb), branch
	} else {
		to = sha
		if r.hasParent(ctx, sha) {
			from = sha + "^"
		}
	}
	if from != "" {
		if before, err = r.textAt(ctx, from, path); err != nil {
			return "", "", err
		}
	}
	if after, err = r.textAt(ctx, to, path); err != nil {
		return "", "", err
	}
	return before, after, nil
}

// textAt returns path at rev, or "" when rev has no such path.
func (r Repo) textAt(ctx context.Context, rev, path string) (string, error) {
	listed, err := r.git(ctx, "ls-tree", "--name-only", rev, "--", path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(listed) == "" {
		return "", nil
	}
	return r.git(ctx, "show", rev+":"+path)
}

// hasParent reports whether sha has a parent, that is, is not a root commit.
func (r Repo) hasParent(ctx context.Context, sha string) bool {
	_, err := r.git(ctx, "rev-parse", "--verify", "--quiet", sha+"^")
	return err == nil
}

// filesFrom lists the files of a diff command: cmd is the git command with
// its options, rev is the revision argument.
func (r Repo) filesFrom(ctx context.Context, cmd []string, rev string) ([]File, error) {
	status, err := r.git(ctx, append(append([]string{}, cmd...), "--name-status", rev)...)
	if err != nil {
		return nil, err
	}
	nums, err := r.git(ctx, append(append([]string{}, cmd...), "--numstat", rev)...)
	if err != nil {
		return nil, err
	}
	type count struct {
		added, removed int
		binary         bool
	}
	counts := map[string]count{}
	for _, l := range lines(nums) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		var c count
		if f[0] == "-" {
			c.binary = true
		} else {
			c.added, _ = strconv.Atoi(f[0])
			c.removed, _ = strconv.Atoi(f[1])
		}
		counts[f[2]] = c
	}
	var files []File
	for _, l := range lines(status) {
		f := strings.SplitN(l, "\t", 2)
		if len(f) != 2 || f[0] == "" {
			continue
		}
		c := counts[f[1]]
		files = append(files, File{Path: f[1], Status: Status(f[0][:1]), Added: c.added, Removed: c.removed, Binary: c.binary})
	}
	return files, nil
}

// lines splits text into lines without the final newline.
func lines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Summary is the size of a list of changed files: the lines added and
// removed, and the number of files.
type Summary struct {
	Added   int
	Removed int
	Files   int
}

// Total adds up the files that [Repo.Changes] lists. The file tree and the
// title bar's diff summary both use it, so their totals agree.
func Total(files []File) Summary {
	s := Summary{Files: len(files)}
	for _, f := range files {
		s.Added += f.Added
		s.Removed += f.Removed
	}
	return s
}
