package orchestrate

// The OrchestratePill part of the spec sheet (D179, D234, D253): title bar strips with a Search
// icon button after the pill. The first has the pill of an orchestrating session with three
// workers, the second the same pill compact (a narrow window), the third the Orchestrate button
// with its popover open and a goal typed.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// The size of the part: the spec images are 1000px wide, with the sheet pad around the part.
const (
	sheetW     = 1000 - 2*apptest.SheetPad
	sheetOpenH = 190
	sheetH     = 2*theme.TitleBarHeight + 2*theme.Space12 + sheetOpenH
)

// sheetBar draws a strip h high that stands for the title bar, with the pill of p at the top
// right, left of a Search button.
func sheetBar(c *ui.Context, h float32, p *Pill, compact bool) {
	ui.Column(c).Width(sheetW).Height(h).Children(func() {
		ui.Row(c).FillWidth().Height(theme.TitleBarHeight).AlignItems(ui.Center).PaddingEnd(theme.TitleBarPadEnd).
			Children(func() {
				ui.Box(c).Grow(1)
				p.View(c, compact)
				kit.IconButton(c, kit.IconButtonOpts{Icon: "search", Label: "Search", OnClick: func() {}})
			})
	})
}

func sheetPill(c *ui.Context) {
	orchestrating := snapWith(true, 3, 0)
	idle := snapWith(false, 0, 0)
	state := &Pill{Snapshot: func() agentstate.Snapshot { return orchestrating }, Selected: func() string { return orch }}
	button := &Pill{Snapshot: func() agentstate.Snapshot { return idle }, Selected: func() string { return orch },
		Send: func(string, string) error { return nil }, open: true, target: orch, goal: "ship #619, the orchestrate pill"}
	ui.Column(c).Gap(theme.Space12).Children(func() {
		sheetBar(c, theme.TitleBarHeight, state, false)
		sheetBar(c, theme.TitleBarHeight, state, true)
		sheetBar(c, sheetOpenH, button, false)
	})
}

// TestSpecSheet draws the parts of package orchestrate (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "OrchestratePill", W: sheetW, H: sheetH, View: sheetPill},
	}, []string{"OrchestratePill"})
}
