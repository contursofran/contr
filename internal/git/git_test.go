package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

// Fake git layouts on disk (plain files, no `git` binary).

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// mainRepo makes a main checkout at root with `.git/HEAD` = head. It returns
// the git dir.
func mainRepo(t *testing.T, root, head string) string {
	t.Helper()
	gitDir := filepath.Join(root, ".git")
	write(t, filepath.Join(gitDir, "HEAD"), head)
	return gitDir
}

// linked makes a linked worktree at root with admin dir
// `<common>/worktrees/<name>`, laid out as git writes it. It returns the admin
// dir.
func linked(t *testing.T, common, name, root, head string) string {
	t.Helper()
	admin := filepath.Join(common, "worktrees", name)
	write(t, filepath.Join(admin, "HEAD"), head)
	write(t, filepath.Join(admin, "gitdir"), filepath.Join(root, ".git")+"\n")
	write(t, filepath.Join(admin, "commondir"), "../..\n")
	write(t, filepath.Join(root, ".git"), "gitdir: "+admin+"\n")
	return admin
}

// tmp returns the canonical path of a temporary directory.
func tmp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func branchOf(t *testing.T, gitDir string) (string, bool) {
	t.Helper()
	return HeadBranch(gitDir)
}

func TestHeadBranchReadsBranchesAndShortShas(t *testing.T) {
	git := filepath.Join(tmp(t), ".git")
	write(t, filepath.Join(git, "HEAD"), "ref: refs/heads/agent/6-x\n")
	if b, ok := branchOf(t, git); !ok || b != "agent/6-x" {
		t.Fatalf("got %q %v", b, ok)
	}
	write(t, filepath.Join(git, "HEAD"), sha+"\n")
	if b, ok := branchOf(t, git); !ok || b != "0123456" {
		t.Fatalf("got %q %v", b, ok)
	}
}

func TestHeadBranchRejectsHeadsThatNameNothing(t *testing.T) {
	git := filepath.Join(tmp(t), ".git")
	if _, ok := branchOf(t, git); ok {
		t.Fatal("no HEAD file")
	}
	for _, head := range []string{"ref: refs/heads/", "ref: refs/remotes/o/main", "xyz", ""} {
		write(t, filepath.Join(git, "HEAD"), head)
		if b, ok := branchOf(t, git); ok {
			t.Fatalf("HEAD %q gave %q", head, b)
		}
	}
}

func TestDiscoverFindsTheMainCheckoutFromANestedDir(t *testing.T) {
	base := tmp(t)
	repo := filepath.Join(base, "repo")
	git := mainRepo(t, repo, "ref: refs/heads/main\n")
	deep := filepath.Join(repo, "src/a/b")
	mkdir(t, deep)
	found, ok := Discover(deep)
	if !ok {
		t.Fatal("not found")
	}
	want := Checkout{Root: repo, GitDir: git, CommonDir: git, IsMain: true}
	if found != want {
		t.Fatalf("got %+v want %+v", found, want)
	}
	if b, _ := HeadBranch(found.GitDir); b != "main" {
		t.Fatalf("branch %q", b)
	}
}

func TestDiscoverOutsideGitOrMissingIsNone(t *testing.T) {
	base := tmp(t)
	mkdir(t, filepath.Join(base, "plain"))
	if _, ok := Discover(filepath.Join(base, "plain")); ok {
		t.Fatal("plain dir found")
	}
	if _, ok := Discover(filepath.Join(base, "missing")); ok {
		t.Fatal("missing dir found")
	}
}

func TestDiscoverResolvesALinkedWorktreeDeepInsideIt(t *testing.T) {
	base := tmp(t)
	git := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	wt := filepath.Join(base, "wt1")
	admin := linked(t, git, "w1", wt, "ref: refs/heads/feature/x\n")
	mkdir(t, filepath.Join(wt, "src/deep"))
	found, ok := Discover(filepath.Join(wt, "src/deep"))
	if !ok {
		t.Fatal("not found")
	}
	want := Checkout{Root: wt, GitDir: admin, CommonDir: git}
	if found != want {
		t.Fatalf("got %+v want %+v", found, want)
	}
}

