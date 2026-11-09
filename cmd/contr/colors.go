package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/prefs"
)

// The colors a new terminal gets from contr (#521, D212): hc answers a
// program's color queries (OSC 10, 11, 12) with them, so Claude Code's auto
// theme sees a light terminal in Light.

func hexColor(c ui.Color) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// paletteColors are the terminal colors of a palette, as the terminal view
// shows them.
func paletteColors(p *theme.Palette) *agents.Colors {
	return &agents.Colors{
		FG:     hexColor(p.TerminalForeground()),
		BG:     hexColor(p.TerminalBackground()),
		Cursor: hexColor(p.TerminalCursor()),
	}
}

// appColors are the colors of the theme the window shows now. Call it only
// in the app, after it applied the appearance.
func appColors() *agents.Colors { return paletteColors(theme.Current()) }

// sourceColors are the colors for a stored appearance. System follows
// systemDark.
func sourceColors(src mygo.ThemeSource, systemDark func() bool) *agents.Colors {
	dark := false
	switch prefs.Normalize(src) {
	case mygo.ThemeDark:
		dark = true
	case mygo.ThemeSystem:
		dark = systemDark()
	}
	return paletteColors(theme.For(dark))
}

// macDark tells if macOS is in dark mode: `defaults` has AppleInterfaceStyle
// "Dark" then, and no value in light mode.
func macDark() bool {
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	return err == nil && strings.TrimSpace(string(out)) == "Dark"
}

// storedColors are the colors for a spawn from the command line (`contr mcp`,
// `contr agents spawn`): the stored Appearance setting, else the macOS
// appearance. A setting that cannot be read gives nil: hc keeps its default.
var storedColors = func() *agents.Colors {
	path, err := prefs.Path()
	if err != nil {
		return nil
	}
	p, err := prefs.Load(path)
	if err != nil { // Load gives the defaults with the error: System
		return nil
	}
	return sourceColors(p.Source(), macDark)
}
