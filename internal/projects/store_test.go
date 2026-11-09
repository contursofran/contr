package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil || s == nil {
		t.Fatalf("Load = %v, %v", s, err)
	}
	if got := s.Order([]string{"a"}); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("order = %v", got)
	}
}

func TestLoadCorruptFileReturnsErrorAndEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(p, []byte("{nope"), 0o644)
	s, err := Load(p)
	if err == nil || s == nil || s.Hidden("a") {
		t.Fatalf("Load = %v, %v", s, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "{nope" {
		t.Fatal("corrupt file was changed")
	}
}

func TestLoadWrongVersionIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(p, []byte(`{"version":2}`), 0o644)
	if _, err := Load(p); err == nil {
		t.Fatal("want error")
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "p.json")
	s := &Store{}
	s.SetOrder([]string{"b", "a"})
	s.Hide("c")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(p); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.order, []string{"b", "a"}) || !got.Hidden("c") {
		t.Fatalf("got %+v", got)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "p.json")
	s := &Store{}
	s.Hide("a")
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	if e, _ := os.ReadDir(dir); len(e) != 1 {
		t.Fatalf("temp file left: %v", e)
	}
	// A read-only folder: the write fails and the old file stays whole.
	os.Chmod(dir, 0o555)
	defer os.Chmod(dir, 0o755)
	s.Hide("b")
	if err := s.Save(p); err == nil {
		t.Skip("folder is writable (running as root?)")
	}
	got, err := Load(p)
	if err != nil || !got.Hidden("a") || got.Hidden("b") {
		t.Fatalf("old file changed: %+v, %v", got, err)
	}
	os.Chmod(dir, 0o755)
	if e, _ := os.ReadDir(dir); len(e) != 1 {
		t.Fatalf("temp file left: %v", e)
	}
}

func TestOrderPutsUnknownAfterKnownInTreeOrder(t *testing.T) {
	s := &Store{}
	s.SetOrder([]string{"c", "a"})
	got := s.Order([]string{"a", "x", "c", "y"})
	if want := []string{"c", "a", "x", "y"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestOrderDropsGoneDirs(t *testing.T) {
	s := &Store{}
	s.SetOrder([]string{"gone", "a"})
	if got := s.Order([]string{"a"}); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("got %v", got)
	}
}

func TestMoveMatchesReorder(t *testing.T) {
	s := &Store{}
	s.SetOrder([]string{"a", "b", "c"})
	s.Move([]int{2}, 0)
	if want := []string{"c", "a", "b"}; !slices.Equal(s.order, want) {
		t.Fatalf("got %v", s.order)
	}
	s.Move([]int{0}, 3) // to the end
	if want := []string{"a", "b", "c"}; !slices.Equal(s.order, want) {
		t.Fatalf("got %v", s.order)
	}
	s.Move([]int{0, 1}, 3)
	if want := []string{"c", "a", "b"}; !slices.Equal(s.order, want) {
		t.Fatalf("got %v", s.order)
	}
	s.Move([]int{9}, 0) // out of range: no change
	if want := []string{"c", "a", "b"}; !slices.Equal(s.order, want) {
		t.Fatalf("got %v", s.order)
	}
}

func TestHideShow(t *testing.T) {
	s := &Store{}
	s.Hide("a")
	s.Hide("a")
	if !s.Hidden("a") || len(s.hidden) != 1 {
		t.Fatalf("hidden = %v", s.hidden)
	}
	s.Show("a")
	if s.Hidden("a") {
		t.Fatal("still hidden")
	}
}

func TestPathIsUnderConfigFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	p, err := Path()
	if err != nil || filepath.Base(p) != "projects.json" || filepath.Base(filepath.Dir(p)) != "contr" {
		t.Fatalf("Path = %q, %v", p, err)
	}
}

