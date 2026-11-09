// Package session keeps the todo list and the artifacts of one Claude Code
// agent, in one JSON file per hc terminal (D195). The kit's hooks write it;
// the app reads it. The file is keyed by HC_TERMINAL_ID under the state dir,
// as the context files of D144.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Dir is the folder of the session files, under the state dir.
const Dir = "session"

// Schema is the file format version.
const Schema = 1

// MaxArtifacts is the most artifacts a file keeps.
const MaxArtifacts = 20

// Todo status values.
const (
	Pending    = "pending"
	InProgress = "in_progress"
	Completed  = "completed"
)

// YouPrefix starts the text of a todo that came from the owner's prompt.
const YouPrefix = "[you] "

// Todo is one item of the agent's todo list.
type Todo struct {
	// ID is the task id of the task tools; empty for TodoWrite items.
	ID     string `json:"id,omitempty"`
	Text   string `json:"text"`
	Active string `json:"active"`
	Status string `json:"status"`
	You    bool   `json:"you"`
}

// Artifact is one artifact the agent published, edited or read.
type Artifact struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	UpdatedAt uint64 `json:"updated_at"`
}

// Session is the file of one terminal.
type Session struct {
	Schema    uint32     `json:"schema"`
	UpdatedAt uint64     `json:"updated_at"`
	Todos     []Todo     `json:"todos"`
	Artifacts []Artifact `json:"artifacts"`
}

// NewTodo makes a todo from the text, stripping the YouPrefix into You.
func NewTodo(text, active, status string) Todo {
	t := Todo{Text: text, Active: active, Status: status}
	if s, ok := strings.CutPrefix(text, YouPrefix); ok {
		t.Text, t.You = s, true
	}
	if s, ok := strings.CutPrefix(active, YouPrefix); ok {
		t.Active, t.You = s, true
	}
	return t
}

// AddArtifact puts a at the top, drops an older entry with the same slug and
// keeps at most MaxArtifacts. Empty title or URL keep the old values.
func (s *Session) AddArtifact(a Artifact) {
	out := []Artifact{a}
	for _, old := range s.Artifacts {
		if old.Slug == a.Slug {
			if out[0].Title == "" {
				out[0].Title = old.Title
			}
			if out[0].URL == "" {
				out[0].URL = old.URL
			}
			continue
		}
		out = append(out, old)
	}
	if len(out) > MaxArtifacts {
		out = out[:MaxArtifacts]
	}
	s.Artifacts = out
}

// Path is the file of terminal id, or "" when the id has no safe characters.
func Path(stateDir, id string) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '_'
	}, id)
	if stateDir == "" || strings.Trim(name, "_") == "" {
		return ""
	}
	return filepath.Join(stateDir, Dir, name+".json")
}

// Read returns the session of terminal id. A missing file gives an empty
// session and no error. It blocks: run it on a goroutine in the app.
func Read(stateDir, id string) (*Session, error) {
	path := Path(stateDir, id)
	if path == "" {
		return nil, errors.New("no terminal id or state dir")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Session{Schema: Schema}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("session file %s: %w", filepath.Base(path), err)
	}
	if s.Schema != Schema {
		return nil, fmt.Errorf("session file %s: schema %d, want %d", filepath.Base(path), s.Schema, Schema)
	}
	return &s, nil
}

// Write saves s for terminal id with a temp file and a rename, so a reader
// never sees half a file. Writers that read the file first use Update.
func Write(stateDir, id string, s *Session) error {
	path := Path(stateDir, id)
	if path == "" {
		return errors.New("no terminal id or state dir")
	}
	return write(path, s)
}

// Update reads the session of terminal id, runs f on it and writes it when f
// returns true. An exclusive lock on a file next to the session file holds
// over the read, f and the write, so two hooks that run at the same time do
// not lose changes. A bad file gives f an empty session, so the write
// replaces it. Read needs no lock: the write is a temp file and a rename.
func Update(stateDir, id string, f func(*Session) (bool, error)) error {
	path := Path(stateDir, id)
	if path == "" {
		return errors.New("no terminal id or state dir")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("session lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck // closing the file unlocks it too

	s, err := Read(stateDir, id)
	if err != nil {
		s = &Session{Schema: Schema}
	}
	changed, err := f(s)
	if err != nil || !changed {
		return err
	}
	return write(path, s)
}

func write(path string, s *Session) error {
	s.Schema = Schema
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
