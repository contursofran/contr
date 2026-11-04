package projects

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
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
