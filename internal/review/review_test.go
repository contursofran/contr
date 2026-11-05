package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type scratchRepo struct {
	t   *testing.T
	dir string
}

// scratch makes a repository with branch main in a temporary folder.
func scratch(t *testing.T) *scratchRepo {
	t.Helper()
	s := &scratchRepo{t: t, dir: t.TempDir()}
	s.git("init", "-b", "main")
	return s
}

func (s *scratchRepo) repo() Repo { return Repo{Dir: s.dir, Base: "main"} }

func (s *scratchRepo) git(args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", s.dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (s *scratchRepo) write(path, text string) {
	s.t.Helper()
	p := filepath.Join(s.dir, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// commit writes the files, stages everything and returns the new SHA.
func (s *scratchRepo) commit(msg string, files map[string]string) string {
	s.t.Helper()
	for p, text := range files {
		s.write(p, text)
	}
	s.git("add", "-A")
	s.git("commit", "-m", msg)
	return s.git("rev-parse", "HEAD")
}

func byPath(files []File) map[string]File {
	m := map[string]File{}
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}

func TestBranchesSkipBaseAndCount(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f.txt": "1\n2\n3\n"})
	s.git("checkout", "-b", "a")
	s.commit("one", map[string]string{"f.txt": "1\n3\nx\ny\n"}) // -1 (2), +2
	s.commit("two", map[string]string{"g.txt": "g\n"})          // +1
	got, err := s.repo().Branches()
	if err != nil {
		t.Fatal(err)
	}
	want := []Branch{{Name: "a", Ahead: 2, Added: 3, Removed: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestBranchesSortedByName(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("branch", "z")
	s.git("branch", "b")
	got, err := s.repo().Branches()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "z" || got[0].Ahead != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestFilesStatuses(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"mod.txt": "a\nb\n", "del.txt": "x\ny\n"})
	s.git("checkout", "-b", "b")
	s.commit("work", map[string]string{"add.txt": "1\n2\n3\n", "mod.txt": "a\nB\nc\n"})
	s.git("rm", "-q", "del.txt")
	s.git("commit", "-m", "rm")
	files, err := s.repo().Files("b")
	if err != nil {
		t.Fatal(err)
	}
	got := byPath(files)
	want := map[string]File{
		"add.txt": {Path: "add.txt", Status: Added, Added: 3},
		"mod.txt": {Path: "mod.txt", Status: Modified, Added: 2, Removed: 1},
		"del.txt": {Path: "del.txt", Status: Deleted, Removed: 2},
	}
	if !reflect.DeepEqual(got, want) || len(files) != 3 {
		t.Fatalf("got %+v, want %+v", files, want)
	}
}

func TestFilesBinary(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("checkout", "-b", "b")
	s.commit("bin", map[string]string{"img.bin": "a\x00b\x00\x01"})
	files, err := s.repo().Files("b")
	if err != nil {
		t.Fatal(err)
	}
	want := []File{{Path: "img.bin", Status: Added, Binary: true}}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("got %+v, want %+v", files, want)
	}
}

func TestFilesSinceMergeBase(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("checkout", "-b", "b")
	s.commit("mine", map[string]string{"mine.txt": "m\n"})
	s.git("checkout", "main")
	s.commit("later", map[string]string{"later.txt": "l\n"})
	files, err := s.repo().Files("b")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "mine.txt" {
		t.Fatalf("got %+v", files)
	}
}

func TestCommitsOldestFirstNoMerges(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("checkout", "-b", "b")
	first := s.commit("first", map[string]string{"a.txt": "1\n2\n"})
	second := s.commit("second", map[string]string{"a.txt": "1\n", "b.txt": "x\n"})
	s.git("checkout", "main")
	s.commit("later", map[string]string{"later.txt": "l\n"})
	s.git("checkout", "b")
	s.git("merge", "--no-ff", "-m", "merge main", "main")
	got, err := s.repo().Commits("b")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].SHA != first || got[0].Subject != "first" || got[0].Added != 2 || got[0].Removed != 0 || got[0].Short == "" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].SHA != second || got[1].Subject != "second" || got[1].Added != 1 || got[1].Removed != 1 {
		t.Errorf("second: %+v", got[1])
	}
}

func TestCommitFilesRootCommit(t *testing.T) {
	s := scratch(t)
	root := s.commit("root", map[string]string{"a.txt": "1\n", "d/b.txt": "2\n3\n"})
	files, err := s.repo().CommitFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []File{{Path: "a.txt", Status: Added, Added: 1}, {Path: "d/b.txt", Status: Added, Added: 2}}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("got %+v, want %+v", files, want)
	}
}

