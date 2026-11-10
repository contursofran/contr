// Package orchestrate is the orchestrate pill (docs/spec/OrchestratePill.md, D234): a glass pill at
// the top right of the terminal of a Claude session. It says that the session orchestrates, with
// the count of its live workers, or it is a button that sends /contr:orchestrate with a goal.
package orchestrate

// The data is the agent state (agentstate): the view reads only the copy that Snapshot returns.
// The send runs off the UI thread through view.Env and lands back on it.

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
)

// Labels and texts of the pill.
const (
	// LabelLayer names the pill layer of the terminal overlay.
	LabelLayer = "orchestrate pill"
	// TextOrchestrate is the text of the button, and of the send button of the popover.
	TextOrchestrate = "Orchestrate"
	// LabelButton is the screen-reader label of the button: it opens the popover.
	LabelButton = "Orchestrate. Open"
	// LabelPopover names the popover.
	LabelPopover = "Orchestrate this session"
	// LabelGoal is the label and the placeholder of the goal field.
	LabelGoal = "Goal (optional)"
	// TextCancel closes the popover.
	TextCancel = "Cancel"
	// Command is the slash command that starts orchestration (kit, D191).
	Command = "/contr:orchestrate"
)

// Sizes of the spec that tokens.md leaves to the spec text.
const (
	pillHeight = 26
	// pillRight is the space between the pill and the right side: the outline inset.
	pillRight = 10
	// pillRightClear keeps the pill clear of the outline column when the column reaches the pill's
	// rows: inset 10px, 20px wide, and 10px more.
	pillRightClear = 40
	// notchReserve is the width kept free at the center while the notch shows, and notchClear the
	// space on each side of it.
	notchReserve = 240
	notchClear   = theme.Space8
	// wrapGap is the gap of the wrapping row: the second line, under the notch, is at
	// notch-height + space-8 from the top.
	wrapGap      = theme.SizeNotchHeight
	popoverWidth = 320
	popoverGap   = theme.Space6
)

// spacerMargin makes the spacer, half of the row (the row is right narrower than the terminal),
// plus the gap, end at the right of the notch reserve and its clear space.
func spacerMargin(right float32) float32 {
	return notchReserve/2 + notchClear + right/2 - wrapGap
}

// pillBottom is the bottom of the pill's rows: the first row, or the row under the notch while the
// notch shows.
func pillBottom(notch bool) float32 {
	if notch {
		return wrapGap + theme.Space8 + pillHeight
	}
	return theme.Space8 + pillHeight
}

// PillText is the text of the pill of an orchestrating session with n live workers:
// "Orchestrating", "Orchestrating · 1 worker", "Orchestrating · 3 workers".
func PillText(n int) string {
	switch {
	case n <= 0:
		return "Orchestrating"
	case n == 1:
		return "Orchestrating · 1 worker"
	}
	return "Orchestrating · " + strconv.Itoa(n) + " workers"
}

// CommandText is the text that the popover sends for goal: the command, a space and the trimmed
// goal, or the command alone for an empty goal.
func CommandText(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return Command
	}
	return Command + " " + goal
}

// Pill is the orchestrate pill. Its methods run on the UI thread.
type Pill struct {
	// Snapshot reads the agent state; nil shows no pill.
	Snapshot func() agentstate.Snapshot
	// Selected returns the hc address of the selected terminal, or "".
	Selected func() string
	// NotchShown tells whether the questions notch shows; may be nil.
	NotchShown func() bool
	// OutlineTop returns the top of the outline column in the terminal, and false while no column
	// shows; may be nil.
	OutlineTop func() (float32, bool)
	// Send types text and Enter into the terminal at an address. It blocks: it runs off the UI
	// thread. Nil makes the button do nothing.
	Send func(addr, text string) error
	// Env runs the send and shows its error.
	Env view.Env
	// Lead returns what shows left of the pill in its row, as the Chat | Terminal switch (D246),
	// or nil for nothing. It shows for a terminal that has no pill too. May be nil.
	Lead func() func(c *ui.Context)

	open   bool
	goal   string
	busy   bool
	target string // the session the popover is open for
}

