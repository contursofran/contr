// Package git finds git checkouts for the sidebar: which checkout holds a
// directory, and which worktrees a repository has. It reads plain files only
// (`.git` entries, `commondir`, `gitdir`, `worktrees/<name>/`, `HEAD`). It never
// runs `git`.
package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ErrNotGitDir means a path is not a `.git` directory of a non-bare repository.
var ErrNotGitDir = errors.New("git: not a `.git` directory")

// ErrNoName means the main root of a repository has no directory name.
var ErrNoName = errors.New("git: main root has no name")

// Checkout is one checkout (the main one or a linked worktree) that holds a
// directory. All paths are canonical.
type Checkout struct {
	// Root is the working tree root.
	Root string
	// GitDir is `<main>/.git`, or `<main>/.git/worktrees/<name>` for a linked
	// worktree.
	GitDir string
	// CommonDir is the shared git dir of the repository: `<main>/.git`.
	CommonDir string
	// IsMain is true for the main checkout, whose `.git` is a directory.
	IsMain bool
}

// WorktreeEntry is one worktree of a repository. Name is the root's directory
// name.
type WorktreeEntry struct {
	Root string
	Name string
	// Branch is the branch name; a detached HEAD gives its first 7 hex digits.
	// It is empty when HEAD names neither.
	Branch string
	IsMain bool
}

// RepoInfo is a repository: its main checkout and its worktrees.
type RepoInfo struct {
	CommonDir string
	// Name is the directory name of the main root.
	Name       string
	MainRoot   string
	MainBranch string
	// Worktrees lists the main one first, then linked worktrees sorted by root
	// path.
	Worktrees []WorktreeEntry
}

// Scan holds checkouts and repositories for a set of pane working directories.
type Scan struct {
	// Checkouts is keyed by the cwd string as herdr sent it.
	Checkouts map[string]Checkout
	// Repos is keyed by CommonDir. Every checkout's CommonDir is a key here.
	Repos map[string]RepoInfo
}

// Discover returns the checkout that holds path: the nearest ancestor (or path
// itself) with a `.git` entry. It reports false when path does not exist, lies
// outside git, or its nearest `.git` entry is broken (the search does not go on
// past it). A linked worktree whose common dir is not `<main>/.git` (a
// submodule, a separate git dir, or a worktree of a bare repository) also gives
// false, so such panes are not in Spaces.
func Discover(path string) (Checkout, bool) {
	start, err := canonical(path)
	if err != nil {
		return Checkout{}, false
	}
	for root := start; ; {
		dotGit := filepath.Join(root, ".git")
		if info, err := os.Stat(dotGit); err == nil {
			if info.IsDir() {
				gitDir, err := canonical(dotGit)
				if err != nil {
					return Checkout{}, false
				}
				return Checkout{Root: root, GitDir: gitDir, CommonDir: gitDir, IsMain: true}, true
			}
			if info.Mode().IsRegular() {
				return linkedCheckout(root, dotGit)
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			return Checkout{}, false
		}
		root = parent
	}
}

// HeadBranch returns the branch of the checkout whose git dir is gitDir, read
// from `HEAD`: `ref: refs/heads/<name>` gives `<name>`, a detached commit gives
// its first 7 hex digits. It reports false when HEAD is unreadable or names
// neither.
func HeadBranch(gitDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(data))
	if reference, ok := strings.CutPrefix(head, "ref:"); ok {
		branch, ok := strings.CutPrefix(strings.TrimSpace(reference), "refs/heads/")
		if !ok || branch == "" {
			return "", false
		}
		return branch, true
	}
	if len(head) < 7 {
		return "", false
	}
	for i := 0; i < len(head); i++ {
		if !isHex(head[i]) {
			return "", false
		}
	}
	return head[:7], true
}

// ReadRepo returns the main checkout and the linked worktrees of the
// repository whose shared git dir is commonDir. Worktrees whose `.git` file is
// gone are left out. It fails when the repository cannot be read, including a
// bare repository.
func ReadRepo(commonDir string) (RepoInfo, error) {
	commonDir, err := canonical(commonDir)
	if err != nil {
		return RepoInfo{}, err
	}
	mainRoot, ok := mainRootOf(commonDir)
	if !ok {
		return RepoInfo{}, ErrNotGitDir
	}
	name := dirName(mainRoot)
	if name == "" {
		return RepoInfo{}, ErrNoName
	}
	mainBranch, _ := HeadBranch(commonDir)
	linked, err := linkedWorktrees(commonDir)
	if err != nil {
		return RepoInfo{}, err
	}
	slices.SortFunc(linked, func(a, b WorktreeEntry) int { return comparePaths(a.Root, b.Root) })
	worktrees := append([]WorktreeEntry{{Root: mainRoot, Name: name, Branch: mainBranch, IsMain: true}}, linked...)
	return RepoInfo{
		CommonDir:  commonDir,
		Name:       name,
		MainRoot:   mainRoot,
		MainBranch: mainBranch,
		Worktrees:  worktrees,
	}, nil
}

