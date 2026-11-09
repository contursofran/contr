package questions

// The outline (docs/spec/Outline.md and OutlineCard.md): a column of short
// ticks on the right edge of the orchestrator terminal, one for each message
// the orchestrator pinned for the owner (`pin_for_owner`). A tick that came
// while the owner was away is ink-100 and counts; a hover or the keyboard
// focus shows the card of the message.
//
// The pins come from agentstate, which watches owner.json with
// mygo.WatchFiles and reads it on a goroutine. The view reads only the copy
// that Snapshot returns: it does no file or hc I/O. There is no timer: the
// ticks change on a data event or a focus event, and the card shows a clock
// time, not an age.

import (
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/owner"
)

// Labels of the outline.
const (
	// LabelOutline names the column of ticks.
	LabelOutline = "outline"
	// LabelOutlineCount names the count of new messages above the ticks.
	LabelOutlineCount = "outline new count"
	// LabelOutlineCard names the card of a tick.
	LabelOutlineCard = "outline card"
)

// Sizes of the outline that tokens.md leaves to the spec text.
const (
	// outlineInset is the space between the ticks and the right edge.
	outlineInset = 10
	// outlineHitWidth and outlineHitHeight are the hit area of a tick
	// (spec: at least 20×10 px, for a 2px tick). The height plus the gap
	// below it is the 11px pitch of 2px ticks 9px apart.
	outlineHitWidth  = 20
	outlineHitHeight = 10
	// outlineHover is the width of a hovered or focused tick.
	outlineHover = 20
	// outlineMinWidth and outlineMaxWidth bound the width of a tick, which
	// follows the length of the message.
	outlineMinWidth = 8
	outlineMaxWidth = 14
	// outlineMaxTicks is the most ticks the column shows: the newest.
	outlineMaxTicks = 40
	// outlineCountGap is the space between the count and the first tick.
	outlineCountGap = 3
)

// OutlineTick is one message of the orchestrator, ready to draw.
type OutlineTick struct {
	ID       string // T1, T2, …
	Headline string
	Detail   string
	Pinned   time.Time
	Width    float32 // of the tick at rest, in DIPs
}

// outlineTicks turns the open to-dos of a project into ticks, oldest first.
// The headline is the first line of the text. The detail is the next line of
// the text, or else what kind of message it is and who pinned it.
func outlineTicks(todos []owner.Todo, project string) []OutlineTick {
	var out []OutlineTick
	for _, t := range todos {
		if t.Project != project || !t.Done.IsZero() {
			continue
		}
		head, rest := splitMessage(t.Text)
		if head == "" {
			continue
		}
		detail := rest
		if detail == "" {
			detail = "to do"
			if t.Kind == "know" {
				detail = "to know"
			}
			if t.From != "" {
				detail += " · from " + t.From
			}
		}
		n := utf8.RuneCountInString(t.Text)
		w := min(outlineMaxWidth, outlineMinWidth+n/30)
		out = append(out, OutlineTick{ID: t.ID, Headline: head, Detail: detail, Pinned: t.Pinned, Width: float32(w)})
	}
	slices.SortStableFunc(out, func(a, b OutlineTick) int { return a.Pinned.Compare(b.Pinned) })
	return out
}

// splitMessage returns the first and the second non-empty line of text.
func splitMessage(text string) (head, rest string) {
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "":
		case head == "":
			head = l
		default:
			return head, l
		}
	}
	return head, ""
}

// seenState is what the outline knows of the owner's look at one terminal.
type seenState struct {
	// left is the last time the terminal had the focus, or when the outline
	// began, or the time of the latest tick the owner clicked.
	left time.Time
	// gained is when the terminal got the focus now; zero while it does not
	// have it. A message that comes while the owner looks is not new.
	gained time.Time
}

// Outline is the column of ticks. Build it with [NewOutline] and add View as a
// layer of the terminal overlay. UI thread only.
type Outline struct {
	// Snapshot gives the copy of the owner's state. It must not do I/O.
	Snapshot func() agentstate.Snapshot
	// Selection is the sidebar selection: the outline shows only for a
	// selected orchestrator.
	Selection *state.Selection
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Env asks for a new frame.
	Env view.Env

	start   time.Time
	seen    map[string]*seenState // by hc terminal address
	focused string                // the terminal that has the focus now
	// open is the tick whose card a click opened. The tooltip hides after a
	// click, so the outline keeps the card until the pointer leaves.
	open string
}

