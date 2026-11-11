package kit

import (
	"image/color"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

func TestIconLookStates(t *testing.T) {
	for _, p := range []*theme.Palette{theme.Dark, theme.Light} {
		cases := []struct {
			name                     string
			hover, pressed, disabled bool
			wantFill, wantInk        ui.Color
		}{
			{"default", false, false, false, ui.Color{}, p.Ink48},
			{"hover", true, false, false, p.HoverStrong, p.Ink100},
			{"pressed", false, true, false, ui.Color{}, p.Ink88},
			{"hover over pressed", true, true, false, p.HoverStrong, p.Ink100},
			{"disabled", false, false, true, ui.Color{}, p.Ink30},
			{"disabled with hover", true, false, true, ui.Color{}, p.Ink30},
		}
		for _, c := range cases {
			fill, ink := IconLook(p, c.hover, c.pressed, c.disabled)
			if fill != c.wantFill || ink != c.wantInk {
				t.Errorf("dark=%v %s: fill %v ink %v, want %v %v", p.Dark, c.name, fill, ink, c.wantFill, c.wantInk)
			}
		}
	}
}

func TestTipTextHasTheKey(t *testing.T) {
	o := IconButtonOpts{Label: "Split terminal", Key: "⌘D"}
	if got := TipText(o); got != "Split terminal  ⌘D" {
		t.Errorf("tip = %q", got)
	}
	if got := TipText(IconButtonOpts{Label: "Close"}); got != "Close" {
		t.Errorf("tip without key = %q", got)
	}
	o.Disabled = true
	if got := TipText(o); got != "Split terminal" {
		t.Errorf("disabled tip = %q, want no key", got)
	}
}

// view builds a row of one button on the panel color, so a pixel tells its state.
func buttonView(o IconButtonOpts) func(*ui.Context) {
	return func(c *ui.Context) {
		theme.Apply(c)
		ui.Row(c).Fill().Padding(20).Background(theme.Current().Ground).Children(func() {
			IconButton(c, o)
		})
	}
}

func px(tt *ui.Tester, x, y int) color.RGBA { return tt.Image().RGBAAt(x, y) }

func TestIconButtonSizeLabelAndClick(t *testing.T) {
	for _, small := range []bool{false, true} {
		clicks := 0
		tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "split", Label: "Split terminal", Key: "⌘D", Small: small, OnClick: func() { clicks++ }}), 120, 80)
		r, ok := tt.Find("Split terminal")
		if !ok {
			t.Fatal("button not found by its label")
		}
		want := float32(theme.SizeIconButton)
		if small {
			want = SizeIconButtonSmall
		}
		if r.W != want || r.H != want {
			t.Errorf("small=%v: box %vx%v, want %v square", small, r.W, r.H, want)
		}
		if err := tt.Click("Split terminal"); err != nil {
			t.Fatal(err)
		}
		if clicks != 1 {
			t.Errorf("clicks = %d, want 1", clicks)
		}
		// Enter presses a focused button.
		tt.Key(0, ui.KeyTab)
		tt.Key(0, ui.KeyEnter)
		if clicks != 2 {
			t.Errorf("clicks after Tab and Enter = %d, want 2", clicks)
		}
		tt.Key(0, ui.KeySpace)
		if clicks != 3 {
			t.Errorf("clicks after Space = %d, want 3", clicks)
		}
	}
}

func TestIconButtonHoverFillsAndPressedHasNone(t *testing.T) {
	on := false
	tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "plus", Label: "Add", On: &on}), 120, 80)
	r, _ := tt.Find("Add")
	corner := func() color.RGBA { return px(tt, int(r.X)+3, int(r.Y)+3) } // inside the box, off the icon
	rest := corner()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if hover := corner(); hover == rest {
		t.Errorf("hover left the box as it was: %v", hover)
	}
	tt.Move(100, 70) // away
	tt.Frame()
	if back := corner(); back != rest {
		t.Errorf("after the pointer left the box is %v, want %v", back, rest)
	}
	icon := func() color.RGBA { return px(tt, int(r.X+r.W/2), int(r.Y+r.H/2)) } // where the plus crosses
	off := icon()
	on = true
	tt.Frame()
	tt.Move(100, 70)
	tt.Frame()
	if got := icon(); got == off {
		t.Errorf("pressed icon %v looks like the default %v", got, off)
	}
	if corner() != rest {
		t.Error("a pressed toggle got a fill")
	}
}

func TestToggleReportsPressedFromTheModel(t *testing.T) {
	on, changes := false, 0
	tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "filter", Label: "Show changed files only", On: &on, OnClick: func() { changes++ }}), 120, 80)
	if err := tt.Click("Show changed files only"); err != nil {
		t.Fatal(err)
	}
	if !on || changes != 1 {
		t.Errorf("on = %v, changes = %d after a click, want true and 1", on, changes)
	}
	on = false // the model changes it back: the button follows
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	tt.Key(0, ui.KeySpace)
	if !on || changes != 2 {
		t.Errorf("on = %v, changes = %d after Space, want true and 2", on, changes)
	}
}

