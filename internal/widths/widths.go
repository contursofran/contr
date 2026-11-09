// Package widths keeps the widths of the sidebar and the panel that the
// owner set by dragging (D111). It is one small JSON file in the user config
// folder, beside the projects store.
package widths

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/contursofran/contr/internal/jsonfile"
)

const version = 1

// Widths holds the two widths, in DIPs. A zero value means "not set".
type Widths struct {
	Sidebar float32 `json:"sidebar"`
	Panel   float32 `json:"panel"`
}

type file struct {
	Version int `json:"version"`
	Widths
}

// Path returns the file: the user config folder, contr/widths.json.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("widths: config folder: %w", err)
	}
	return filepath.Join(dir, "contr", "widths.json"), nil
}

// Load reads the widths at path. A missing file gives zero widths and no
// error. A bad file gives zero widths and an error; the file stays as it is
// until the next Save.
func Load(path string) (Widths, error) {
	var f file
	found, err := jsonfile.Load(path, &f)
	if err != nil {
		return Widths{}, fmt.Errorf("widths: %w", err)
	}
	if !found {
		return Widths{}, nil
	}
	if f.Version != version {
		return Widths{}, fmt.Errorf("widths: %s has version %d, want %d", path, f.Version, version)
	}
	if f.Sidebar < 0 || f.Panel < 0 {
		return Widths{}, fmt.Errorf("widths: %s has a negative width", path)
	}
	return f.Widths, nil
}

// Save writes the widths to path through a temp file in the same folder and
// a rename, so a failed write leaves the old file whole.
func Save(path string, w Widths) error {
	data, err := json.MarshalIndent(file{Version: version, Widths: w}, "", "  ")
	if err != nil {
		return fmt.Errorf("widths: encode: %w", err)
	}
	if err := jsonfile.Write(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("widths: save: %w", err)
	}
	return nil
}
