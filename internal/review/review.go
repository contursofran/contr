// Package review reads a git repository for the Review view: the branches
// that are ahead of the base branch, the files and commits of each one, and
// the text of a file before and after a change.
//
// Every method runs the git command line and only reads. Nothing is cached,
// so call them from a goroutine, not from the UI thread.
package review

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is a git repository and the branch that the others are compared with.
type Repo struct {
	Dir  string // the repository folder
	Base string // for example "main"
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

func (r Repo) git(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir, "-c", "core.quotepath=false"}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Branches lists every local branch except the base, sorted by name. It does
// not filter by size: a branch with no commits ahead is in the list.
func (r Repo) Branches() ([]Branch, error) {
	out, err := r.git("for-each-ref", "--format=%(refname:short)", "--sort=refname", "refs/heads")
	if err != nil {
		return nil, err
	}
	var list []Branch
	for _, name := range lines(out) {
		if name == r.Base {
			continue
		}
		b := Branch{Name: name}
		n, err := r.git("rev-list", "--count", r.Base+".."+name)
		if err != nil {
			return nil, err
		}
		if b.Ahead, err = strconv.Atoi(strings.TrimSpace(n)); err != nil {
			return nil, fmt.Errorf("review: commit count of %s: %w", name, err)
		}
		files, err := r.Files(name)
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

// Files lists the files that branch changes against the merge base with the
// base, like `git diff base...branch`. A rename shows as a delete and an add.
func (r Repo) Files(branch string) ([]File, error) {
	return r.filesFrom([]string{"diff", "--no-renames"}, r.Base+"..."+branch)
}

// CommitFiles lists the files that one commit changes against its parent. A
// root commit compares with the empty tree.
func (r Repo) CommitFiles(sha string) ([]File, error) {
	return r.filesFrom([]string{"diff-tree", "--root", "-r", "--no-commit-id", "--no-renames"}, sha)
}

// Commits lists the commits in Base..branch, oldest first. Merge commits are
// left out: they change nothing that their parents did not.
func (r Repo) Commits(branch string) ([]Commit, error) {
	out, err := r.git("log", "--reverse", "--no-merges", "--format=%H%x09%h%x09%s", r.Base+".."+branch)
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
		files, err := r.CommitFiles(c.SHA)
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
func (r Repo) Texts(branch, sha, path string) (before, after string, err error) {
	var from, to string
	if sha == "" {
		mb, err := r.git("merge-base", r.Base, branch)
		if err != nil {
			return "", "", err
		}
		from, to = strings.TrimSpace(mb), branch
	} else {
		to = sha
		if r.hasParent(sha) {
			from = sha + "^"
		}
	}
	if from != "" {
		if before, err = r.textAt(from, path); err != nil {
			return "", "", err
		}
	}
	if after, err = r.textAt(to, path); err != nil {
		return "", "", err
	}
	return before, after, nil
}

// textAt returns path at rev, or "" when rev has no such path.
func (r Repo) textAt(rev, path string) (string, error) {
	listed, err := r.git("ls-tree", "--name-only", rev, "--", path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(listed) == "" {
		return "", nil
	}
	return r.git("show", rev+":"+path)
}

// hasParent reports whether sha has a parent, that is, is not a root commit.
func (r Repo) hasParent(sha string) bool {
	_, err := r.git("rev-parse", "--verify", "--quiet", sha+"^")
	return err == nil
}

// filesFrom lists the files of a diff command: cmd is the git command with
// its options, rev is the revision argument.
func (r Repo) filesFrom(cmd []string, rev string) ([]File, error) {
	status, err := r.git(append(append([]string{}, cmd...), "--name-status", rev)...)
	if err != nil {
		return nil, err
	}
	nums, err := r.git(append(append([]string{}, cmd...), "--numstat", rev)...)
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

// Summary is the size of all the work that the Review view lists: the sum of
// the branches ahead of the base.
type Summary struct {
	Added   int
	Removed int
	Files   int // distinct paths over all the branches
}

// Summary adds up the branches ahead of the base. A branch with no commits
// ahead is left out, like the Review view does. A folder that is not a
// repository gives an error.
func (r Repo) Summary() (Summary, error) {
	branches, err := r.Branches()
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	paths := map[string]bool{}
	for _, b := range branches {
		if b.Ahead == 0 {
			continue
		}
		s.Added += b.Added
		s.Removed += b.Removed
		files, err := r.Files(b.Name)
		if err != nil {
			return Summary{}, err
		}
		for _, f := range files {
			paths[f.Path] = true
		}
	}
	s.Files = len(paths)
	return s, nil
}

// RefsDir is the folder that holds the branch files of the repository. It is
// shared by all the worktrees. A change in it means a commit, a new branch
// or a deleted one.
func (r Repo) RefsDir() (string, error) {
	out, err := r.git("rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(out), "refs"), nil
}
