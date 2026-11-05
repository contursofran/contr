package app

// The look of the terminal panel (docs/spec/Terminal.md): its colors by theme,
// its padding, its lines, and the overlay layer above it.

import (
	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// LabelTerminalOverlay names the layer that holds the overlays of the terminal.
const LabelTerminalOverlay = "terminal overlay"

// terminalTheme returns the plugin's colors for a palette: terminal-text over
// the panel, the cursor in ink-95, and the 16 ANSI colors of the theme.
func terminalTheme(p *theme.Palette) *gterm.Theme {
	bg := p.TerminalBackground()
	return &gterm.Theme{
		Foreground:    p.TerminalForeground(),
		Background:    bg,
		Cursor:        p.TerminalCursor(),
		CursorText:    bg,
		Selection:     p.TerminalSelection(),
		SelectionText: p.TerminalForeground(),
		// Search matches use greys only (docs/spec/Terminal.md): a press tint for each match
		// and ink-95 for the selected one, so no amber or orange shows.
		Search:             theme.Over(p.Press, bg),
		SearchText:         p.TerminalForeground(),
		SearchSelected:     p.Ink95,
		SearchSelectedText: bg,
		Palette:            p.ANSI,
	}
}

// terminalColors returns the light and dark colors of the plugin. The plugin
// picks one by the appearance of the UI theme (theme.Apply), so a theme change
// changes the colors of the running terminal and never reconnects it.
func terminalColors() (light, dark *gterm.Theme) {
	return terminalTheme(theme.Light), terminalTheme(theme.Dark)
}

// TerminalOverlay is the layer above the terminal for the questions notch and
// the outline (#346, #348). It builds nothing while it has no layer, so it
// takes no pointer or key from the terminal. A layer draws only small floating
// elements, placed with Absolute; it must not cover the grid. A key goes to a
// layer only while an element of the layer has the focus. UI thread only.
type TerminalOverlay struct {
	layers []*overlayLayer
}

type overlayLayer struct {
	name  string
	build func(c *ui.Context)
}

// Add puts a layer above the terminal and returns the function that removes
// it. Layers build in the order they were added, the last on top.
func (o *TerminalOverlay) Add(name string, build func(c *ui.Context)) (remove func()) {
	l := &overlayLayer{name: name, build: build}
	o.layers = append(o.layers, l)
	return func() {
		for i, x := range o.layers {
			if x == l {
				o.layers = append(o.layers[:i], o.layers[i+1:]...)
				return
			}
		}
	}
}

// Empty tells that no layer is set.
func (o *TerminalOverlay) Empty() bool { return len(o.layers) == 0 }

// View builds the layers inside the terminal area.
func (o *TerminalOverlay) View(c *ui.Context) {
	for _, l := range o.layers {
		l.build(c)
	}
}
