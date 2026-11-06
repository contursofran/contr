package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReadMissingIsEmpty(t *testing.T) {
	s, err := Read(t.TempDir(), "t1")
	if err != nil || s == nil || len(s.Todos) != 0 {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestWriteRead(t *testing.T) {
	dir := t.TempDir()
	in := &Session{UpdatedAt: 5, Todos: []Todo{NewTodo("[you] Fix it", "Fixing it", InProgress)}}
	if err := Write(dir, "w/1", in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, Dir, "w_1.json")); err != nil {
		t.Fatal(err)
	}
	out, err := Read(dir, "w/1")
	if err != nil {
		t.Fatal(err)
	}
	if out.Schema != Schema || len(out.Todos) != 1 || !out.Todos[0].You || out.Todos[0].Text != "Fix it" {
		t.Fatalf("got %+v", out)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, Dir))
	if len(entries) != 1 {
		t.Fatalf("temp file left: %v", entries)
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(dir, "///"); err == nil {
		t.Fatal("no error for a bad id")
	}
	if err := Write("", "t", &Session{}); err == nil {
		t.Fatal("no error for no dir")
	}
	_ = os.MkdirAll(filepath.Join(dir, Dir), 0o755)
	_ = os.WriteFile(filepath.Join(dir, Dir, "t.json"), []byte("{"), 0o644)
	if _, err := Read(dir, "t"); err == nil {
		t.Fatal("no error for a corrupt file")
	}
	_ = os.WriteFile(filepath.Join(dir, Dir, "t.json"), []byte(`{"schema":9}`), 0o644)
	if _, err := Read(dir, "t"); err == nil {
		t.Fatal("no error for another schema")
	}
}

func TestAddArtifact(t *testing.T) {
	var s Session
	s.AddArtifact(Artifact{Slug: "a", Title: "A", URL: "u"})
	s.AddArtifact(Artifact{Slug: "b"})
	s.AddArtifact(Artifact{Slug: "a", UpdatedAt: 3})
	if len(s.Artifacts) != 2 || s.Artifacts[0].Slug != "a" || s.Artifacts[0].Title != "A" || s.Artifacts[0].URL != "u" {
		t.Fatalf("got %+v", s.Artifacts)
	}
	for i := range 30 {
		s.AddArtifact(Artifact{Slug: fmt.Sprint(i)})
	}
	if len(s.Artifacts) != MaxArtifacts || s.Artifacts[0].Slug != "29" {
		t.Fatalf("got %d, top %s", len(s.Artifacts), s.Artifacts[0].Slug)
	}
}
