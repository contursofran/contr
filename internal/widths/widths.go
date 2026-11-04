// Package widths keeps the widths of the sidebar and the panel that the
// owner set by dragging (D111). It is one small JSON file in the user config
// folder, beside the projects store.
package widths

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Widths{}, nil
	}
	if err != nil {
		return Widths{}, fmt.Errorf("widths: read %s: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return Widths{}, fmt.Errorf("widths: parse %s: %w", path, err)
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("widths: make %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".widths-*")
	if err != nil {
		return fmt.Errorf("widths: temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("widths: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("widths: close: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("widths: rename: %w", err)
	}
	return nil
}
