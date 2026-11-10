package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestUpdateConcurrentKeepsAll(t *testing.T) {
	dir := t.TempDir()
	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Update(dir, "t1", func(s *Session) (bool, error) {
				s.Todos = append(s.Todos, NewTodo(fmt.Sprint(i), "", Pending))
				return true, nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := Read(dir, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Todos) != n {
		t.Fatalf("kept %d of %d todos", len(s.Todos), n)
	}
}

func TestUpdateReplacesBadFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, Dir), 0o755)
	if err := os.WriteFile(filepath.Join(dir, Dir, "t1.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Update(dir, "t1", func(s *Session) (bool, error) {
		if len(s.Todos) != 0 {
			t.Fatalf("bad file gave %+v", s)
		}
		s.Todos = append(s.Todos, NewTodo("new", "", Pending))
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Read(dir, "t1")
	if err != nil || len(s.Todos) != 1 || s.Todos[0].Text != "new" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestUpdateNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Dir, "t1.json")
	if err := Update(dir, "t1", func(*Session) (bool, error) { return false, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("no change wrote a file: %v", err)
	}
	boom := errors.New("boom")
	if err := Update(dir, "t1", func(*Session) (bool, error) { return true, boom }); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an error wrote a file: %v", err)
	}
	if err := Update(dir, "///", func(*Session) (bool, error) { return true, nil }); err == nil {
		t.Fatal("no error for a bad id")
	}
}

func TestSetTranscript(t *testing.T) {
	var s Session
	if !s.SetTranscript("s1", "/x/a.jsonl") || s.TranscriptPath != "/x/a.jsonl" || s.SessionID != "s1" {
		t.Fatalf("first set: %+v", s)
	}
	if s.SetTranscript("s1", "/x/a.jsonl") {
		t.Error("the same values changed it")
	}
	if !s.SetTranscript("s2", "/x/a.jsonl") || s.SessionID != "s2" {
		t.Errorf("a new id: %+v", s)
	}
	if s.SetTranscript("s3", "") || s.TranscriptPath != "/x/a.jsonl" || s.SessionID != "s2" {
		t.Errorf("an empty path changed it: %+v", s)
	}
}
