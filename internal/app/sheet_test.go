package app

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
)

// sheetParts are the spec'd components of package app that the spec sheet draws (#458, D179).
var sheetParts = []apptest.SheetPart{
	{Name: "AgentRow", W: 320, H: 280, View: func(c *ui.Context) {
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Agent board polish", Model: "opus", Age: "14m", State: kit.GlyphWorking, Orchestrator: true, Selected: true})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Agent rows on ui.List", Name: "b-241", Model: "sonnet", Age: "6m", State: kit.GlyphWorking})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Checks on hc event", Name: "b-250", Model: "sonnet", Age: "2m", State: kit.GlyphNeedsYou})
		kit.AgentRow(c, kit.AgentRowOpts{Title: "Sprite atlas rebuild", Name: "b-88", Model: "deepseek", Age: "25m", CI: true, State: kit.GlyphIdle})         // waiting on CI: the idle glyph
		kit.AgentRow(c, kit.AgentRowOpts{Title: "lead", Name: "2 workers", Model: "opus", Age: "14m", State: kit.GlyphWorking, Orchestrator: true, Items: 2}) // two open items: the badge (D240)
	}},
	{Name: "StatusGlyph", W: 700, H: 60, View: func(c *ui.Context) {
		glyphs := []struct {
			o    kit.GlyphOpts
			text string
		}{
			{kit.GlyphOpts{State: kit.GlyphWorking}, "working"},
			{kit.GlyphOpts{State: kit.GlyphWorkingSlow}, "working, slow"},
			{kit.GlyphOpts{State: kit.GlyphWaiting}, "waiting"},
			{kit.GlyphOpts{State: kit.GlyphIdle}, "idle"},
			{kit.GlyphOpts{State: kit.GlyphNeedsYou}, "needs you"},
			{kit.GlyphOpts{State: kit.GlyphWorking, Claude: true}, "orchestrating"},
			{kit.GlyphOpts{State: kit.GlyphIdle, Claude: true}, "orchestrator idle"},
			{kit.GlyphOpts{State: kit.GlyphNeedsYou, Claude: true}, "orchestrator asks"},
		}
		for line := range 2 {
			ui.Row(c).Height(32).Children(func() {
				for _, g := range glyphs[line*4 : line*4+4] {
					ui.Row(c).Width(160).Gap(8).AlignItems(ui.Center).Children(func() {
						kit.StatusGlyph(c, g.o)
						ui.Text(c, g.text).TextColor(theme.Current().Ink62)
					})
				}
			})
		}
	}},
	{Name: "IconButton", W: 520, H: 40, View: func(c *ui.Context) {
		on := true
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			kit.IconButton(c, kit.IconButtonOpts{Icon: "sidebar-right", Label: "Hide files"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "search", Label: "Search"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "plus", Label: "New tab"})
			kit.IconButton(c, kit.IconButtonOpts{Icon: "sidebar-right", Label: "Files", On: &on})
			ui.Text(c, "pressed").TextColor(theme.Current().Ink50)
			kit.IconButton(c, kit.IconButtonOpts{Icon: "settings", Label: "Settings", Small: true})
			ui.Text(c, "24px footer").TextColor(theme.Current().Ink50)
			kit.IconButton(c, kit.IconButtonOpts{Icon: "close", Label: "Close", Small: true})
			ui.Text(c, "close, 16px").TextColor(theme.Current().Ink50)
		})
	}},
	{Name: "DiffSummary", W: 500, H: 30, View: func(c *ui.Context) {
		ui.Row(c).Gap(32).AlignItems(ui.Center).Children(func() {
			kit.DiffSummary(c, kit.DiffSummaryOpts{Added: 120, Removed: 126, Files: 9})
			kit.DiffSummary(c, kit.DiffSummaryOpts{Added: 31, Files: 1})
			kit.DiffSummary(c, kit.DiffSummaryOpts{})
		})
	}},
	{Name: "UsageMeter", W: 258, H: 130, View: func(c *ui.Context) {
		kit.UsageMeter(c, []kit.Meter{
			{Label: "Context", Value: "90k of 200k 45%", Percent: 45, Has: true},
			{Label: "Claude", Value: "38%", Percent: 38, Has: true},
			{Label: "Codex", Value: "12%", Percent: 12, Has: true},
		})
	}},
}

