package app

// The look of the terminal panel (docs/spec/Terminal.md): its colors by theme,
// its padding and its lines. The overlay layer above it is kit.TerminalOverlay.

import (
	gterm "github.com/egoist/mygo/plugins/terminal"

	"github.com/contursofran/contr/internal/app/theme"
)

// terminalTheme returns the plugin's colors for a palette: terminal-text over
// the panel, the cursor in ink-88, and the 16 ANSI colors of the theme.
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
		// and ink-88 for the selected one, so no amber or orange shows.
		Search:             theme.Over(p.Press, bg),
		SearchText:         p.TerminalForeground(),
		SearchSelected:     p.Ink88,
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
