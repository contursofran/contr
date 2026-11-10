package orchestrate

// The OrchestratePill part of the spec sheet (D179, D234): two terminal-sized panels. The first
// has the pill of an orchestrating session with three workers, 10px from the right side; the second has the Orchestrate button with its popover open and a goal typed.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// The size of the part: the spec images are 1000px wide, with the sheet pad around the part.
const (
	sheetW      = 1000 - 2*apptest.SheetPad
	sheetStateH = 60
	sheetOpenH  = 190
	sheetH      = sheetStateH + theme.Space12 + sheetOpenH
)

// sheetPanel draws a panel that stands for the terminal, with the pill of p over it.
func sheetPanel(c *ui.Context, h float32, p *Pill) {
	pal := theme.Current()
	ui.Box(c).Width(sheetW).Height(h).Background(pal.Stage).Border(1, pal.PanelLine).
		Radius(theme.Radius12).Children(func() { p.View(c) })
}

func sheetPill(c *ui.Context) {
	orchestrating := snapWith(true, 3, 0)
	idle := snapWith(false, 0, 0)
	state := &Pill{Snapshot: func() agentstate.Snapshot { return orchestrating }, Selected: func() string { return orch }}
	button := &Pill{Snapshot: func() agentstate.Snapshot { return idle }, Selected: func() string { return orch },
		Send: func(string, string) error { return nil }, open: true, target: orch, goal: "ship #619, the orchestrate pill"}
	ui.Column(c).Gap(theme.Space12).Children(func() {
		sheetPanel(c, sheetStateH, state)
		sheetPanel(c, sheetOpenH, button)
	})
}

// TestSpecSheet draws the parts of package orchestrate (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "OrchestratePill", W: sheetW, H: sheetH, View: sheetPill},
	}, []string{"OrchestratePill"})
}
