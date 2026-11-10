package theme

import "github.com/egoist/mygo/ui"

// Over returns fg painted over bg as one opaque color. A terminal draws solid
// colors only, so the tokens that are translucent (stage, terminal-text) are
// flattened over what is under them.
func Over(fg, bg ui.Color) ui.Color {
	a := float32(fg.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(float32(f)*a + float32(b)*(1-a) + 0.5) }
	return ui.RGB(mix(fg.R, bg.R), mix(fg.G, bg.G), mix(fg.B, bg.B))
}

// StageBase returns the color the stage (the terminal panel, D250) shows over
// the ground: the glass of the window, then the stage fill. The real window
// shows the desktop through the glass; the ground stands in for it, as in
// docs/spec/tokens.md. It is the background the contrast ratios of the
// terminal text are measured on.
func (p *Palette) StageBase() ui.Color {
	return Over(p.Stage, Over(p.GlassWindow, p.Ground))
}

// TerminalBackground is the terminal's default background for the plugin: the
// stage color. The plugin does not draw it (the terminal is transparent), but
// it reads it as the color text sits on.
func (p *Palette) TerminalBackground() ui.Color { return p.StageBase() }

// TerminalForeground is terminal-text flattened over the stage.
func (p *Palette) TerminalForeground() ui.Color { return Over(p.TerminalText, p.StageBase()) }

// TerminalCursor is the cursor block: ink-95 (docs/spec/tokens.md).
func (p *Palette) TerminalCursor() ui.Color { return p.Ink95 }

// TerminalSelection is the fill behind selected text: the selected token,
// flattened over the stage.
func (p *Palette) TerminalSelection() ui.Color { return Over(p.Selected, p.StageBase()) }
