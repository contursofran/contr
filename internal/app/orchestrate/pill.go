// Package orchestrate is the orchestrate pill (docs/spec/OrchestratePill.md, D234, D253): a pill in
// the title bar, left of the diff summary, for the selected Claude session. It says that the session
// orchestrates, with the count of its live workers, or it is a button that sends /contr:orchestrate
// with a goal.
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
	popoverWidth = 320
	popoverGap   = theme.Space6
)

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
	// Send types text and Enter into the terminal at an address. It blocks: it runs off the UI
	// thread. Nil makes the button do nothing.
	Send func(addr, text string) error
	// Env runs the send and shows its error.
	Env view.Env

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

// View builds the pill in the title bar row, or nothing for a terminal that is not a running
// Claude session. compact shows only the mascot and the worker count, with the full text as a
// tooltip: the title bar uses it in a narrow window, so the tabs keep their room (D253).
func (p *Pill) View(c *ui.Context, compact bool) {
	w, snap, ok := p.session()
	orchestrating := ok && snap.Orchestrating(w)
	if !ok || orchestrating || (p.open && p.target != w.Address) {
		p.open = false // the popover goes with the button, or when the selection moves
	}
	switch {
	case !ok:
	case orchestrating:
		p.stateView(c, snap.LiveWorkersOf(w.Address), compact)
	default:
		p.buttonView(c, w.Address, compact)
	}
}

// box styles the pill as the icon buttons of the title bar: icon-button height, fully round,
// no line and no fill at rest (D253, D258). The space-8 after it keeps it from the next item.
func box(e ui.Element, compact bool) ui.Element {
	pad := float32(theme.Space10)
	if compact {
		pad = theme.Space6
	}
	return e.Height(theme.SizeIconButton).Shrink(0).PaddingX(pad).Gap(theme.Space6).AlignItems(ui.Center).
		Radius(theme.SizeIconButton / 2).MarginEnd(theme.Space8)
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

// mascot is the still Claude mascot of the pill: claude while it orchestrates, ink-48 on the
// button (D191). Waiting is a still state at full opacity; the pill's label names it.
func mascot(c *ui.Context, grey bool) {
	kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWaiting, Claude: true, Grey: grey, Small: true}).AccessHidden(true)
}

// stateView builds the pill of an orchestrating session with n live workers. It takes no click.
// Compact, it shows the mascot and n (none while n is 0).
func (p *Pill) stateView(c *ui.Context, n int, compact bool) {
	pal := theme.Current()
	text := PillText(n)
	b := box(ui.Row(c).Label(text), compact)
	if compact {
		b = b.Tooltip(text)
	}
	b.Children(func() {
		mascot(c, false)
		switch {
		case !compact:
			kit.Styled(ui.Text(c, text), theme.StyleLabel).NoWrap().TextColor(pal.Ink88)
		case n > 0:
			kit.Styled(ui.Text(c, strconv.Itoa(n)), theme.StyleCount).NoWrap().TextColor(pal.Ink88)
		}
	})
}

// buttonView builds the Orchestrate button and its popover. Compact, it shows the mascot alone.
func (p *Pill) buttonView(c *ui.Context, addr string, compact bool) {
	pal := theme.Current()
	b := box(ui.ButtonBase(c).Label(LabelButton), compact).Cursor(ui.CursorPointer).FocusRing(false).
		OnClick(func() {
			p.open = !p.open
			p.target = addr
		})
	if compact {
		b = b.Tooltip(TextOrchestrate)
	}
	focus := b.FocusVisible()
	ink := pal.Ink88
	if b.Hovered() || p.open {
		b = b.Background(pal.Hover)
		ink = pal.Ink100
	}
	b = b.DrawOver(func(pt *ui.Painter, r ui.Rect) {
		if focus {
			// 1px ring, 2px outside the box (docs/spec/README.md, Focus).
			const gap, width = 2, 1
			pt.Stroke(ui.Rect{X: r.X - gap - width, Y: r.Y - gap - width, W: r.W + 2*(gap+width), H: r.H + 2*(gap+width)},
				pal.FocusRing, theme.SizeIconButton/2+gap+width, width)
		}
	})
	b.Children(func() {
		mascot(c, true)
		if !compact {
			kit.Styled(ui.Text(c, TextOrchestrate), theme.StyleLabel).NoWrap().TextColor(ink)
		}
	})
	ui.PopoverBase(c, b, &p.open, func(panel ui.Element) { p.popover(c, b, panel) })
}

// popover styles the panel and builds the goal field, the command and the buttons.
func (p *Pill) popover(c *ui.Context, anchor, panel ui.Element) {
	pal := theme.Current()
	panel = panel.AttachTo(anchor, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(popoverGap, 0, 0, 0).
		Role(ui.RoleDialog).Label(LabelPopover).Column().Width(popoverWidth).Padding(theme.Space12).Gap(theme.Space8).
		Radius(theme.Radius12).Background(pal.FloatSolid).Border(1, pal.FloatLine)
	panel = kit.PopIn(panel)
	withShadow(panel, pal.ShadowFloat).Children(func() {
		in := ui.TextInputBase(c.Key("orchestrate-goal"), &p.goal).Label(LabelGoal).Placeholder(LabelGoal).
			PlaceholderColor(pal.Ink48).TextColor(pal.Ink88).Height(30).FillWidth().Shrink(0).PaddingX(theme.Space12).
			Radius(theme.Radius8).Border(1, pal.InputLine).AutoFocus()
		if in.Submitted() {
			p.send()
		}
		kit.Styled(ui.Text(c, CommandText(p.goal)), theme.StyleMeta).SingleLine().MinWidth(0).TextColor(pal.Ink48)
		ui.Row(c).Justify(ui.End).Gap(theme.Space8).Children(func() {
			kit.CardButton(c, TextCancel, kit.ButtonOutlined, pal.Ink88, func() { p.open = false })
			var send kit.ClickFn
			if !p.busy && p.Send != nil {
				send = p.send
			}
			kit.CardButton(c, TextOrchestrate, kit.ButtonFilled, pal.Ink88, send)
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
