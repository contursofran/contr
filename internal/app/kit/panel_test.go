package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

func TestBarePanelHasNoHeadAndTheBodyFillsIt(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			Panel(c, PanelOpts{Name: "Terminal", Bare: true, Body: func() {
				ui.Box(c).Label("body").Fill()
			}})
		})
	}, 300, 200)
	panel, ok := tt.Find("Terminal")
	if !ok {
		t.Fatalf("no panel; texts %q", tt.Texts())
	}
	if _, head := tt.Find("Terminal name"); head {
		t.Error("a bare panel draws a head")
	}
	body, ok := tt.Find("body")
	if !ok {
		t.Fatal("no body")
	}
	// Inside the 1px line, with no padding.
	if body.W != panel.W-2 || body.H != panel.H-2 {
		t.Errorf("body %+v in panel %+v, want the box less its line", body, panel)
	}
}

func TestPanelWithHeadShowsItsName(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		Panel(c, PanelOpts{Name: "Files", Body: func() { ui.Text(c, "content") }})
	}, 300, 200)
	if _, head := tt.Find("Files name"); !head || !tt.HasText("content") {
		t.Errorf("texts %q", tt.Texts())
	}
}

// The stage and a wing put their content at the same place: the wing pads 1px in place of the
// line it does not draw (D250).
func TestStageAndWingKeepTheContentInset(t *testing.T) {
	for _, bare := range []bool{true, false} {
		at := func(stage bool) (ui.Rect, ui.Rect) {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
					Panel(c, PanelOpts{Name: "P", Bare: bare, Stage: stage, Body: func() {
						ui.Box(c).Label("body").Height(40).FillWidth()
					}})
				})
			}, 300, 200)
			panel, _ := tt.Find("P")
			body, ok := tt.Find("body")
			if !ok {
				t.Fatalf("bare %v stage %v: no body", bare, stage)
			}
			return panel, body
		}
		sp, sb := at(true)
		wp, wb := at(false)
		if sp != wp || sb != wb {
			t.Errorf("bare %v: stage %+v body %+v, wing %+v body %+v; want the same boxes", bare, sp, sb, wp, wb)
		}
	}
}

// Only the stage draws a fill and a line; a wing shows the glass under it (D250).
func TestOnlyTheStageDrawsAFillAndALine(t *testing.T) {
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		p := theme.Current()
		for _, stage := range []bool{true, false} {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Fill().AlignItems(ui.Stretch).Padding(10).Background(p.Ground).Children(func() {
					Panel(c, PanelOpts{Name: "P", Bare: true, Stage: stage})
				})
			}, 200, 120)
			tt.Frame()
			img := tt.Image()
			r, _ := tt.Find("P")
			mid := apptest.RGB(img, r.X+r.W/2, r.Y+r.H/2)
			edge := apptest.RGB(img, r.X+r.W/2, r.Y)
			fill, line := theme.Over(p.Stage, p.Ground), theme.Over(p.PanelLine, theme.Over(p.Stage, p.Ground))
			if stage && (!apptest.Near(mid, fill, 2) || !apptest.Near(edge, line, 2)) {
				t.Errorf("%s stage: middle %v edge %v, want the fill %v and the line %v", tc.Name, mid, edge, fill, line)
			}
			if !stage && (!apptest.Near(mid, p.Ground, 1) || !apptest.Near(edge, p.Ground, 1)) {
				t.Errorf("%s wing: middle %v edge %v, want the ground %v", tc.Name, mid, edge, p.Ground)
			}
		}
	}
}