func TestIconButtonFocusRing(t *testing.T) {
	tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "search", Label: "Search"}), 120, 80)
	r, _ := tt.Find("Search")
	// 2px outside the box, the ring is 1px wide: its pixel is 3px left of the box.
	at := func() color.RGBA { return px(tt, int(r.X)-3, int(r.Y+r.H/2)) }
	before := at()
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if ring := at(); ring == before {
		t.Errorf("no focus ring after Tab: pixel %v", ring)
	}
	if got := px(tt, int(r.X)-2, int(r.Y+r.H/2)); got != before {
		t.Errorf("the 2px gap holds %v, want the background %v", got, before)
	}
}

func TestDisabledIconButtonHasNoHoverNoClick(t *testing.T) {
	clicks := 0
	tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "close", Label: "Close", Disabled: true, OnClick: func() { clicks++ }}), 120, 80)
	r, _ := tt.Find("Close")
	corner := func() color.RGBA { return px(tt, int(r.X)+3, int(r.Y)+3) }
	rest := corner()
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if corner() != rest {
		t.Error("a disabled button got a hover fill")
	}
	_ = tt.Click("Close")
	if clicks != 0 {
		t.Errorf("a disabled button ran its action %d times", clicks)
	}
}

func TestIconButtonWithoutIconFileStillBuilds(t *testing.T) {
	tt := ui.NewTester(buttonView(IconButtonOpts{Icon: "no-such-icon", Label: "Nothing"}), 120, 80)
	if _, ok := tt.Find("Nothing"); !ok {
		t.Error("button missing")
	}
	if icons.Line("no-such-icon") != nil {
		t.Error("test icon exists")
	}
}

func panelView(name string, width float32, scrolled bool) func(*ui.Context) {
	return func(c *ui.Context) {
		theme.Apply(c)
		ui.Row(c).Fill().Background(theme.Current().Ground).Children(func() {
			ui.Column(c).Width(width).Height(200).Children(func() {
				Panel(c, PanelOpts{Name: name,
					Head:   func() { IconButton(c, IconButtonOpts{Icon: "plus", Label: "Add"}) },
					Footer: func() { IconButton(c, IconButtonOpts{Icon: "terminal", Label: "Terminal", Small: true}) },
					Body: func() {
						for i := 0; i < 30; i++ {
							ui.Text(c, "line "+strconv.Itoa(i)).Height(18)
						}
					}})
			})
		})
	}
}

func TestPanelShowsHeadAndFooter(t *testing.T) {
	tt := ui.NewTester(panelView("contr", 240, false), 300, 240)
	for _, want := range []string{"contr", "line 0"} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
	add, ok1 := tt.Find("Add")
	term, ok2 := tt.Find("Terminal")
	if !ok1 || !ok2 {
		t.Fatal("head or footer button missing")
	}
	if term.H != SizeIconButtonSmall {
		t.Errorf("footer button is %v high, want %d", term.H, SizeIconButtonSmall)
	}
	if term.Y <= add.Y+add.H {
		t.Error("the footer is not below the head")
	}
	if _, ok := tt.Find("contr"); !ok {
		t.Error("the panel is not a group named by its head")
	}
}

func TestPanelHeadClipsALongName(t *testing.T) {
	long := strings.Repeat("a-very-long-project-name-", 12)
	tt := ui.NewTester(panelView(long, 240, false), 300, 240)
	name, ok := tt.Find(long + " name")
	if !ok {
		t.Fatal("head name not found")
	}
	add, _ := tt.Find("Add")
	if name.X+name.W > add.X {
		t.Errorf("the name (ends at %v) runs under the head button (starts at %v)", name.X+name.W, add.X)
	}
	if name.H > theme.StyleLabel.Line {
		t.Errorf("the name is %v high: it wrapped instead of clipping with an ellipsis", name.H)
	}
	panel, _ := tt.Find(long)
	if panel.W > 240 {
		t.Errorf("a long name widened the panel to %v; it must clip in 240", panel.W)
	}
}

func TestPanelContentScrollsAndHeadStays(t *testing.T) {
	tt := ui.NewTester(panelView("contr", 240, false), 300, 240)
	add, _ := tt.Find("Add")
	term, _ := tt.Find("Terminal")
	panel, _ := tt.Find("contr")
	if term.Y+term.H > panel.Y+panel.H {
		t.Errorf("the content pushed the footer out of the panel: footer ends at %v, panel at %v", term.Y+term.H, panel.Y+panel.H)
	}
	first, _ := tt.Find("line 5")
	tt.Scroll(100, 100, 0, 40)
	tt.Frame()
	add2, _ := tt.Find("Add")
	term2, _ := tt.Find("Terminal")
	if add2 != add || term2 != term {
		t.Errorf("head or footer moved with the content: %v -> %v, %v -> %v", add, add2, term, term2)
	}
	if moved, _ := tt.Find("line 5"); moved.Y >= first.Y {
		t.Errorf("the content did not scroll: line 5 at %v, then at %v", first.Y, moved.Y)
	}
}
