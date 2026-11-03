// Package owner keeps what waits for the owner: questions that only the
// owner can answer, and to-dos that the owner must do or should know. The
// items are in one JSON file that every coordinator writes and the UI reads.
// A lock file and an atomic rename keep concurrent writers safe.
package owner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"
)

// Question is a decision that waits for the owner.
type Question struct {
	ID          string    `json:"id"` // D1, D2, …
	Project     string    `json:"project"`
	Coordinator string    `json:"coordinator,omitempty"` // hc terminal that asked; the UI sends the answer there
	From        string    `json:"from,omitempty"`        // the worker, issue or PR it is about
	Title       string    `json:"title,omitempty"`
	Question    string    `json:"question"`
	Options     []string  `json:"options,omitempty"`
	Recommended *int      `json:"recommended,omitempty"` // index in Options
	Asked       time.Time `json:"asked"`
	Answer      string    `json:"answer,omitempty"`
	AnsweredBy  string    `json:"answered_by,omitempty"` // owner or coordinator
	Answered    time.Time `json:"answered,omitzero"`
}

// Todo is something the owner must do ("do") or should know ("know").
type Todo struct {
	ID          string    `json:"id"` // T1, T2, …
	Project     string    `json:"project"`
	Coordinator string    `json:"coordinator,omitempty"`
	From        string    `json:"from,omitempty"`
	Kind        string    `json:"kind"` // do or know
	Text        string    `json:"text"`
	Pinned      time.Time `json:"pinned"`
	Done        time.Time `json:"done,omitzero"`
}

// State is the whole file.
type State struct {
	Questions []Question `json:"questions"`
	Todos     []Todo     `json:"todos"`
	NextQ     int        `json:"next_question"`
	NextT     int        `json:"next_todo"`
}

// Store is the file at Path. Now is the clock; nil means time.Now.
type Store struct {
	Path string
	Now  func() time.Time
}

// DefaultPath is owner.json in contr's folder of the user config folder.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "contr", "owner.json"), nil
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Read returns the state. A missing file is an empty state.
func (s *Store) Read() (State, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{NextQ: 1, NextT: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("owner: %s: %w", s.Path, err)
	}
	if st.NextQ < 1 {
		st.NextQ = 1
	}
	if st.NextT < 1 {
		st.NextT = 1
	}
	return st, nil
}

// update runs f on the state under the lock and writes the result.
func (s *Store) update(f func(*State) error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("owner: lock: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck // closing the file unlocks it too

	st, err := s.Read()
	if err != nil {
		return err
	}
	if err := f(&st); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Ask adds a question and returns it with its id.
func (s *Store) Ask(q Question) (Question, error) {
	if q.Question == "" {
		return q, errors.New("owner: a question needs text")
	}
	if q.Recommended != nil && (*q.Recommended < 0 || *q.Recommended >= len(q.Options)) {
		return q, fmt.Errorf("owner: recommended %d is not an option index", *q.Recommended)
	}
	err := s.update(func(st *State) error {
		q.ID = "D" + strconv.Itoa(st.NextQ)
		st.NextQ++
		q.Asked = s.now()
		q.Answer, q.AnsweredBy, q.Answered = "", "", time.Time{}
		st.Questions = append(st.Questions, q)
		return nil
	})
	return q, err
}

// Pin adds a to-do and returns it with its id. Kind defaults to "do".
func (s *Store) Pin(t Todo) (Todo, error) {
	if t.Text == "" {
		return t, errors.New("owner: a to-do needs text")
	}
	if t.Kind == "" {
		t.Kind = "do"
	}
	if t.Kind != "do" && t.Kind != "know" {
		return t, fmt.Errorf("owner: kind %q is not do or know", t.Kind)
	}
	err := s.update(func(st *State) error {
		t.ID = "T" + strconv.Itoa(st.NextT)
		st.NextT++
		t.Pinned = s.now()
		t.Done = time.Time{}
		st.Todos = append(st.Todos, t)
		return nil
	})
	return t, err
}

// Decide records the answer of open questions. by is "owner" or
// "coordinator". An id that is not an open question is an error, and then
// nothing changes.
func (s *Store) Decide(ids []string, answer, by string) error {
	if len(ids) == 0 {
		return errors.New("owner: no question ids")
	}
	if answer == "" {
		return errors.New("owner: an answer needs text")
	}
	if by != "owner" && by != "coordinator" {
		return fmt.Errorf("owner: by %q is not owner or coordinator", by)
	}
	return s.update(func(st *State) error {
		for _, id := range ids {
			if !slices.ContainsFunc(st.Questions, func(q Question) bool { return q.ID == id && q.Answer == "" }) {
				return fmt.Errorf("owner: no open question %s", id)
			}
		}
		for i := range st.Questions {
			if slices.Contains(ids, st.Questions[i].ID) {
				st.Questions[i].Answer, st.Questions[i].AnsweredBy, st.Questions[i].Answered = answer, by, s.now()
			}
		}
		return nil
	})
}

// Unpin marks open to-dos done. An id that is not an open to-do is an error,
// and then nothing changes.
func (s *Store) Unpin(ids []string) error {
	if len(ids) == 0 {
		return errors.New("owner: no to-do ids")
	}
	return s.update(func(st *State) error {
		for _, id := range ids {
			if !slices.ContainsFunc(st.Todos, func(t Todo) bool { return t.ID == id && t.Done.IsZero() }) {
				return fmt.Errorf("owner: no open to-do %s", id)
			}
		}
		for i := range st.Todos {
			if slices.Contains(ids, st.Todos[i].ID) {
				st.Todos[i].Done = s.now()
			}
		}
		return nil
	})
}

// Open returns the open questions and to-dos of project; an empty project
// means all projects.
func (st State) Open(project string) ([]Question, []Todo) {
	var qs []Question
	var ts []Todo
	for _, q := range st.Questions {
		if q.Answer == "" && (project == "" || q.Project == project) {
			qs = append(qs, q)
		}
	}
	for _, t := range st.Todos {
		if t.Done.IsZero() && (project == "" || t.Project == project) {
			ts = append(ts, t)
		}
	}
	return qs, ts
}
