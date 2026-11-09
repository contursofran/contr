// Package updated keeps the update that contr installed last, so the first start of that
// version can show What's new (D209). It is one small JSON file in contr's state folder.
package updated

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/contursofran/contr/internal/jsonfile"
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
	var n Note
	if _, err := jsonfile.Load(path, &n); err != nil {
		return Note{}, fmt.Errorf("updated: %w", err)
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
	if err := jsonfile.Write(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("updated: save: %w", err)
	}
	return nil
}
