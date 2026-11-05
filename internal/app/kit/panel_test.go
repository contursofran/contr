package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"
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
