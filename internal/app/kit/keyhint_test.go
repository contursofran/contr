package kit

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

func TestTheKeyChipSitsOnTheButtonIconAndMovesNothing(t *testing.T) {
	o := IconButtonOpts{Icon: "search", Label: "Search", Key: "⌘F", OnClick: func() {}}
	plain := ui.NewTester(buttonView(o), 120, 80)
	before, _ := plain.Find("Search")
	o.Hint = "F"
	tt := ui.NewTester(buttonView(o), 120, 80)
	r, ok := tt.Find("Search")
	if !ok || r != before {
		t.Fatalf("button %v with the chip, %v without", r, before)
	}
	if !slices.Contains(tt.Texts(), "F") {
		t.Fatalf("no chip: %q", tt.Texts())
	}
	chip := apptest.ChipOn(t, tt, KeyHintLabel("F"), "Search", 120, 80)
	// The icon is centered in the button: so is the chip.
	apptest.Centered(t, "chip", chip, "the button", r)
	if chip.H != SizeKeyHint || chip.W < SizeKeyHint {
		t.Errorf("chip %vx%v", chip.W, chip.H)
	}
	// The chip takes no click: a click on it goes to the button under it.
	clicks := 0
	o.OnClick = func() { clicks++ }
	tt = ui.NewTester(buttonView(o), 120, 80)
	tt.Frame()
	chip = apptest.Box(t, tt, KeyHintLabel("F"))
	tt.ClickAt(chip.X+chip.W/2, chip.Y+chip.H/2)
	if clicks != 1 {
		t.Errorf("a click on the chip: %d clicks, want 1", clicks)
	}
}

func TestAWideChipStaysInsideTheButtonAtTheWindowEdge(t *testing.T) {
	// The files-panel button sits at the window's right edge; its chip "⇧E" is wider than the
	// icon and must not leave the button or the window.
	o := IconButtonOpts{Icon: "sidebar-right", Label: "Hide files", Hint: "⇧E", OnClick: func() {}}
	view := func(c *ui.Context) {
		theme.Apply(c)
		ui.Row(c).Fill().Justify(ui.End).Background(theme.Current().Ground).Children(func() {
			IconButton(c, o)
		})
	}
	tt := ui.NewTester(view, 200, 40)
	tt.Frame()
	apptest.ChipOn(t, tt, KeyHintLabel("⇧E"), "Hide files", 200, 40)
}

func TestNoChipWithoutAHintOrOnADisabledButton(t *testing.T) {
	for _, o := range []IconButtonOpts{
		{Icon: "search", Label: "Search"},
		{Icon: "search", Label: "Search", Hint: "F", Disabled: true},
	} {
		tt := ui.NewTester(buttonView(o), 120, 80)
		if _, ok := tt.Find(KeyHintLabel("F")); ok {
			t.Errorf("%+v: a chip shows", o)
		}
	}
}

func TestTheAgentRowChipSitsOnTheStatusGlyph(t *testing.T) {
	for _, c := range []struct {
		name string
		o    AgentRowOpts
	}{
		{"orchestrator", AgentRowOpts{Orchestrator: true}},
		{"worker", AgentRowOpts{}},
		{"icon column", AgentRowOpts{Compact: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := c.o
			o.Label, o.Title, o.Name, o.Age, o.State, o.Hint = "row", "Agent rows on ui.List", "b-241", "6m", GlyphWorking, "⌥↓"
			width := float32(310)
			if o.Compact {
				width = 48
			}
			tt := ui.NewTester(func(ctx *ui.Context) {
				theme.Apply(ctx)
				ui.Column(ctx).Fill().Background(theme.Current().Ground).Children(func() {
					AgentRow(ctx, o)
				})
			}, int(width), 120)
			chip := apptest.ChipOn(t, tt, KeyHintLabel("⌥↓"), "row", width, 120)
			glyph := apptest.Box(t, tt, GlyphWorking.Label())
			apptest.Centered(t, "chip", chip, "the glyph", glyph)
		})
	}
}

func TestAFlatRowChipSitsOnItsLeadIcon(t *testing.T) {
	for _, compact := range []bool{false, true} {
		width := float32(260)
		if compact {
			width = 48
		}
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c)
			ui.Column(c).Fill().Background(theme.Current().Ground).Children(func() {
				Row(c, RowOpts{Flat: true, Label: "alpha", Text: "alpha", Meta: "main", Compact: compact, Hint: "⌥→",
					Lead: func() ui.Element { return ui.Box(c).Label("lead").Size(15, 15).Shrink(0) }})
			})
		}, int(width), 60)
		chip := apptest.ChipOn(t, tt, KeyHintLabel("⌥→"), "alpha", width, 60)
		apptest.Centered(t, "chip", chip, "the lead icon", apptest.Box(t, tt, "lead"))
	}
}

func TestKeyHintOffset(t *testing.T) {
	box := ui.Rect{X: 100, Y: 0, W: 120, H: 30}
	for _, c := range []struct {
		name   string
		box    ui.Rect
		icon   ui.Rect
		w      float32
		winW   float32
		dx, dy float32
	}{
		{"on the icon", box, ui.Rect{X: 150, Y: 8, W: 14, H: 14}, 18, 800, -3, 0},
		{"no last frame: on the box", ui.Rect{}, ui.Rect{X: 150, Y: 8, W: 14, H: 14}, 18, 800, 0, 0},
		{"no icon: on the box", box, ui.Rect{}, 18, 800, 0, 0},
		// The icon at the left end of the box: a wide chip moves in to the box's left edge.
		{"left edge", box, ui.Rect{X: 102, Y: 8, W: 6, H: 14}, 30, 800, -45, 0},
		// The icon at the right end: the chip moves in to the box's right edge.
		{"right edge", box, ui.Rect{X: 214, Y: 8, W: 6, H: 14}, 30, 800, 45, 0},
		// A box past the window's right edge: the window wins.
		{"window edge", box, ui.Rect{X: 214, Y: 8, W: 6, H: 14}, 30, 200, 25, 0},
		// A box narrower than the chip: centered on it.
		{"narrow box", ui.Rect{X: 100, Y: 0, W: 20, H: 30}, ui.Rect{X: 100, Y: 8, W: 6, H: 14}, 30, 800, 0, 0},
	} {
		dx, dy := keyHintOffset(c.box, c.icon, c.w, SizeKeyHint, c.winW, 600)
		if dx != c.dx || dy != c.dy {
			t.Errorf("%s: move %v,%v, want %v,%v", c.name, dx, dy, c.dx, c.dy)
		}
	}
}
