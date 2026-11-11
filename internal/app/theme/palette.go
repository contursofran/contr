package theme

import "github.com/egoist/mygo/ui"

// Palette holds the value of every color and shadow token of
// docs/spec/tokens.md for one theme (D119). Each field has the token name in
// its `token` tag. Views read the palette of the current theme (Current) and
// never write a color of their own.
type Palette struct {
	Dark bool // the palette is for a dark appearance

	Ground       ui.Color `token:"ground"`
	GlowSlate    ui.Color `token:"glow-slate"`    // canvas only: never paint it in the app
	GlowUmber    ui.Color `token:"glow-umber"`    // canvas only: never paint it in the app
	GlowGraphite ui.Color `token:"glow-graphite"` // canvas only: never paint it in the app

	GlassWindow   ui.Color `token:"glass-window"`
	GlassFloat    ui.Color `token:"glass-float"`
	FloatSolid    ui.Color `token:"float-solid"`
	Scrim         ui.Color `token:"scrim"`
	Stage         ui.Color `token:"stage"` // the terminal panel only (D250)
	PanelLine     ui.Color `token:"panel-line"`
	WindowLine    ui.Color `token:"window-line"`
	FloatLine     ui.Color `token:"float-line"`
	Hairline      ui.Color `token:"hairline"`
	Rail          ui.Color `token:"rail"`
	Hover         ui.Color `token:"hover"`
	HoverStrong   ui.Color `token:"hover-strong"`
	Press         ui.Color `token:"press"`
	Selected      ui.Color `token:"selected"`
	SelectedQuiet ui.Color `token:"selected-quiet"`
	SelectedLine  ui.Color `token:"selected-line"`
	InputLine     ui.Color `token:"input-line"`
	MeterTrack    ui.Color `token:"meter-track"`
	MeterFill     ui.Color `token:"meter-fill"`
	Tick          ui.Color `token:"tick"`

	// The five text inks (D268): each means one thing. ink-30 is for marks only, never text.
	Ink100 ui.Color `token:"ink-100"` // chosen, needs the owner
	Ink88  ui.Color `token:"ink-88"`  // text, live rows
	Ink64  ui.Color `token:"ink-64"`  // rows at rest
	Ink48  ui.Color `token:"ink-48"`  // meta, labels: at least 4.5:1 where text sits
	Ink30  ui.Color `token:"ink-30"`  // marks only: icons, rails, dots

	TerminalText ui.Color `token:"terminal-text"`
	Blocked      ui.Color `token:"blocked"`
	BlockedText  ui.Color `token:"blocked-text"`
	Claude       ui.Color `token:"claude"`
	FocusRing    ui.Color `token:"focus-ring"`
	Lights       ui.Color `token:"lights"`

	// The content palette (D261): colors of what Claude writes in the chat, never of the chrome.
	ContentDone    ui.Color `token:"content-done"`
	ContentRunning ui.Color `token:"content-running"`
	ContentWaiting ui.Color `token:"content-waiting"`
	ContentFailed  ui.Color `token:"content-failed"`
	ContentInfo    ui.Color `token:"content-info"`
	CodeKeyword    ui.Color `token:"code-keyword"`
	CodeFunction   ui.Color `token:"code-function"`
	CodeString     ui.Color `token:"code-string"`
	CodeNumber     ui.Color `token:"code-number"`
	CodeComment    ui.Color `token:"code-comment"`

	// ANSI is the 16-color palette of programs in the terminal, in ANSIName
	// order (docs/spec/tokens.md, ANSI). A program draws these colors itself,
	// so they are not chrome tokens.
	ANSI [16]ui.Color

	ShadowWindowTop Shadow `token:"shadow-window-top"`
	ShadowSelected  Shadow `token:"shadow-selected"`
	ShadowFloat     Shadow `token:"shadow-float"`
	ShadowNotch     Shadow `token:"shadow-notch"`
}