// session returns the selected Claude session while it runs, and the snapshot.
func (p *Pill) session() (agents.Worker, agentstate.Snapshot, bool) {
	if p.Snapshot == nil || p.Selected == nil {
		return agents.Worker{}, agentstate.Snapshot{}, false
	}
	addr := p.Selected()
	if addr == "" {
		return agents.Worker{}, agentstate.Snapshot{}, false
	}
	snap := p.Snapshot()
	for _, w := range snap.Orchestrators {
		if w.Address == addr && w.Live {
			return w, snap, true
		}
	}
	return agents.Worker{}, snap, false
}

// IsOpen tells whether the popover shows.
func (p *Pill) IsOpen() bool { return p.open }

// View builds the pill, or nothing for a terminal that is not a running Claude session. It is
// absolute, at the top right of the terminal; the row around it lets the pointer through.
func (p *Pill) View(c *ui.Context) {
	w, snap, ok := p.session()
	orchestrating := ok && snap.Orchestrating(w)
	if !ok || orchestrating || (p.open && p.target != w.Address) {
		p.open = false // the popover goes with the button, or when the selection moves
	}
	var lead func(c *ui.Context)
	if p.Lead != nil {
		lead = p.Lead()
	}
	if !ok && lead == nil {
		return
	}
	notch := p.NotchShown != nil && p.NotchShown()
	right := p.right(notch)
	ui.Row(c).Absolute().Top(theme.Space8).Left(0).Right(right).Wrap().Justify(ui.End).Gap(wrapGap).
		PassThrough().Children(func() {
		if notch {
			// The spacer ends at the right of the notch reserve: a pill that does not fit after
			// it wraps to the second line, under the notch.
			ui.Box(c).Height(0).WidthPercent(50).Shrink(0).Margin(0, spacerMargin(right), 0, 0)
		}
		// The lead and the pill wrap as one, space-8 apart.
		ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			if lead != nil {
				lead(c)
			}
			switch {
			case !ok:
			case orchestrating:
				p.stateView(c, PillText(snap.LiveWorkersOf(w.Address)))
			default:
				p.buttonView(c, w.Address)
			}
		})
	})
}

// right is the space between the pill and the right side: the outline inset, or clear of the
// outline column when the column's top is above the bottom of the pill's rows.
func (p *Pill) right(notch bool) float32 {
	if p.OutlineTop == nil {
		return pillRight
	}
	if top, ok := p.OutlineTop(); ok && top < pillBottom(notch) {
		return pillRightClear
	}
	return pillRight
}

// glass styles the inside of the pill: glass-float with float-line all round.
func glass(c *ui.Context, pal *theme.Palette) ui.Element {
	return kit.GlassFloat(ui.Row(c).Height(pillHeight).AlignItems(ui.Center).Gap(theme.Space8).
		Padding(0, theme.Space12, 0, theme.Space10).Radius(theme.Radius14), pal).Border(1, pal.FloatLine)
}

// withShadow gives e the layers of shadow s.
func withShadow(e ui.Element, s theme.Shadow) ui.Element {
	for _, l := range s {
		if l.Inset {
			e = e.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		} else {
			e = e.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
	return e
}

// mascot is the still Claude mascot of the pill: claude while it orchestrates, ink-45 on the
// button (D191). Waiting is a still state at full opacity; the pill's label names it.
func mascot(c *ui.Context, grey bool) {
	kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWaiting, Claude: true, Grey: grey, Small: true}).AccessHidden(true)
}

