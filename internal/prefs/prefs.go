// Package prefs keeps the settings that the owner chooses in the Settings
// modal (D119). It is one small JSON file in the user config folder, beside
// the projects store and the widths file.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

const version = 1

// Prefs holds the settings. The zero value is the default.
type Prefs struct {
	// Appearance is the theme source: System, Dark or Light. Source reads it.
	Appearance mygo.ThemeSource `json:"appearance"`
	// Tint holds the transparency the owner tuned for each theme (D162).
	Tint Tints `json:"tint"`
}

// Tint is the alpha, 0 to 1, of the window ground (`glass-window`) and of the
// panels (`panel`) for one theme. A nil value is the token default.
type Tint struct {
	Window *float64 `json:"window,omitempty"`
	Panel  *float64 `json:"panel,omitempty"`
}

// Tints holds the Tint of each theme.
type Tints struct {
	Dark  Tint `json:"dark"`
	Light Tint `json:"light"`
}

// clamp returns v limited to 0..1; NaN and nil give nil.
func clamp(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) {
		return nil
	}
	c := math.Min(1, math.Max(0, *v))
	return &c
}

func (t Tint) clean() Tint { return Tint{Window: clamp(t.Window), Panel: clamp(t.Panel)} }

func (t Tints) clean() Tints { return Tints{Dark: t.Dark.clean(), Light: t.Light.clean()} }

type file struct {
	Version int `json:"version"`
	Prefs
}

// Source returns the theme source. An empty or unknown value is System.
func (p Prefs) Source() mygo.ThemeSource { return Normalize(p.Appearance) }

// Normalize returns src when it is System, Dark or Light, and System otherwise.
func Normalize(src mygo.ThemeSource) mygo.ThemeSource {
	switch src {
	case mygo.ThemeDark, mygo.ThemeLight:
		return src
	}
	return mygo.ThemeSystem
}

// Path returns the file: the user config folder, contr/settings.json.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("prefs: config folder: %w", err)
	}
	return filepath.Join(dir, "contr", "settings.json"), nil
}

// Load reads the settings at path. A missing file gives the defaults and no
// error. A bad file gives the defaults and an error; the file stays as it is
// until the next Save. A file without tint values gives the token defaults; values
// outside 0 to 1 are limited to it. An unknown appearance value gives System and no error.
func Load(path string) (Prefs, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("prefs: read %s: %w", path, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return Prefs{}, fmt.Errorf("prefs: parse %s: %w", path, err)
	}
	if f.Version != version {
		return Prefs{}, fmt.Errorf("prefs: %s has version %d, want %d", path, f.Version, version)
	}
	f.Appearance = Normalize(f.Appearance)
	f.Tint = f.Tint.clean()
	return f.Prefs, nil
}

// Save writes the settings to path through a temp file in the same folder and
// a rename, so a failed write leaves the old file whole.
func Save(path string, p Prefs) error {
	p.Appearance = Normalize(p.Appearance)
	p.Tint = p.Tint.clean()
	data, err := json.MarshalIndent(file{Version: version, Prefs: p}, "", "  ")
	if err != nil {
		return fmt.Errorf("prefs: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("prefs: make %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return fmt.Errorf("prefs: temp file: %w", err)
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(name)
		return fmt.Errorf("prefs: write %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("prefs: rename to %s: %w", path, err)
	}
	return nil
}