// ShadowLayer is one layer of a CSS-like shadow: inset or drop, offset, blur,
// spread and color.
type ShadowLayer struct {
	Inset            bool
	X, Y, Blur, Wide float32
	Color            ui.Color
}

// Shadow is a stack of layers, first on top.
type Shadow []ShadowLayer

// ANSIName is the token name of each ANSI color: the eight normal colors,
// then the eight bright ones.
var ANSIName = [16]string{
	"ansi-black", "ansi-red", "ansi-green", "ansi-yellow", "ansi-blue", "ansi-magenta", "ansi-cyan", "ansi-white",
	"ansi-bright-black", "ansi-bright-red", "ansi-bright-green", "ansi-bright-yellow",
	"ansi-bright-blue", "ansi-bright-magenta", "ansi-bright-cyan", "ansi-bright-white",
}

// ansi turns 16 0xRRGGBB values into colors.
func ansi(v [16]uint32) (out [16]ui.Color) {
	for i, rgb := range v {
		out[i] = Color(rgb)
	}
	return out
}

func white(a float32) ui.Color { return ui.RGBA(255, 255, 255, a) }
func black(a float32) ui.Color { return ui.RGBA(0, 0, 0, a) }

// Dark is the palette of the dark theme.
var Dark = &Palette{
	Dark:         true,
	Ground:       ui.Hex("#0a0b0d"),
	GlowSlate:    ui.Hex("#3a5a8c"),
	GlowUmber:    ui.Hex("#6b4a2a"),
	GlowGraphite: ui.Hex("#2c3240"),

	GlassWindow:   ui.RGBA(20, 21, 24, 0.7),
	GlassFloat:    ui.RGBA(34, 35, 40, 0.72),
	FloatSolid:    ui.Hex("#222328"),
	Scrim:         ui.RGBA(8, 8, 8, 0.72),
	Stage:         ui.RGBA(12, 13, 15, 0.9),
	PanelLine:     white(0.08),
	WindowLine:    white(0.09),
	FloatLine:     white(0.075),
	Hairline:      white(0.06),
	Rail:          white(0.07),
	Hover:         white(0.045),
	HoverStrong:   white(0.06),
	Press:         white(0.08),
	Selected:      white(0.085),
	SelectedQuiet: white(0.07),
	SelectedLine:  white(0.06),
	InputLine:     white(0.1),
	MeterTrack:    white(0.07),
	MeterFill:     white(0.55),
	Tick:          white(0.14),

	Ink100: ui.Hex("#f4f4f5"),
	Ink88:  white(0.88),
	Ink64:  white(0.64),
	Ink48:  white(0.48),
	Ink30:  white(0.3),

	TerminalText: white(0.82),
	Blocked:      ui.Hex("#f07178"),
	BlockedText:  ui.Hex("#f07178"),
	Claude:       ui.Hex("#d97757"),
	FocusRing:    white(0.5),
	Lights:       white(0.14),

	ContentDone:    ui.Hex("#7fb88a"),
	ContentRunning: ui.Hex("#7aa7e6"),
	ContentWaiting: ui.Hex("#d9b36a"),
	ContentFailed:  ui.Hex("#f07178"),
	ContentInfo:    white(0.64),
	CodeKeyword:    ui.Hex("#b79ce6"),
	CodeFunction:   ui.Hex("#86b4ee"),
	CodeString:     ui.Hex("#9cc58f"),
	CodeNumber:     ui.Hex("#e0b36a"),
	CodeComment:    white(0.48),

	// The palette the v0.2 terminal had (TerminalPalette).
	ANSI: ansi([16]uint32{
		0x1E1E1E, 0xE06C6C, 0x8CC07A, 0xE0C070, 0x6CA8E8, 0xC080D8, 0x5CC0C8, 0xD0D0D0,
		0x5C5C5C, 0xFF8080, 0xA8E090, 0xFFD890, 0x90C4FF, 0xDCA0F0, 0x80DCE4, 0xFFFFFF,
	}),

	ShadowWindowTop: Shadow{{Inset: true, Y: 1, Color: white(0.05)}},
	ShadowSelected:  Shadow{{Inset: true, Wide: 1, Color: white(0.06)}},
	ShadowFloat:     Shadow{{Y: 12, Blur: 32, Color: black(0.28)}},
	ShadowNotch: Shadow{
		{Inset: true, Y: -1, Color: white(0.03)},
		{Y: 12, Blur: 32, Color: black(0.28)},
	},
}

