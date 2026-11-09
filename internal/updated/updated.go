// Package updated keeps the update that contr installed last, so the first start of that
// version can show What's new (D209). It is one small JSON file in contr's state folder.
package updated

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName is the name of the file in the state folder.
const FileName = "updated.json"

// Note is the installed update.
type Note struct {
	// Version is the version that was installed.
	Version string `json:"version"`
	// From is the version that ran when it was installed.
	From string `json:"from"`
	// Notes are the release notes in Markdown.
	Notes string `json:"notes"`
	// InstalledAt is when the install ended.
	InstalledAt time.Time `json:"installed_at"`
	// Shown is true once the owner closed the What's new panel of this version.
	Shown bool `json:"shown"`
}

// Due tells whether the What's new panel shows for the running version: the note is of that
// version and did not show yet.
func (n Note) Due(running string) bool {
	return n.Version != "" && n.Version == running && !n.Shown
}

// Path returns the file in the state folder dir, or "" when dir is "".
func Path(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, FileName)
}

// Load reads the note at path. A missing file gives a zero note and no error. A bad file gives a
// zero note and an error.
func Load(path string) (Note, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Note{}, nil
	}
	if err != nil {
		return Note{}, fmt.Errorf("updated: read %s: %w", path, err)
	}
	var n Note
	if err := json.Unmarshal(data, &n); err != nil {
		return Note{}, fmt.Errorf("updated: parse %s: %w", path, err)
	}
	return n, nil
}

// Save writes the note to path through a temp file in the same folder and a rename, so a failed
// write leaves the old file whole.
func Save(path string, n Note) error {
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return fmt.Errorf("updated: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("updated: make %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".updated-*")
	if err != nil {
		return fmt.Errorf("updated: temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("updated: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("updated: close: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("updated: rename: %w", err)
	}
	return nil
}