func TestTextsWholeBranch(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f.txt": "old\n"})
	s.git("checkout", "-b", "b")
	s.commit("one", map[string]string{"f.txt": "mid\n"})
	s.commit("two", map[string]string{"f.txt": "new\n"})
	s.git("checkout", "main")
	s.commit("later", map[string]string{"f.txt": "main moved\n"})
	before, after, err := s.repo().Texts("b", "", "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if before != "old\n" || after != "new\n" {
		t.Fatalf("before %q after %q", before, after)
	}
}

func TestTextsAddedDeleted(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"gone.txt": "bye\n"})
	s.git("checkout", "-b", "b")
	s.commit("add", map[string]string{"new.txt": "hi\n"})
	s.git("rm", "-q", "gone.txt")
	s.git("commit", "-m", "rm")
	r := s.repo()
	before, after, err := r.Texts("b", "", "new.txt")
	if err != nil || before != "" || after != "hi\n" {
		t.Errorf("added: %q %q %v", before, after, err)
	}
	before, after, err = r.Texts("b", "", "gone.txt")
	if err != nil || before != "bye\n" || after != "" {
		t.Errorf("deleted: %q %q %v", before, after, err)
	}
}

func TestTextsCommitOnlyItsChange(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"other": "o\n"})
	s.git("checkout", "-b", "b")
	sha1 := s.commit("one", map[string]string{"p.txt": "v1\n"})
	s.commit("two", map[string]string{"p.txt": "v2\n"})
	sha3 := s.commit("three", map[string]string{"p.txt": "v3\n"})
	r := s.repo()
	before, after, err := r.Texts("b", sha1, "p.txt")
	if err != nil || before != "" || after != "v1\n" {
		t.Errorf("first: %q %q %v", before, after, err)
	}
	before, after, err = r.Texts("b", sha3, "p.txt")
	if err != nil || before != "v2\n" || after != "v3\n" {
		t.Errorf("last: %q %q %v", before, after, err)
	}
}

func TestTextsRootCommit(t *testing.T) {
	s := scratch(t)
	root := s.commit("root", map[string]string{"a.txt": "1\n"})
	before, after, err := s.repo().Texts("main", root, "a.txt")
	if err != nil || before != "" || after != "1\n" {
		t.Fatalf("%q %q %v", before, after, err)
	}
}

func TestGitErrorHasStderr(t *testing.T) {
	dir := t.TempDir()
	// A temporary folder inside a repository would find that repository.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	r := Repo{Dir: dir, Base: "main"}
	_, err := r.Branches()
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "git") || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error %q", err)
	}
}

func TestUnicodePath(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("checkout", "-b", "b")
	s.commit("add", map[string]string{"ñ.txt": "uno\n"})
	files, err := s.repo().Files("b")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "ñ.txt" || files[0].Added != 1 {
		t.Fatalf("got %+v", files)
	}
	_, after, err := s.repo().Texts("b", "", "ñ.txt")
	if err != nil || after != "uno\n" {
		t.Fatalf("texts %q %v", after, err)
	}
}

func TestTotalAddsUpTheChangesOfTheWorkTree(t *testing.T) {
	s := scratch(t)
	s.commit("root", map[string]string{"a.txt": "1\n2\n3\n", "b.txt": "x\n"})
	s.git("checkout", "-b", "one")
	s.commit("one", map[string]string{"a.txt": "1\n"}) // committed: -2
	s.write("b.txt", "x\ny\n")                         // uncommitted: +1
	s.write("c.txt", "p\nq\n")                         // untracked: +2
	files, err := s.repo().Changes()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Total(files), (Summary{Added: 3, Removed: 2, Files: 3}); got != want {
		t.Errorf("Total = %+v, want %+v", got, want)
	}
}

func TestTotalOfNoFilesIsZero(t *testing.T) {
	if got := Total(nil); got != (Summary{}) {
		t.Errorf("Total(nil) = %+v, want zero", got)
	}
}
