package kit

import (
	"strconv"

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

// RowInsetX is the side margin of a sidebar row (ProjectRow, ServerRow,
// AgentRow). The sidebar panel has no padding of its own, so this margin is
// the panel padding of docs/spec/Panel.md (10px): with the row padding
// (10px) the content sits 20px from the panel side (D231). The hover fill
// stays inside it.
const RowInsetX = theme.Space10

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
	// Hint is the key chip on the status glyph while ⌘ is held (KeyHint). Empty: none.
	Hint string
	// Items is the number of open items of an orchestrator: questions, to-dos
	// and blocked menus of it and its workers. Above 0 a `blocked` badge with
	// the number shows at the right of line 1, or a dot on the glyph in an icon
	// column (D240). Arrival is its last arrival, for the spring and glow.
	Items   int
	Arrival Arrival
	// NeedsYou draws a worker that needs the owner (D249): `blocked-fill` with
	// a `blocked-line` ring, the title in ink-100, and line 2 says why:
	// "needs you · <Reason>", the first words in `blocked`, then the age.
	// Reason is the block kind (permission, question, auth); empty: "needs
	// you" alone.
	NeedsYou bool
	Reason   string
}

// NeedsYouText is the start of line 2 of a row that needs the owner.
const NeedsYouText = "needs you"

// needLine is the text of line 2 of a needs-you row in blocked: "needs you ·"
// before a reason, "needs you" with none.
func needLine(reason string) string {
	if reason == "" {
		return NeedsYouText
	}
	return NeedsYouText + " ·"
}

// Sizes of the item badge (D240).
const (
	badgeHeight = 16
	badgePadX   = 5
	// compactDot is the dot on the glyph of an orchestrator with items in an
	// icon column.
	compactDot = 6
)

// LabelBadge names the item badge; LabelBadgeDot the dot of an icon column.
const (
	LabelBadge    = "items for you"
	LabelBadgeDot = "items for you dot"
)

// ItemsText is the part of the row label for open items: "1 item for you",
// "2 items for you"; empty for none.
func ItemsText(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 item for you"
	}
	return strconv.Itoa(n) + " items for you"
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
	// The key chip sits on the status glyph (KeyHint), inside the column.
	col := ui.Column(c).FillWidth().Shrink(0)
	col.Children(func() {
		var glyph ui.Element
		defer func() { KeyHint(c, o.Hint, col, glyph) }()
		if o.Compact {
			glyph = compactAgentButton(c, o)
			return
		}
		if o.Orchestrator {
			glyph = agentRowButton(c, o, RowInsetX, RowInsetX+OrchestratorIndent)
			return
		}
		ui.Row(c).Shrink(0).MarginStart(RowInsetX+OrchestratorIndent+RailIndent).MarginEnd(RowInsetX).
			PaddingStart(RailPad).BorderWidth(0, 0, 0, 1).BorderColor(theme.Current().Rail).Children(func() {
			glyph = agentRowButton(c, o, 0, 0)
		})
	})
}