// stateView builds the pill of an orchestrating session. It takes no click.
func (p *Pill) stateView(c *ui.Context, text string) {
	pal := theme.Current()
	b := ui.Box(c).Label(text).Height(pillHeight).Shrink(0).Radius(theme.Radius14)
	withShadow(b, pal.ShadowNotch).Children(func() {
		glass(c, pal).Children(func() {
			mascot(c, false)
			kit.Styled(ui.Text(c, text), theme.StyleLabel).NoWrap().TextColor(pal.Ink85)
		})
	})
}

// buttonView builds the Orchestrate button and its popover.
func (p *Pill) buttonView(c *ui.Context, addr string) {
	pal := theme.Current()
	b := ui.ButtonBase(c).Label(LabelButton).Height(pillHeight).Shrink(0).Radius(theme.Radius14).
		Cursor(ui.CursorPointer).FocusRing(false).OnClick(func() {
		p.open = !p.open
		p.target = addr
	})
	b = withShadow(b, pal.ShadowNotch)
	focus := b.FocusVisible()
	ink := pal.Ink85
	if b.Hovered() || p.open {
		ink = pal.Ink100
	}
	b = b.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		if focus {
			// 1px ring, 2px outside the box (docs/spec/README.md, Focus).
			const gap, width = 2, 1
			pt.Stroke(ui.Rect{X: r.X - gap - width, Y: r.Y - gap - width, W: r.W + 2*(gap+width), H: r.H + 2*(gap+width)},
				pal.FocusRing, theme.Radius14+gap+width, width)
		}
	})
	b.Children(func() {
		glass(c, pal).Children(func() {
			mascot(c, true)
			kit.Styled(ui.Text(c, TextOrchestrate), theme.StyleLabel).NoWrap().TextColor(ink)
		})
	})
	ui.PopoverBase(c, b, &p.open, func(panel ui.Element) { p.popover(c, b, panel) })
}

// popover styles the panel and builds the goal field, the command and the buttons.
func (p *Pill) popover(c *ui.Context, anchor, panel ui.Element) {
	pal := theme.Current()
	panel = panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(popoverGap, 0, 0, 0).
		Role(ui.RoleDialog).Label(LabelPopover).Column().Width(popoverWidth).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.Radius12).Background(pal.FloatSolid).Border(1, pal.FloatLine)
	withShadow(panel, pal.ShadowFloat).Children(func() {
		in := ui.TextInputBase(c.Key("orchestrate-goal"), &p.goal).Label(LabelGoal).Placeholder(LabelGoal).
			PlaceholderColor(pal.Ink30).TextColor(pal.Ink90).Height(30).FillWidth().Shrink(0).PaddingX(theme.Space12).
			Radius(theme.Radius8).Border(1, pal.InputLine).AutoFocus()
		if in.Submitted() {
			p.send()
		}
		kit.Styled(ui.Text(c, CommandText(p.goal)), theme.StyleMeta).SingleLine().MinWidth(0).TextColor(pal.Ink42)
		ui.Row(c).Justify(ui.End).Gap(theme.Space8).Children(func() {
			kit.CardButton(c, TextCancel, kit.ButtonOutlined, pal.Ink95, func() { p.open = false })
			var send kit.ClickFn
			if !p.busy && p.Send != nil {
				send = p.send
			}
			kit.CardButton(c, TextOrchestrate, kit.ButtonFilled, pal.Ink95, send)
		})
	})
}

// send types the command with the goal into the session the popover is open for, off the UI
// thread. On success the popover closes and the goal clears; an error shows as a toast and keeps
// both.
func (p *Pill) send() {
	if p.busy || p.Send == nil || p.target == "" {
		return
	}
	addr, text := p.target, CommandText(p.goal)
	p.busy = true
	p.Env.Do(func() func() {
		err := p.Send(addr, text)
		return func() {
			p.busy = false
			if err != nil {
				slog.Error("orchestrate: send", "terminal", addr, "err", err)
				p.Env.Show(err.Error())
			} else {
				p.open, p.goal = false, ""
			}
			p.Env.Redraw()
		}
	})
}