// ScanCwds returns checkouts and repositories for cwds. It blocks on file
// reads: call it only from a goroutine, never from the UI thread. A cwd outside
// git is left out. A repository that cannot be read is left out too, with every
// checkout of it.
func ScanCwds(cwds []string) Scan {
	scan := Scan{Checkouts: map[string]Checkout{}, Repos: map[string]RepoInfo{}}
	for _, cwd := range cwds {
		if _, seen := scan.Checkouts[cwd]; seen {
			continue
		}
		if checkout, ok := Discover(cwd); ok {
			scan.Checkouts[cwd] = checkout
		}
	}
	tried := map[string]bool{}
	for _, checkout := range scan.Checkouts {
		if tried[checkout.CommonDir] {
			continue
		}
		tried[checkout.CommonDir] = true
		if repo, err := ReadRepo(checkout.CommonDir); err == nil {
			scan.Repos[repo.CommonDir] = repo
		}
	}
	for cwd, checkout := range scan.Checkouts {
		if _, ok := scan.Repos[checkout.CommonDir]; !ok {
			delete(scan.Checkouts, cwd)
		}
	}
	return scan
}

// linkedCheckout reads a linked worktree from its `.git` file
// (`gitdir: <admin dir>`).
func linkedCheckout(root, dotGit string) (Checkout, bool) {
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return Checkout{}, false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	target := strings.TrimSpace(rest)
	if !ok || target == "" {
		return Checkout{}, false
	}
	gitDir, err := canonical(joinPath(root, target))
	if err != nil {
		return Checkout{}, false
	}
	commonDir, ok := commonDirOf(gitDir)
	if !ok {
		return Checkout{}, false
	}
	if _, ok := mainRootOf(commonDir); !ok {
		return Checkout{}, false
	}
	return Checkout{Root: root, GitDir: gitDir, CommonDir: commonDir}, true
}

// commonDirOf returns the shared git dir named by `commondir` (relative to
// gitDir). Without the file, gitDir is its own common dir.
func commonDirOf(gitDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return gitDir, true
		}
		return "", false
	}
	dir, err := canonical(joinPath(gitDir, strings.TrimSpace(string(data))))
	if err != nil {
		return "", false
	}
	return dir, true
}

// mainRootOf returns the main root of a shared git dir: `<root>/.git` gives
// `<root>`. It reports false for bare repositories and any other layout.
func mainRootOf(commonDir string) (string, bool) {
	if filepath.Base(commonDir) != ".git" {
		return "", false
	}
	return filepath.Dir(commonDir), true
}

// linkedWorktrees lists the linked worktrees under `<common>/worktrees/`,
// unsorted. A missing directory gives none.
func linkedWorktrees(commonDir string) ([]WorktreeEntry, error) {
	entries, err := os.ReadDir(filepath.Join(commonDir, "worktrees"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var linked []WorktreeEntry
	for _, entry := range entries {
		admin := filepath.Join(commonDir, "worktrees", entry.Name())
		if info, err := os.Stat(admin); err != nil || !info.IsDir() {
			continue
		}
		root, ok := linkedRoot(admin)
		if !ok {
			continue
		}
		name := dirName(root)
		if name == "" {
			continue
		}
		branch, _ := HeadBranch(admin)
		linked = append(linked, WorktreeEntry{Root: root, Name: name, Branch: branch})
	}
	return linked, nil
}

// linkedRoot returns the root of the worktree that an admin dir points at: its
// `gitdir` file names `<root>/.git`, relative to the admin dir or absolute. It
// reports false when the file is missing or empty, or the `.git` file is gone.
func linkedRoot(admin string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil {
		return "", false
	}
	target := strings.TrimSpace(string(data))
	if target == "" {
		return "", false
	}
	dotGit, err := canonical(joinPath(admin, target))
	if err != nil {
		return "", false
	}
	return filepath.Dir(dotGit), true
}

// canonical returns the absolute path with symlinks resolved. The path must
// exist.
func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// joinPath joins target to base; an absolute target replaces base.
func joinPath(base, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(base, target)
}

// dirName returns the last element of path, or "" when it has none.
func dirName(path string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

// comparePaths orders paths by their elements, as a path-aware sort does.
func comparePaths(a, b string) int {
	return slices.Compare(strings.Split(a, string(filepath.Separator)), strings.Split(b, string(filepath.Separator)))
}

func isHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