// NewOutline returns an outline. Messages that were pinned before it began
// are old: the owner counts only what comes while away.
func NewOutline(snap func() agentstate.Snapshot, sel *state.Selection, env view.Env) *Outline {
	o := &Outline{Snapshot: snap, Selection: sel, Env: env}
	o.start = o.now()
	return o
}

func (o *Outline) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Outline) state(addr string) *seenState {
	if o.seen == nil {
		o.seen = map[string]*seenState{}
	}
	s := o.seen[addr]
	if s == nil {
		s = &seenState{left: o.start}
		o.seen[addr] = s
	}
	return s
}

// Focus tells which hc terminal has the focus now: the selected terminal while
// the window has the focus, else "". The terminal that loses it becomes
// "last seen now"; the one that gets it keeps the ticks that came while the
// owner was away as new until the owner leaves again. A nil Outline ignores it.
func (o *Outline) Focus(addr string) {
	if o == nil || addr == o.focused {
		return
	}
	now := o.now()
	if o.focused != "" {
		s := o.state(o.focused)
		s.left, s.gained = now, time.Time{}
	}
	o.focused = addr
	if addr != "" {
		o.state(addr).gained = now
	}
	o.Env.Redraw()
}

// Focused is the terminal that has the focus now, as the last [Outline.Focus] told; "" for none
// or a nil Outline.
func (o *Outline) Focused() string {
	if o == nil {
		return ""
	}
	return o.focused
}

// isNew tells whether a message of the terminal came while the owner was away.
func (s *seenState) isNew(t OutlineTick) bool {
	return t.Pinned.After(s.left) && (s.gained.IsZero() || t.Pinned.Before(s.gained))
}

// Read marks the tick and the ones before it as read in the terminal.
func (o *Outline) Read(addr string, t OutlineTick) {
	s := o.state(addr)
	if t.Pinned.After(s.left) {
		s.left = t.Pinned
	}
}

// target returns the selected orchestrator's address and project.
func (o *Outline) target() (addr, project string, ok bool) {
	if o.Selection == nil || o.Snapshot == nil {
		return "", "", false
	}
	sel, ok := o.Selection.Current()
	if !ok || sel.Kind != state.KindOrchestrator {
		return "", "", false
	}
	for _, w := range o.Snapshot().Orchestrators {
		if w.Address == sel.ID {
			return w.Address, w.Project, true
		}
	}
	return "", "", false
}

// OutlineView is what the outline draws for one terminal.
type OutlineView struct {
	Ticks []OutlineTick // the newest that fit, oldest first
	New   []bool        // New[i] tells whether Ticks[i] is new
	Count int           // how many messages are new, also the ones not shown
}

// view returns the ticks to draw for the selected orchestrator, or false when
// the outline does not show: another kind of terminal, or no pin.
func (o *Outline) view() (addr string, v OutlineView, ok bool) {
	addr, project, ok := o.target()
	if !ok {
		return "", v, false
	}
	all := outlineTicks(o.Snapshot().Todos, project)
	if len(all) == 0 {
		return "", v, false
	}
	s := o.state(addr)
	for _, t := range all {
		if s.isNew(t) {
			v.Count++
		}
	}
	v.Ticks = all[max(0, len(all)-outlineMaxTicks):]
	for _, t := range v.Ticks {
		v.New = append(v.New, s.isNew(t))
	}
	return addr, v, true
}

// outlineClock is the time of the card: 24-hour "14:38" (OutlineCard.md).
func outlineClock(t time.Time) string { return t.Local().Format("15:04") }

// View builds the column. It builds nothing for a terminal that is not the
// orchestrator's, or when there is no pin. It reads the snapshot only.
func (o *Outline) View(c *ui.Context) {
	addr, v, ok := o.view()
	if !ok {
		return
	}
	p := theme.Current()
	ui.Box(c).Label(LabelOutline).Absolute().Top(0).Bottom(0).Right(outlineInset).Width(outlineHitWidth).
		Column().Justify(ui.Center).AlignItems(ui.End).PassThrough().
		FocusGroup(ui.Vertical).Children(func() {
		if v.Count > 0 {
			count := theme.StyleCount
			ui.Text(c, strconv.Itoa(v.Count)).Label(LabelOutlineCount).SingleLine().TextColor(p.Ink100).
				Font(count.Stack()).FontSize(count.Size).FontWeight(count.Weight).
				Margin(0, 0, outlineCountGap, 0)
		}
		for i, t := range v.Ticks {
			o.tickView(c, addr, t, v.New[i])
		}
	})
}

