package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Sizes of the AgentRow from docs/spec/AgentRow.md.
const (
	// RailIndent is how far the kids rail sits from the row of the orchestrator.
	RailIndent = 17
	// RailPad is the space between the rail line and a worker row.
	RailPad = 9
	// OrchestratorIndent is how far an orchestrator row sits in from its
	// project row; its workers sit RailIndent further in.
	OrchestratorIndent = 16
	// OrchestratorPadY and WorkerPadY are the vertical padding of the rows.
	OrchestratorPadY = 7
	WorkerPadY       = 5
)

// AgentRowOpts describes an AgentRow.
type AgentRowOpts struct {
	// Label is the screen-reader label: "<title>, <state>, <issue>, <model>".
	Label string
	// Title is line 1.
	Title string
	// Name, Model and Age are the items of line 2 ("b-241  sonnet  6m"). An
	// empty item is left out; the age stays at the right.
	Name, Model, Age string
	// CI adds "CI" to line 2 after the model: the worker waits for checks.
	CI    bool
	State GlyphState
	// Orchestrator draws the Claude glyph and the larger padding; a worker
	// draws a braille glyph and sits on the rail.
	Orchestrator bool
	// Grey draws the Claude glyph in grey: a Claude session that does not
	// orchestrate (D191).
	Grey     bool
	Selected bool
	OnClick  func()
	// Menu fills the context menu. Nil: no menu.
	Menu func(m *ui.Menu)
	// Compact draws the glyph alone, centered, with Tip as its tooltip, in an
	// icon column (D128). The label, click and menu stay.
	Compact bool
	Tip     string
}

// AgentRowHeight is the height of an AgentRow: two lines and the padding.
func AgentRowHeight(orchestrator bool) float32 {
	pad := float32(WorkerPadY)
	if orchestrator {
		pad = OrchestratorPadY
	}
	return 2*pad + theme.StyleBody.Line + theme.StyleMeta.Line
}

// AgentRow is the two-line sidebar row of one agent
// (docs/spec/AgentRow.md). A worker row sits on the thin rail.
func AgentRow(c *ui.Context, o AgentRowOpts) {
	// The column takes the width; the margins sit inside it, so they count in
	// the row's width (D40) and the age at the right is not cut off.
	ui.Column(c).FillWidth().Shrink(0).Children(func() {
		if o.Compact {
			compactAgentButton(c, o)
			return
		}
		if o.Orchestrator {
			agentRowButton(c, o, theme.Space6, theme.Space6+OrchestratorIndent)
			return
		}
		ui.Row(c).Shrink(0).MarginStart(theme.Space6+OrchestratorIndent+RailIndent).MarginEnd(theme.Space6).
			PaddingStart(RailPad).BorderWidth(0, 0, 0, 1).BorderColor(theme.Current().Rail).Children(func() {
			agentRowButton(c, o, 0, 0)
		})
	})
}

func agentRowButton(c *ui.Context, o AgentRowOpts, marginEnd, marginStart float32) {
	p := theme.Current()
	titleInk, metaInk := p.Ink60, p.Ink42
	if o.Selected {
		titleInk, metaInk = p.Ink100, p.Ink62
	}
	padY := float32(WorkerPadY)
	if o.Orchestrator {
		padY = OrchestratorPadY
	}
	row := ui.ButtonBase(c).Label(o.Label).Shrink(0).AlignItems(ui.Center).Grow(1).MinWidth(0).
		Gap(theme.Space10).Padding(padY, theme.Space10).MarginStart(marginStart).MarginEnd(marginEnd).Radius(theme.Radius8).
		TextColor(titleInk).Cursor(ui.CursorPointer).Selected(o.Selected)
	// ButtonBase paints a selected button in the accent color; the explicit
	// background keeps it flat: only the ink shows the selection.
	if row.Hovered() {
		row = row.Background(p.Hover)
	} else {
		row = row.Background(ui.Transparent)
	}
	if o.OnClick != nil {
		row = row.OnClick(o.OnClick)
	}
	if o.Menu != nil {
		row = row.ContextMenu(o.Menu)
	}
	row.Children(func() {
		StatusGlyph(c, GlyphOpts{State: o.State, Claude: o.Orchestrator, Grey: o.Grey})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, o.Title).Label(o.Label + " title").SingleLine().MinWidth(0).
				Font(theme.StyleBody.Family()).FontSize(theme.StyleBody.Size).FixedLineHeight(theme.StyleBody.Line).
				FontWeight(theme.StyleBody.Weight).TextColor(titleInk)
			ui.Row(c).FillWidth().Gap(theme.Space10).MinWidth(0).Children(func() {
				meta := func(s string) {
					ui.Text(c, s).SingleLine().
						Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size).FixedLineHeight(theme.StyleMeta.Line).TextColor(metaInk)
				}
				if o.Name != "" {
					meta(o.Name)
				}
				if o.Model != "" {
					meta(o.Model)
				}
				if o.CI {
					meta("CI")
				}
				// The spacer is always there, so line 2 keeps its height and
				// the age stays at the right, whatever else line 2 holds.
				ui.Spacer(c)
				if o.Age != "" {
					meta(o.Age)
				}
			})
		})
	})
}

// compactAgentButton is an agent in an icon column: the status glyph alone.
// Only the fill shows the selection, as the glyph keeps the color of its state.
func compactAgentButton(c *ui.Context, o AgentRowOpts) {
	p := theme.Current()
	row := ui.ButtonBase(c).Label(o.Label).Height(theme.SizeRowHeight).Shrink(0).Center().
		MarginX(theme.Space6).Radius(theme.Radius8).Cursor(ui.CursorPointer).Selected(o.Selected)
	switch {
	case o.Selected:
		row = row.Background(p.Selected)
	case row.Hovered():
		row = row.Background(p.Hover)
	default:
		row = row.Background(ui.Transparent)
	}
	if o.OnClick != nil {
		row = row.OnClick(o.OnClick)
	}
	if o.Menu != nil {
		row = row.ContextMenu(o.Menu)
	}
	if o.Tip != "" {
		row = row.Tooltip(o.Tip)
	}
	row.Children(func() { StatusGlyph(c, GlyphOpts{State: o.State, Claude: o.Orchestrator, Grey: o.Grey}) })
}