// agentRowButton builds the button of a two-line row and returns its status glyph.
func agentRowButton(c *ui.Context, o AgentRowOpts, marginEnd, marginStart float32) ui.Element {
	p := theme.Current()
	titleInk, metaInk := p.Ink60, p.Ink42
	if o.Selected {
		titleInk, metaInk = p.Ink100, p.Ink62
	}
	if o.NeedsYou {
		titleInk = p.Ink100
	}
	padY := float32(WorkerPadY)
	if o.Orchestrator {
		padY = OrchestratorPadY
	}
	row := ui.ButtonBase(c).Label(o.Label).Shrink(0).AlignItems(ui.Center).Grow(1).MinWidth(0).
		Gap(theme.Space10).Padding(padY, theme.Space10).MarginStart(marginStart).MarginEnd(marginEnd).Radius(theme.Radius8).
		TextColor(titleInk).Cursor(ui.CursorPointer).Selected(o.Selected)
	// ButtonBase paints a selected button in the accent color; the explicit
	// background sets the look of the spec (D249): a row that needs the owner
	// keeps its `blocked` fill, also when selected; a selected row has the
	// `selected` fill and ring of the selected tab.
	switch {
	case o.NeedsYou:
		row = row.Background(p.BlockedFill).InsetShadow(0, 0, 0, 1, p.BlockedLine)
	case o.Selected:
		row = InsetRing(row.Background(p.Selected), p.ShadowSelected)
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
	var glyph ui.Element
	row.Children(func() {
		glyph = StatusGlyph(c, GlyphOpts{State: o.State, Claude: o.Orchestrator, Grey: o.Grey})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			title := func() ui.Element {
				return ui.Text(c, o.Title).Label(o.Label + " title").SingleLine().MinWidth(0).
					Font(theme.StyleBody.Family()).FontSize(theme.StyleBody.Size).FixedLineHeight(theme.StyleBody.Line).
					FontWeight(theme.StyleBody.Weight).TextColor(titleInk)
			}
			if o.Items > 0 {
				ui.Row(c).FillWidth().Gap(theme.Space8).AlignItems(ui.Center).MinWidth(0).Children(func() {
					title().Grow(1)
					itemBadge(c, o.Items, o.Arrival)
				})
			} else {
				title()
			}
			ui.Row(c).FillWidth().Gap(theme.Space10).MinWidth(0).Children(func() {
				meta := func(s string) ui.Element {
					return ui.Text(c, s).SingleLine().
						Font(theme.StyleMeta.Family()).FontSize(theme.StyleMeta.Size).FixedLineHeight(theme.StyleMeta.Line).TextColor(metaInk)
				}
				if o.NeedsYou {
					// The reason takes the place of the name, model and CI.
					meta(needLine(o.Reason)).TextColor(p.Blocked)
					if o.Reason != "" {
						meta(o.Reason)
					}
					ui.Spacer(c)
					if o.Age != "" {
						meta(o.Age)
					}
					return
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
	return glyph
}

// InsetRing draws the inset layers of a token shadow, as MyGo's inset shadow:
// the ring of `shadow-selected`.
func InsetRing(el ui.Element, sh theme.Shadow) ui.Element {
	for _, l := range sh {
		if l.Inset {
			el = el.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
	return el
}

// compactAgentButton is an agent in an icon column: the status glyph alone.
// Only the fill shows the selection, as the glyph keeps the color of its state;
// a worker that needs the owner has the `blocked` fill and ring (D249).
// It returns the glyph.
func compactAgentButton(c *ui.Context, o AgentRowOpts) ui.Element {
	p := theme.Current()
	row := ui.ButtonBase(c).Label(o.Label).Height(theme.SizeRowHeight).Shrink(0).Center().
		MarginX(theme.Space6).Radius(theme.Radius8).Cursor(ui.CursorPointer).Selected(o.Selected)
	switch {
	case o.NeedsYou:
		row = row.Background(p.BlockedFill).InsetShadow(0, 0, 0, 1, p.BlockedLine)
	case o.Selected:
		row = InsetRing(row.Background(p.Selected), p.ShadowSelected)
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
	var g ui.Element
	row.Children(func() {
		g = StatusGlyph(c, GlyphOpts{State: o.State, Claude: o.Orchestrator, Grey: o.Grey})
		if o.Items > 0 {
			// The badge has no room here: a still `blocked` dot on the glyph.
			g.Children(func() {
				ui.Box(c).Label(LabelBadgeDot).Absolute().Top(-2).Right(-2).Size(compactDot, compactDot).
					Radius(compactDot / 2).Background(theme.Current().Blocked)
			})
		}
	})
	return g
}

// itemBadge is the `blocked` badge with the number of open items (D240). On
// an arrival it springs to about 1.25 and glows for 2 s; then it is still.
func itemBadge(c *ui.Context, n int, a Arrival) {
	p := theme.Current()
	b := ui.Row(c).Label(LabelBadge).Height(badgeHeight).MinWidth(badgeHeight).Padding(0, badgePadX).Shrink(0).
		Radius(badgeHeight / 2).Background(p.Blocked).Justify(ui.Center).AlignItems(ui.Center)
	look := BadgeAttention(c, b, a)
	if look.Scale != 1 {
		b = b.Scale(look.Scale, look.Scale).TransformOrigin(0.5, 0.5)
	}
	if look.Glow > 0 {
		b = b.Shadow(0, 0, 12, 3, p.Blocked.Alpha(0.55*look.Glow))
	}
	b.Children(func() {
		ui.Text(c, strconv.Itoa(n)).NoWrap().Font(theme.StyleBadge.Family()).FontSize(theme.StyleBadge.Size).
			FixedLineHeight(theme.StyleBadge.Line).FontWeight(theme.StyleBadge.Weight).TextColor(p.Ground)
	})
}