// sheetKeyHints is the KeyHints part: the title bar and agent rows as they show while ⌘ is held.
func sheetKeyHints(c *ui.Context) {
	bar := titlebar.TitleBar{Hints: true, NewTab: func() {}, Search: func() {}, Tabs: []tabmodel.Tab{
		{Address: "o", Title: "lead", Orchestrator: true, State: kit.GlyphWorking},
		{Address: "w1", Title: "b-241", State: kit.GlyphWorking, Selected: true},
		{Address: "t1", Title: "$ repo", Plain: true},
	}}
	ui.Column(c).Gap(theme.Space12).Children(func() {
		ui.Row(c).Width(640).Height(theme.TitleBarHeight).AlignItems(ui.Center).Children(func() {
			bar.Build(c, true, false, func() {})
		})
		ui.Column(c).Width(300).Children(func() {
			kit.AgentRow(c, kit.AgentRowOpts{Title: "lead", Name: "2 workers", Model: "opus", Age: "14m", State: kit.GlyphWorking, Orchestrator: true, Hint: "⌥↑"})
			kit.AgentRow(c, kit.AgentRowOpts{Title: "Agent rows on ui.List", Name: "b-241", Model: "sonnet", Age: "6m", State: kit.GlyphWorking, Selected: true})
			kit.AgentRow(c, kit.AgentRowOpts{Title: "Checks on hc event", Name: "b-250", Model: "sonnet", Age: "2m", State: kit.GlyphIdle, Hint: "⌥↓"})
		})
	})
}

// TestSpecSheet draws the parts of package app (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	parts := append(slices.Clip(sheetParts), apptest.SheetPart{Name: "KeyHints", W: 640, H: 230, View: sheetKeyHints},
		apptest.SheetPart{Name: "TerminalEmpty", W: 960, H: 620, View: sheetTerminalEmpty},
		apptest.SheetPart{Name: "TerminalEmptyNarrow", W: 480, H: 620, View: sheetTerminalEmpty},
		apptest.SheetPart{Name: "Panel", W: 560, H: 220, View: sheetPanel})
	// The sheet draws one frame: the drawing must be decoded before it.
	for deadline := time.Now().Add(5 * time.Second); handsBitmap(nil) == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
	}
	apptest.SpecSheet(t, parts, []string{"KeyHints", "TerminalEmpty", "TerminalEmptyNarrow"})
}

// sheetTerminalEmpty is the terminal panel while no terminal is open (docs/spec/Terminal.md).
func sheetTerminalEmpty(c *ui.Context) {
	f := &Frame{}
	kit.Panel(c, kit.PanelOpts{Name: LabelPanelTerminal, Bare: true, Stage: true, Body: func() {
		ui.Box(c).Fill().Center().Children(func() { f.emptyTerminal(c) })
	}})
}

// sheetPanel is a wing next to the stage, on the window glass (docs/spec/Panel.md, D250).
func sheetPanel(c *ui.Context) {
	p := theme.Current()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Gap(theme.PanelGap).Padding(theme.PanelGap).Background(p.GlassWindow).Children(func() {
		ui.Column(c).Width(220).Children(func() {
			kit.Panel(c, kit.PanelOpts{Name: "contr", Body: func() {
				ui.Text(c, "wing: no fill, no line").TextColor(p.Ink50)
			}})
		})
		kit.Panel(c, kit.PanelOpts{Name: "stage", Stage: true, Body: func() {
			ui.Text(c, "stage: fill and line").TextColor(p.Ink95)
		}})
	})
}