func TestCloneSharesNothing(t *testing.T) {
	s := &Store{}
	s.SetOrder([]string{"a", "b"})
	s.Hide("a")
	c := s.Clone()
	s.SetOrder([]string{"b"})
	s.Show("a")
	s.Hide("b")
	if !c.Hidden("a") || c.Hidden("b") {
		t.Errorf("hidden changed in the copy")
	}
	if got := c.Order([]string{"a", "b"}); got[0] != "a" || got[1] != "b" {
		t.Errorf("order changed in the copy: %v", got)
	}
}

func TestRecentNewestFirstNoDuplicatesCapped(t *testing.T) {
	s := &Store{}
	at := time.Unix(1000, 0)
	s.Touch("/a/.git", "/a", at)
	s.Touch("/b/.git", "/b", at.Add(time.Second))
	s.Touch("/a/.git", "/a2", at.Add(2*time.Second))
	got := s.Recent()
	if len(got) != 2 || got[0].Dir != "/a/.git" || got[0].Path != "/a2" || got[1].Dir != "/b/.git" {
		t.Fatalf("recent = %v", got)
	}
	for i := range MaxRecent + 5 {
		s.Touch(fmt.Sprintf("/p%d/.git", i), "/p", at.Add(time.Duration(10+i)*time.Second))
	}
	got = s.Recent()
	if len(got) != MaxRecent || got[0].Dir != fmt.Sprintf("/p%d/.git", MaxRecent+4) {
		t.Fatalf("recent = %v", got)
	}
}

func TestFoldersKeepOrderAndHide(t *testing.T) {
	s := &Store{}
	s.AddFolder("/a/.git", "/a")
	s.AddFolder("/b/.git", "/b")
	s.AddFolder("/a/.git", "/a")
	if got := s.Folders(); !slices.Equal(got, []string{"/a/.git", "/b/.git"}) {
		t.Fatalf("folders = %v", got)
	}
	s.Hide("/a/.git")
	if got := s.Folders(); !slices.Equal(got, []string{"/b/.git"}) {
		t.Fatalf("hidden folder shows: %v", got)
	}
	s.AddFolder("/a/.git", "/a")
	if s.Hidden("/a/.git") || s.Path("/a/.git") != "/a" {
		t.Fatal("adding a folder again does not show it")
	}
}

func TestRecentAndFoldersSurviveSaveAndLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.json")
	s := &Store{}
	s.AddFolder("/a/.git", "/a")
	s.Touch("/a/.git", "/a", time.Unix(5, 0).UTC())
	if err := s.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil || len(got.Recent()) != 1 || got.Path("/a/.git") != "/a" {
		t.Fatalf("Load = %v, %v", got, err)
	}
	// A copy shares nothing.
	c := got.Clone()
	c.AddFolder("/z/.git", "/z")
	if got.Path("/z/.git") != "" {
		t.Fatal("clone shares folders")
	}
}

func TestAddLive(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	s := &Store{}
	s.SetOrder([]string{"/a/.git"})
	s.Hide("/h/.git")
	if !s.AddLive("/n/.git", "/n", at) {
		t.Fatal("a new project was not added")
	}
	if s.AddLive("/n/.git", "/n", at.Add(time.Hour)) || s.AddLive("/a/.git", "/a", at) {
		t.Error("a known project was added again")
	}
	if s.AddLive("/h/.git", "/h", at) {
		t.Error("a hidden project was added")
	}
	if !s.Hidden("/h/.git") || s.Path("/h/.git") != "" {
		t.Error("the hidden project changed")
	}
	if got := s.Order([]string{"/n/.git", "/a/.git"}); got[0] != "/a/.git" || got[1] != "/n/.git" {
		t.Errorf("order %v, want the new project last", got)
	}
	if r := s.Recent(); len(r) != 1 || r[0].Dir != "/n/.git" || !r[0].At.Equal(at) {
		t.Errorf("recent %+v", r)
	}
	if f := s.Folders(); len(f) != 1 || f[0] != "/n/.git" || s.Path("/n/.git") != "/n" {
		t.Errorf("folders %v", f)
	}
}
