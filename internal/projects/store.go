// Package projects keeps the owner's choices about projects: their order,
// the hidden ones and the recent ones. A project is a repo group of the
// sidebar, keyed by its git common dir (D63).
package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const version = 1

// MaxRecent is the most entries that the recent list keeps (D116).
const MaxRecent = 10

// Entry is a recent project: the project key, the folder opened and when.
type Entry struct {
	Dir  string    `json:"dir"`
	Path string    `json:"path"`
	At   time.Time `json:"at"`
}

// Store holds the project order, the hidden projects and the recent ones.
// It is not safe for concurrent use: the caller guards it.
type Store struct {
	order  []string
	hidden []string
	recent []Entry
	// folders maps a project key to the folder that the owner opened for it.
	// Only these projects show in the sidebar with no hc terminal (D116).
	folders map[string]string
}

type file struct {
	Version int      `json:"version"`
	Order   []string `json:"order"`
	Hidden  []string `json:"hidden"`
	Recent  []Entry  `json:"recent"`
	// Folders is added in D116. A file without it is still version 1.
	Folders map[string]string `json:"folders,omitempty"`
}

// Path returns the store file: the user config folder, contr/projects.json.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("projects: config folder: %w", err)
	}
	return filepath.Join(dir, "contr", "projects.json"), nil
}

// Load reads the store at path. A missing file gives an empty store and no
// error. A corrupt file gives an empty store and an error; the file stays
// as it is until the next Save.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{}, nil
	}
	if err != nil {
		return &Store{}, fmt.Errorf("projects: read %s: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return &Store{}, fmt.Errorf("projects: parse %s: %w", path, err)
	}
	if f.Version != version {
		return &Store{}, fmt.Errorf("projects: %s has version %d, want %d", path, f.Version, version)
	}
	return &Store{order: f.Order, hidden: f.Hidden, recent: f.Recent, folders: f.Folders}, nil
}

// Save writes the store to path through a temp file in the same folder and a
// rename, so a failed write leaves the old file whole.
func (s *Store) Save(path string) error {
	f := file{Version: version, Order: s.order, Hidden: s.hidden, Recent: s.recent, Folders: s.folders}
	if f.Order == nil {
		f.Order = []string{}
	}
	if f.Hidden == nil {
		f.Hidden = []string{}
	}
	if f.Recent == nil {
		f.Recent = []Entry{}
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("projects: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("projects: create folder: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".projects-*.tmp")
	if err != nil {
		return fmt.Errorf("projects: temp file: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return fmt.Errorf("projects: write %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("projects: rename to %s: %w", path, err)
	}
	return nil
}

// Order returns groups in the stored order. Stored dirs that are not in
// groups are dropped. Groups the store does not know come after the known
// ones, in the order given (the tree order). Hidden groups stay: use Hidden.
func (s *Store) Order(groups []string) []string {
	out := make([]string, 0, len(groups))
	for _, d := range s.order {
		if slices.Contains(groups, d) && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	for _, g := range groups {
		if !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// SetOrder stores dirs as the order. Call it with Order's result before Move
// so that row numbers and the stored order agree.
func (s *Store) SetOrder(dirs []string) { s.order = slices.Clone(dirs) }

// Move moves the rows of the stored order before row to (len for the end),
// as ListState.Reorder asks. The rows keep their order. Rows out of range
// are ignored.
func (s *Store) Move(rows []int, to int) {
	n := len(s.order)
	moved := make([]string, 0, len(rows))
	isMoved := make([]bool, n)
	for _, r := range rows {
		if r >= 0 && r < n && !isMoved[r] {
			isMoved[r] = true
			moved = append(moved, s.order[r])
		}
	}
	if len(moved) == 0 {
		return
	}
	to = max(0, min(to, n))
	out := make([]string, 0, n)
	for i := 0; i <= n; i++ {
		if i == to {
			out = append(out, moved...)
		}
		if i < n && !isMoved[i] {
			out = append(out, s.order[i])
		}
	}
	s.order = out
}

// Clone returns a copy that shares nothing with s, so that one goroutine can
// save the copy while another changes s.
func (s *Store) Clone() *Store {
	c := *s
	c.order = slices.Clone(s.order)
	c.hidden = slices.Clone(s.hidden)
	c.recent = slices.Clone(s.recent)
	c.folders = maps.Clone(s.folders)
	return &c
}

// Hide marks dir as hidden.
func (s *Store) Hide(dir string) {
	if !s.Hidden(dir) {
		s.hidden = append(s.hidden, dir)
	}
}

// Show removes dir from the hidden projects.
func (s *Store) Show(dir string) {
	s.hidden = slices.DeleteFunc(s.hidden, func(d string) bool { return d == dir })
}

// Hidden reports whether dir is hidden.
func (s *Store) Hidden(dir string) bool { return slices.Contains(s.hidden, dir) }

// Recent returns the recent projects, newest first.
func (s *Store) Recent() []Entry { return slices.Clone(s.recent) }

// Touch records that the folder path of project dir was opened at at. An
// older entry of the same project is dropped, so the list has each project
// once, newest first. It keeps at most MaxRecent entries (D116).
func (s *Store) Touch(dir, path string, at time.Time) {
	s.recent = slices.DeleteFunc(s.recent, func(e Entry) bool { return e.Dir == dir })
	s.recent = append([]Entry{{Dir: dir, Path: path, At: at}}, s.recent...)
	if len(s.recent) > MaxRecent {
		s.recent = s.recent[:MaxRecent]
	}
}

// AddFolder adds the project dir with its folder path to the store: it goes
// to the end of the order when it is new, and it is no longer hidden (D116).
func (s *Store) AddFolder(dir, path string) {
	if !slices.Contains(s.order, dir) {
		s.order = append(s.order, dir)
	}
	if s.folders == nil {
		s.folders = map[string]string{}
	}
	s.folders[dir] = path
	s.Show(dir)
}

// Folders returns the stored projects that were added with a folder, in the
// stored order and with the hidden ones left out, as project dirs. Path gives
// the folder of one.
func (s *Store) Folders() []string {
	var out []string
	for _, d := range s.order {
		if _, ok := s.folders[d]; ok && !s.Hidden(d) {
			out = append(out, d)
		}
	}
	return out
}

// Path returns the folder that was added for project dir, or "".
func (s *Store) Path(dir string) string { return s.folders[dir] }