// tickView builds one tick: a button with the 20×10 hit area, its tick bar
// right-aligned, and the card of the message.
func (o *Outline) tickView(c *ui.Context, addr string, t OutlineTick, isNew bool) {
	p := theme.Current()
	label := t.Headline
	if isNew {
		label += ", new"
	}
	b := ui.ButtonBase(c.Key("outline-tick-"+t.ID)).Label(label).Description(outlineCardText(t)).
		Size(outlineHitWidth, outlineHitHeight).Shrink(0).Justify(ui.End).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).FocusRing(false)
	b = b.OnClick(func() {
		o.Read(addr, t)
		o.open = t.ID
		o.Env.Redraw()
	})
	b.Margin(0, 0, theme.Space9-1, 0) // the 11px pitch: 10px of hit area and 1px more
	grow := b.Hovered() || b.FocusVisible()
	width := t.Width
	if grow {
		width = outlineHover
	}
	ink := p.Tick
	if isNew {
		ink = p.Ink100
	}
	b.Children(func() {
		bar := ui.Box(c).Size(width, theme.SizeTickHeight).Background(ink).Radius(theme.Radius1)
		if b.FocusVisible() {
			// The focus ring is 1px, 2px outside (spec README, Focus).
			bar.Border(1, p.FocusRing)
		}
	})
	o.card(c, b, t)
}

// card shows the card of the tick beside it, at its left: the tooltip while
// the pointer rests or the focus is on it, or the card a click opened, until
// the pointer and the focus leave or Esc.
func (o *Outline) card(c *ui.Context, b ui.Element, t OutlineTick) {
	place := func(tip ui.Element) {
		tip.AttachTo(b, ui.AnchorLeft, ui.AnchorRight).Margin(0, theme.Space12-6, 0, 0)
		outlineCard(c, tip, t)
	}
	if tip := ui.TooltipBase(c, b, place); tip != (ui.Element{}) || o.open != t.ID {
		return
	}
	if !b.Hovered() && !b.FocusVisible() {
		o.open = "" // the pointer and the focus left: the card of the click goes
		return
	}
	ui.Overlay(c, func() {
		tip := ui.Box(c).Absolute().PassThrough()
		place(tip)
		if tip.OverlayShortcut(0, ui.KeyEscape) {
			o.open = ""
		}
	})
}

// outlineCardText is the text of the card for assistive technology: the headline, the
// detail and the time (OutlineCard.md, Accessibility).
func outlineCardText(t OutlineTick) string {
	return t.Headline + ". " + t.Detail + ". " + outlineClock(t.Pinned)
}

// outlineCard styles tip as the OutlineCard and builds its lines: the headline,
// then the detail with the time at the bottom right.
func outlineCard(c *ui.Context, tip ui.Element, t OutlineTick) {
	p := theme.Current()
	shadow := p.ShadowFloat
	tip.Label(LabelOutlineCard).Width(theme.SizeOutlineCardWidth).Padding(12, 14, 13, 16).Gap(5).Column().
		Radius(theme.Radius12)
	kit.GlassFloat(tip, p).Border(1, p.FloatLine)
	for _, l := range shadow {
		tip.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
	}
	tip.Children(func() {
		q, detail, meta := theme.StyleQuestion, theme.StyleRowSmall, theme.StyleMeta
		ui.Text(c, t.Headline).SingleLine().MinWidth(0).TextColor(p.Ink100).
			Font(q.Stack()).FontSize(q.Size).FontWeight(q.Weight)
		ui.Box(c).Row().Gap(theme.Space16).AlignItems(ui.End).Children(func() {
			ui.Text(c, t.Detail).SingleLine().Grow(1).MinWidth(0).TextColor(p.Ink42).
				Font(detail.Stack()).FontSize(detail.Size).FontWeight(detail.Weight)
			ui.Text(c, outlineClock(t.Pinned)).SingleLine().Shrink(0).TextColor(p.Ink30).
				Font(meta.Stack()).FontSize(meta.Size).FontWeight(meta.Weight)
		})
	})
}
