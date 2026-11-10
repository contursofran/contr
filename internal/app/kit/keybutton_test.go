package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// The main key button is ink-100; the other has no fill or border: its edge is the ground
// (D255). A click runs the action; the accessible name is the label.
func TestKeyButtonFillsOnlyTheMainAction(t *testing.T) {
	p := theme.Current()
	clicks := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(p.Ground).Gap(theme.Space8).Children(func() {
			KeyButton(c, "Main", "⌘O", KeyMain, func() { clicks++ })
			KeyButton(c, "Other", "⌘T", KeyPlain, nil)
			KeyButton(c, "Row", "⌘O", KeyRow, nil)
		})
	}, 200, 120)
	tt.Frame()
	for _, s := range []string{"Main", "⌘O", "Other", "⌘T", "Row"} {
		if !tt.HasText(s) {
			t.Errorf("no text %q in %q", s, tt.Texts())
		}
	}
	img := tt.Image()
	at := func(y int) [3]uint8 { c := img.RGBAAt(2, y); return [3]uint8{c.R, c.G, c.B} }
	if got, want := at(theme.KeyButtonHeight/2), [3]uint8{p.Ink100.R, p.Ink100.G, p.Ink100.B}; got != want {
		t.Errorf("main button edge %v, want ink-100 %v", got, want)
	}
	if got, want := at(theme.KeyButtonHeight+theme.Space8+theme.KeyButtonHeight/2), [3]uint8{p.Ground.R, p.Ground.G, p.Ground.B}; got != want {
		t.Errorf("other button edge %v, want the ground %v (no fill, no border)", got, want)
	}
	if got, want := at(2*(theme.KeyButtonHeight+theme.Space8)+theme.KeyButtonHeight/2), [3]uint8{p.Ground.R, p.Ground.G, p.Ground.B}; got != want {
		t.Errorf("row button edge %v, want the ground %v (no fill, no border)", got, want)
	}
	if err := tt.Click("Main"); err != nil {
		t.Fatal(err)
	}
	if clicks != 1 {
		t.Errorf("%d clicks, want 1", clicks)
	}
}

// A narrow foot shows whole plans only: the one that does not fit goes below the clipped line.
func TestPlanUsageHidesAPlanThatDoesNotFit(t *testing.T) {
	meters := []Meter{{Label: "Claude"}, {Label: "Codex"}}
	for _, tc := range []struct {
		w     float32
		codex bool
	}{{400, true}, {150, false}} {
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Row(c).Width(tc.w).Children(func() { PlanUsage(c, meters) })
		}, 420, 60)
		tt.Frame()
		row, _ := tt.Find("plan usage")
		claude, _ := tt.Find("Claude plan")
		codex, _ := tt.Find("Codex plan")
		if claude.Y != row.Y {
			t.Errorf("width %v: Claude at y %v, want on the line %v", tc.w, claude.Y, row.Y)
		}
		if shown := codex.Y == row.Y && codex.X+codex.W <= row.X+row.W; shown != tc.codex {
			t.Errorf("width %v: Codex %+v in row %+v shown %v, want %v", tc.w, codex, row, shown, tc.codex)
		}
	}
}
