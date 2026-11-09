package review

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func names(es []Entry) []string {
	var out []string
	for _, e := range es {
		if e.Dir {
			out = append(out, e.Name+"/")
		} else {
			out = append(out, e.Name)
		}
	}
	return out
}

func TestEntriesRespectGitignoreAndSortFoldersFirst(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{".gitignore": "build/\n*.log\n", "b.go": "x\n", "A.md": "x\n", "src/main.go": "x\n"})
	s.write("build/out.bin", "x")
	s.write("debug.log", "x")
	s.write("new.txt", "x")
	got, err := s.repo().Entries(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/", ".gitignore", "A.md", "b.go", "new.txt"}
	if !reflect.DeepEqual(names(got), want) {
		t.Errorf("entries %v, want %v", names(got), want)
	}
	// One level only: the folder is read on its own.
	sub, err := s.repo().Entries(t.Context(), "src")
	if err != nil || !reflect.DeepEqual(names(sub), []string{"main.go"}) {
		t.Errorf("src entries %v, %v", names(sub), err)
	}
}

func TestEntriesOutsideGitShowEverythingButNoDotGit(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.log"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	got, err := Repo{Dir: dir, Base: "main"}.Entries(t.Context(), "")
	if err != nil || !reflect.DeepEqual(names(got), []string{"a.log"}) {
		t.Errorf("entries %v, %v", names(got), err)
	}
	if _, err := (Repo{Dir: dir}).Entries(t.Context(), "missing"); err == nil {
		t.Error("a missing folder gave no error")
	}
}

func TestChangesCountsModifiedNewAndDeleted(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"keep.go": "1\n2\n3\n", "gone.go": "1\n2\n", ".gitignore": "*.log\n"})
	s.git("checkout", "-b", "feature")
	s.commit("committed", map[string]string{"done.go": "a\nb\n"})
	s.write("keep.go", "1\n2\n3\n4\n5\n") // unstaged: +2
	os.Remove(filepath.Join(s.dir, "gone.go"))
	s.write("fresh.go", "x\ny\nz") // untracked, 3 lines without the final newline
	s.write("skip.log", "ignored\n")
	got, err := s.repo().Changes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(got)
	want := map[string]File{
		"keep.go":  {Path: "keep.go", Status: Modified, Added: 2},
		"gone.go":  {Path: "gone.go", Status: Deleted, Removed: 2},
		"done.go":  {Path: "done.go", Status: Added, Added: 2},
		"fresh.go": {Path: "fresh.go", Status: Added, Added: 3},
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("changes %+v, want %+v", m, want)
	}
}

func TestChangesNotARepoAndBinaryNewFile(t *testing.T) {
	got, err := Repo{Dir: t.TempDir(), Base: "main"}.Changes(t.Context())
	if err != nil || got != nil {
		t.Errorf("not a repo: %v, %v", got, err)
	}
	s := scratch(t) // no commit yet: every file is new
	s.write("bin.dat", "a\x00b")
	s.write("t.txt", "one\n")
	got, err = s.repo().Changes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m := byPath(got)
	if !m["bin.dat"].Binary || m["t.txt"].Added != 1 || m["t.txt"].Status != Added {
		t.Errorf("changes %+v", m)
	}
}

func TestCurrentIsTheCheckedOutBranch(t *testing.T) {
	s := scratch(t)
	s.commit("base", map[string]string{"f": "1\n"})
	s.git("checkout", "-b", "topic")
	if got := s.repo().Current(t.Context()); got != "topic" {
		t.Errorf("current %q, want topic", got)
	}
	if got := (Repo{Dir: t.TempDir()}).Current(t.Context()); got != "" {
		t.Errorf("outside git: %q", got)
	}
}