// Light is the palette of the light theme. Its inks are black at the opacity
// that gives the contrast the dark inks have.
var Light = &Palette{
	Ground:       ui.Hex("#e4e6ea"),
	GlowSlate:    ui.Hex("#b2c3e0"),
	GlowUmber:    ui.Hex("#ebcfa9"),
	GlowGraphite: ui.Hex("#cdd2db"),

	GlassWindow:   ui.RGBA(246, 246, 248, 0.6),
	GlassFloat:    ui.RGBA(252, 252, 253, 0.8),
	FloatSolid:    ui.Hex("#fcfcfd"),
	Scrim:         ui.RGBA(20, 20, 20, 0.18),
	Stage:         white(0.86),
	PanelLine:     black(0.06),
	WindowLine:    black(0.1),
	FloatLine:     black(0.08),
	Hairline:      black(0.07),
	Rail:          black(0.08),
	Hover:         black(0.04),
	HoverStrong:   black(0.055),
	Press:         black(0.08),
	Selected:      black(0.065),
	SelectedQuiet: black(0.08),
	SelectedLine:  black(0.06),
	InputLine:     black(0.13),
	MeterTrack:    black(0.08),
	MeterFill:     black(0.5),
	Tick:          black(0.16),

	Ink100: ui.Hex("#141414"),
	Ink88:  black(0.86),
	Ink64:  black(0.7),
	Ink48:  black(0.57),
	Ink30:  black(0.38),

	TerminalText: black(0.815),
	Blocked:      ui.Hex("#c73947"),
	BlockedText:  ui.Hex("#b1333f"),
	Claude:       ui.Hex("#c15f3c"),
	FocusRing:    black(0.45),
	Lights:       black(0.14),

	ContentDone:    ui.Hex("#1e763b"),
	ContentRunning: ui.Hex("#1f5fbf"),
	ContentWaiting: ui.Hex("#8a5a00"),
	ContentFailed:  ui.Hex("#b1333f"),
	ContentInfo:    black(0.7),
	CodeKeyword:    ui.Hex("#8250df"),
	CodeFunction:   ui.Hex("#1f5fbf"),
	CodeString:     ui.Hex("#1e763b"),
	CodeNumber:     ui.Hex("#8a5a00"),
	CodeComment:    black(0.586),

	// Ghostty's GitHub Light Default, tuned for the Light panel (D129): the
	// dim yellow and the white colors are darker, so each of the 16 colors
	// but black has at least 4.5:1 on the panel.
	ANSI: ansi([16]uint32{
		0x24292F, 0xCF222E, 0x116329, 0x7D4E00, 0x0969DA, 0x8250DF, 0x1A7980, 0x656D76,
		0x57606A, 0xA40E26, 0x1A7F37, 0x633C01, 0x0550AE, 0x6639BA, 0x0E6B78, 0x424A53,
	}),

	ShadowWindowTop: Shadow{{Inset: true, Y: 1, Color: white(0.7)}},
	ShadowSelected:  Shadow{{Inset: true, Wide: 1, Color: black(0.06)}},
	ShadowFloat:     Shadow{{Y: 12, Blur: 32, Color: black(0.12)}},
	ShadowNotch: Shadow{
		{Inset: true, Y: -1, Color: white(0.6)},
		{Y: 12, Blur: 32, Color: black(0.12)},
	},
}

// For returns the palette of a dark or light appearance.
func For(dark bool) *Palette {
	if dark {
		return Dark
	}
	return Light
}
