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

	GlassWindow  ui.Color `token:"glass-window"`
	GlassFloat   ui.Color `token:"glass-float"`
	Panel        ui.Color `token:"panel"`
	PanelLine    ui.Color `token:"panel-line"`
	WindowLine   ui.Color `token:"window-line"`
	FloatLine    ui.Color `token:"float-line"`
	Hairline     ui.Color `token:"hairline"`
	Rail         ui.Color `token:"rail"`
	Hover        ui.Color `token:"hover"`
	HoverStrong  ui.Color `token:"hover-strong"`
	Press        ui.Color `token:"press"`
	Selected     ui.Color `token:"selected"`
	SelectedLine ui.Color `token:"selected-line"`
	InputLine    ui.Color `token:"input-line"`
	MeterTrack   ui.Color `token:"meter-track"`
	MeterFill    ui.Color `token:"meter-fill"`
	Tick         ui.Color `token:"tick"`

	Ink100 ui.Color `token:"ink-100"`
	Ink95  ui.Color `token:"ink-95"`
	Ink90  ui.Color `token:"ink-90"`
	Ink85  ui.Color `token:"ink-85"`
	Ink62  ui.Color `token:"ink-62"`
	Ink60  ui.Color `token:"ink-60"`
	Ink50  ui.Color `token:"ink-50"`
	Ink45  ui.Color `token:"ink-45"`
	Ink42  ui.Color `token:"ink-42"`
	Ink40  ui.Color `token:"ink-40"`
	Ink38  ui.Color `token:"ink-38"`
	Ink35  ui.Color `token:"ink-35"`
	Ink32  ui.Color `token:"ink-32"`
	Ink30  ui.Color `token:"ink-30"`
	Ink26  ui.Color `token:"ink-26"`
	Ink24  ui.Color `token:"ink-24"`
	Ink22  ui.Color `token:"ink-22"`

	TerminalText ui.Color `token:"terminal-text"`
	Blocked      ui.Color `token:"blocked"`
	Claude       ui.Color `token:"claude"`
	FocusRing    ui.Color `token:"focus-ring"`
	Lights       ui.Color `token:"lights"`

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

func white(a float32) ui.Color { return ui.RGBA(255, 255, 255, a) }
func black(a float32) ui.Color { return ui.RGBA(0, 0, 0, a) }

// Dark is the palette of the dark theme.
var Dark = &Palette{
	Dark:         true,
	Ground:       ui.Hex("#0a0b0d"),
	GlowSlate:    ui.Hex("#2b3647"),
	GlowUmber:    ui.Hex("#3a3127"),
	GlowGraphite: ui.Hex("#23262e"),

	GlassWindow:  ui.RGBA(20, 21, 24, 0.58),
	GlassFloat:   ui.RGBA(34, 35, 40, 0.72),
	Panel:        white(0.03),
	PanelLine:    white(0.065),
	WindowLine:   white(0.09),
	FloatLine:    white(0.075),
	Hairline:     white(0.06),
	Rail:         white(0.07),
	Hover:        white(0.045),
	HoverStrong:  white(0.06),
	Press:        white(0.08),
	Selected:     white(0.085),
	SelectedLine: white(0.06),
	InputLine:    white(0.1),
	MeterTrack:   white(0.07),
	MeterFill:    white(0.55),
	Tick:         white(0.14),

	Ink100: ui.Hex("#f4f4f5"),
	Ink95:  ui.Hex("#e9e9ea"),
	Ink90:  white(0.9),
	Ink85:  white(0.85),
	Ink62:  white(0.62),
	Ink60:  white(0.6),
	Ink50:  white(0.5),
	Ink45:  white(0.45),
	Ink42:  white(0.42),
	Ink40:  white(0.4),
	Ink38:  white(0.38),
	Ink35:  white(0.35),
	Ink32:  white(0.32),
	Ink30:  white(0.3),
	Ink26:  white(0.26),
	Ink24:  white(0.24),
	Ink22:  white(0.22),

	TerminalText: white(0.82),
	Blocked:      ui.Hex("#f07178"),
	Claude:       ui.Hex("#d97757"),
	FocusRing:    white(0.5),
	Lights:       white(0.14),

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
	GlowSlate:    ui.Hex("#c6d1e3"),
	GlowUmber:    ui.Hex("#e8d9c4"),
	GlowGraphite: ui.Hex("#d5d8de"),

	GlassWindow:  ui.RGBA(246, 246, 248, 0.62),
	GlassFloat:   ui.RGBA(252, 252, 253, 0.8),
	Panel:        white(0.5),
	PanelLine:    black(0.06),
	WindowLine:   black(0.1),
	FloatLine:    black(0.08),
	Hairline:     black(0.07),
	Rail:         black(0.08),
	Hover:        black(0.04),
	HoverStrong:  black(0.055),
	Press:        black(0.08),
	Selected:     black(0.065),
	SelectedLine: black(0.06),
	InputLine:    black(0.13),
	MeterTrack:   black(0.08),
	MeterFill:    black(0.5),
	Tick:         black(0.16),

	Ink100: ui.Hex("#1b1b1b"),
	Ink95:  ui.Hex("#232324"),
	Ink90:  black(0.853),
	Ink85:  black(0.82),
	Ink62:  black(0.671),
	Ink60:  black(0.657),
	Ink50:  black(0.581),
	Ink45:  black(0.538),
	Ink42:  black(0.511),
	Ink40:  black(0.492),
	Ink38:  black(0.472),
	Ink35:  black(0.442),
	Ink32:  black(0.409),
	Ink30:  black(0.386),
	Ink26:  black(0.339),
	Ink24:  black(0.314),
	Ink22:  black(0.288),

	TerminalText: black(0.801),
	Blocked:      ui.Hex("#c93a48"),
	Claude:       ui.Hex("#c15f3c"),
	FocusRing:    black(0.45),
	Lights:       black(0.14),

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