func TestRelativeGitdirAndCommondirResolve(t *testing.T) {
	base := tmp(t)
	git := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	wt := filepath.Join(base, "wt2")
	admin := filepath.Join(git, "worktrees/w2")
	write(t, filepath.Join(admin, "HEAD"), sha+"\n")
	write(t, filepath.Join(admin, "gitdir"), "../../../../wt2/.git\n")
	write(t, filepath.Join(admin, "commondir"), "../..\n")
	write(t, filepath.Join(wt, ".git"), "gitdir: ../repo/.git/worktrees/w2\n")
	found, ok := Discover(wt)
	if !ok {
		t.Fatal("not found")
	}
	if found.Root != wt || found.CommonDir != git || found.IsMain {
		t.Fatalf("got %+v", found)
	}
	info, err := ReadRepo(git)
	if err != nil {
		t.Fatal(err)
	}
	if info.Worktrees[1].Root != wt || info.Worktrees[1].Branch != "0123456" {
		t.Fatalf("got %+v", info.Worktrees[1])
	}
}

func TestNestedWorktreeInsideTheMainRootIsLinkedNotMain(t *testing.T) {
	base := tmp(t)
	repo := filepath.Join(base, "repo")
	git := mainRepo(t, repo, "ref: refs/heads/main\n")
	wt := filepath.Join(repo, ".worktrees/w1")
	linked(t, git, "w1", wt, "ref: refs/heads/b\n")
	mkdir(t, filepath.Join(wt, "src"))
	mkdir(t, filepath.Join(repo, "src"))
	inside, ok := Discover(filepath.Join(wt, "src"))
	if !ok || inside.IsMain || inside.Root != wt {
		t.Fatalf("got %+v %v", inside, ok)
	}
	if outer, ok := Discover(filepath.Join(repo, "src")); !ok || !outer.IsMain {
		t.Fatalf("got %+v %v", outer, ok)
	}
}

func TestReadRepoListsMainFirstThenLinkedByPath(t *testing.T) {
	base := tmp(t)
	repo := filepath.Join(base, "repo")
	git := mainRepo(t, repo, "ref: refs/heads/main\n")
	linked(t, git, "one", filepath.Join(base, "zz-wt"), "ref: refs/heads/z\n")
	linked(t, git, "two", filepath.Join(base, "aa-wt"), sha+"\n")
	info, err := ReadRepo(git)
	if err != nil {
		t.Fatal(err)
	}
	if info.CommonDir != git || info.Name != "repo" || info.MainRoot != repo || info.MainBranch != "main" {
		t.Fatalf("got %+v", info)
	}
	type row struct {
		name   string
		isMain bool
		branch string
	}
	var rows []row
	for _, w := range info.Worktrees {
		rows = append(rows, row{w.Name, w.IsMain, w.Branch})
	}
	want := []row{{"repo", true, "main"}, {"aa-wt", false, "0123456"}, {"zz-wt", false, "z"}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("got %v want %v", rows, want)
	}
}

func TestReadRepoWithoutWorktreesDirListsOnlyMain(t *testing.T) {
	git := mainRepo(t, filepath.Join(tmp(t), "repo"), "ref: refs/heads/main\n")
	info, err := ReadRepo(git)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Worktrees) != 1 || !info.Worktrees[0].IsMain {
		t.Fatalf("got %+v", info.Worktrees)
	}
}

func TestReadRepoDropsPrunableAndBrokenEntries(t *testing.T) {
	base := tmp(t)
	git := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	// Checkout gone: `.git` target does not exist.
	linked(t, git, "gone", filepath.Join(base, "gone"), "ref: refs/heads/x\n")
	if err := os.RemoveAll(filepath.Join(base, "gone")); err != nil {
		t.Fatal(err)
	}
	// Admin dir without a `gitdir` file, and one with an empty `gitdir`.
	write(t, filepath.Join(git, "worktrees/nofile/HEAD"), "ref: refs/heads/y\n")
	write(t, filepath.Join(git, "worktrees/empty/gitdir"), "\n")
	// A stray file is not an admin dir.
	write(t, filepath.Join(git, "worktrees/stray"), "x")
	info, err := ReadRepo(git)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Worktrees) != 1 || !info.Worktrees[0].IsMain {
		t.Fatalf("got %+v", info.Worktrees)
	}
}

func TestReadRepoRejectsMissingAndNonGitPaths(t *testing.T) {
	base := tmp(t)
	mkdir(t, filepath.Join(base, "plain"))
	if _, err := ReadRepo(filepath.Join(base, "plain")); !errors.Is(err, ErrNotGitDir) {
		t.Fatalf("plain: %v", err)
	}
	if _, err := ReadRepo(filepath.Join(base, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}

func TestReadRepoFailsWhenWorktreesIsNotADirectory(t *testing.T) {
	git := mainRepo(t, filepath.Join(tmp(t), "repo"), "ref: refs/heads/main\n")
	write(t, filepath.Join(git, "worktrees"), "not a dir")
	if _, err := ReadRepo(git); err == nil {
		t.Fatal("want error")
	}
}

func TestMalformedOrDanglingDotGitIsNotACheckout(t *testing.T) {
	wt := filepath.Join(tmp(t), "wt")
	for _, c := range []struct{ text, why string }{
		{"hello\n", "no gitdir line"},
		{"gitdir:\n", "empty gitdir"},
		{"gitdir: ../nowhere\n", "target missing"},
	} {
		write(t, filepath.Join(wt, ".git"), c.text)
		if _, ok := Discover(wt); ok {
			t.Fatal(c.why)
		}
	}
}

func TestLinkedAdminWithoutAUsableCommondirIsNotACheckout(t *testing.T) {
	base := tmp(t)
	git := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	wt := filepath.Join(base, "wt")
	admin := linked(t, git, "w", wt, "ref: refs/heads/b\n")
	if err := os.Remove(filepath.Join(admin, "commondir")); err != nil {
		t.Fatal(err)
	}
	// Without the file, the admin dir is its own common dir: not `.git`.
	if _, ok := Discover(wt); ok {
		t.Fatal("commondir missing")
	}
	mkdir(t, filepath.Join(admin, "commondir"))
	if _, ok := Discover(wt); ok {
		t.Fatal("commondir unreadable")
	}
}

func TestBareRepositoriesAreNotCheckouts(t *testing.T) {
	base := tmp(t)
	bare := filepath.Join(base, "bare.git")
	write(t, filepath.Join(bare, "HEAD"), "ref: refs/heads/main\n")
	if _, ok := Discover(bare); ok {
		t.Fatal("bare dir itself")
	}
	wt := filepath.Join(base, "wt")
	linked(t, bare, "w", wt, "ref: refs/heads/b\n")
	if _, ok := Discover(wt); ok {
		t.Fatal("worktree of a bare repo")
	}
	if _, err := ReadRepo(bare); err == nil {
		t.Fatal("want error")
	}
}

func TestScanMapsCwdsAndSharesOneRepo(t *testing.T) {
	base := tmp(t)
	repo := filepath.Join(base, "repo")
	git := mainRepo(t, repo, "ref: refs/heads/main\n")
	wt := filepath.Join(base, "wt1")
	linked(t, git, "w1", wt, "ref: refs/heads/f\n")
	mkdir(t, filepath.Join(repo, "src"))
	mkdir(t, filepath.Join(base, "plain"))
	src, plain := filepath.Join(repo, "src"), filepath.Join(base, "plain")
	got := ScanCwds([]string{src, wt, src, plain})
	if len(got.Repos) != 1 || len(got.Checkouts) != 2 {
		t.Fatalf("got %d repos, %d checkouts", len(got.Repos), len(got.Checkouts))
	}
	if _, ok := got.Checkouts[plain]; ok {
		t.Fatal("plain dir in scan")
	}
	if !got.Checkouts[src].IsMain || got.Checkouts[wt].IsMain {
		t.Fatalf("got %+v", got.Checkouts)
	}
	if len(got.Repos[git].Worktrees) != 2 {
		t.Fatalf("got %+v", got.Repos[git])
	}
}

func TestScanOfNothingIsEmpty(t *testing.T) {
	for _, cwds := range [][]string{nil, {"/no/such/dir/anywhere"}} {
		got := ScanCwds(cwds)
		if len(got.Checkouts) != 0 || len(got.Repos) != 0 {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestScanDropsCheckoutsOfARepoThatCannotBeRead(t *testing.T) {
	base := tmp(t)
	git := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	write(t, filepath.Join(git, "worktrees"), "not a dir")
	got := ScanCwds([]string{filepath.Join(base, "repo")})
	if len(got.Repos) != 0 || len(got.Checkouts) != 0 {
		t.Fatalf("got %+v", got)
	}
}
