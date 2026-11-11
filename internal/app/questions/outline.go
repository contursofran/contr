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
	// (spec: at least 20×10 px, for a 2px tick).
	outlineHitWidth  = 20
	outlineHitHeight = 10
	// outlinePitch is the distance from one tick to the next: 2px ticks
	// 9px apart (Outline.md, gap 9px). A hit area plus the margin below it
	// is one pitch.
	outlinePitch = theme.SizeTickHeight + theme.Space9
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
	// outlineCountRoom is the height of the count and its gap: one 14px line and 3px.
	outlineCountRoom = 14 + outlineCountGap
	// outlineMinPitch is the smallest pitch of a column with many ticks: a 2px tick and 2px (D270).
	outlineMinPitch = theme.SizeTickHeight + 2
)

// OutlineTick is one message of the orchestrator, ready to draw.
type OutlineTick struct {
	ID       string // T1, T2, …
	Headline string
	Detail   string
	Pinned   time.Time
	Width    float32 // of the tick at rest, in DIPs
	// Kind is what the tick is, for a tick of the chat (D270): the card's quiet line in place of
	// the detail, and the headline wraps to two lines. "" for a pin of the orchestrator terminal.
	Kind string
	// Blocked is set for a tick that waits for the owner: it is `blocked`, new or not (D270).
	Blocked bool
}

// ChatTicks are the ticks of the chat that shows (D270), from the frame.
type ChatTicks struct {
	// Addr is the hc address of the terminal of the chat: its seen state is the terminal's.
	Addr  string
	Ticks []OutlineTick
	// Top and Bottom are the space above and below the transcript list in the terminal: the
	// column keeps to the list's height.
	Top, Bottom float32
	// Jump scrolls the chat to the row of the tick with the id.
	Jump func(id string)
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
		out = append(out, OutlineTick{ID: t.ID, Headline: head, Detail: detail, Pinned: t.Pinned, Width: TickWidth(t.Text)})
	}
	slices.SortStableFunc(out, func(a, b OutlineTick) int { return a.Pinned.Compare(b.Pinned) })
	return out
}

// TickWidth is the width of a tick at rest for a message of text: 8px and 1px more for each 30
// runes, at most 14px.
func TickWidth(text string) float32 {
	return float32(min(outlineMaxWidth, outlineMinWidth+utf8.RuneCountInString(text)/30))
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
	// Chat gives the ticks of the chat while the selected terminal shows it, in place of the pins
	// (D270); ok is false in Terminal mode. May be nil.
	Chat func() (ChatTicks, bool)

	start   time.Time
	seen    map[string]*seenState // by hc terminal address
	focused string                // the terminal that has the focus now
	// open is the tick whose card a click opened. The tooltip hides after a
	// click, so the outline keeps the card until the pointer leaves.
	open string
	// top is the top of the column's content in the overlay, from the last
	// frame drawn; hasTop is false while no column shows (see Top).
	top    float32
	hasTop bool
	// height is the height of the column's box in the last frame drawn, for the fit (D270).
	height float32
}

// Top returns the top of the column's content, the count or the first tick's
// hit area, in the overlay, from the last frame drawn. ok is false while
// no column shows. The Chat | Terminal switch reads it to keep clear of the
// column (D237, D253). A nil outline has no column.
func (o *Outline) Top() (top float32, ok bool) {
	if o == nil {
		return 0, false
	}
	return o.top, o.hasTop
}

// setTop keeps the top of the column and asks for one more frame when it
// changed, so that a layer that reads Top in its build follows the column.
func (o *Outline) setTop(top float32, ok bool) {
	if top == o.top && ok == o.hasTop {
		return
	}
	o.top, o.hasTop = top, ok
	o.Env.Redraw()
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
	Pitch float32       // the distance from one tick to the next
}

// view returns the ticks to draw for the chat that shows, or else for the selected orchestrator,
// or false when the outline does not show: another kind of terminal, or no tick. h is the height
// of the column's box, 0 before it was laid out.
func (o *Outline) view(chat ChatTicks, inChat bool, h float32) (addr string, v OutlineView, ok bool) {
	var all []OutlineTick
	if inChat {
		addr, all = chat.Addr, chat.Ticks
	} else {
		var project string
		if addr, project, ok = o.target(); !ok {
			return "", v, false
		}
		all = outlineTicks(o.Snapshot().Todos, project)
		all = all[max(0, len(all)-outlineMaxTicks):]
	}
	if len(all) == 0 {
		return "", v, false
	}
	s := o.state(addr)
	for _, t := range all {
		if s.isNew(t) {
			v.Count++
		}
	}
	if v.Count > 0 {
		h -= outlineCountRoom
	}
	n, pitch := outlineFit(len(all), h)
	v.Ticks, v.Pitch = all[len(all)-n:], pitch
	for _, t := range v.Ticks {
		v.New = append(v.New, s.isNew(t))
	}
	return addr, v, true
}

// outlineFit is how many of n ticks show, the newest, and their pitch, in a column h high (D270):
// all at the 11px pitch while they fit, else all at a smaller pitch down to 4px, else the newest
// that fit at 4px. h 0 or less, before the column was laid out, shows all at 11px.
func outlineFit(n int, h float32) (shown int, pitch float32) {
	if h <= 0 || float32(n)*outlinePitch <= h {
		return n, outlinePitch
	}
	if p := h / float32(n); p >= outlineMinPitch {
		return n, p
	}
	return max(1, min(n, int(h/outlineMinPitch))), outlineMinPitch
}

// setHeight keeps the height of the column's box and asks for one more frame when it changed, so
// that the fit follows the column.
func (o *Outline) setHeight(h float32) {
	if h == o.height {
		return
	}
	o.height = h
	o.Env.Redraw()
}

// outlineClock is the time of the card: 24-hour "14:38" (OutlineCard.md).
func outlineClock(t time.Time) string { return t.Local().Format("15:04") }

// View builds the column. It builds nothing for a terminal that is not the
// orchestrator's, or when there is no pin. It reads the snapshot only.
func (o *Outline) View(c *ui.Context) {
	var chat ChatTicks
	inChat := false
	if o.Chat != nil {
		chat, inChat = o.Chat()
	}
	addr, v, ok := o.view(chat, inChat, o.height)
	if !ok {
		o.setTop(0, false)
		return
	}
	p := theme.Current()
	// The draw callbacks get this frame's layout: they keep the column's top
	// for Top, after the layout and before the next build.
	var colY float32
	ui.Box(c).Label(LabelOutline).Absolute().Top(chat.Top).Bottom(chat.Bottom).Right(outlineInset).Width(outlineHitWidth).
		Column().Justify(ui.Center).AlignItems(ui.End).PassThrough().
		FocusGroup(ui.Vertical).Draw(func(_ *ui.Painter, r ui.Rect) {
		colY = r.Y
		o.setHeight(r.H)
	}).Children(func() {
		ui.Column(c).AlignItems(ui.End).PassThrough().Shrink(0).
			Draw(func(_ *ui.Painter, r ui.Rect) { o.setTop(r.Y-colY, true) }).Children(func() {
			if v.Count > 0 {
				count := theme.StyleCount
				ui.Text(c, strconv.Itoa(v.Count)).Label(LabelOutlineCount).SingleLine().TextColor(p.Ink100).
					Font(count.Stack()).FontSize(count.Size).FontWeight(count.Weight).
					Margin(0, 0, outlineCountGap, 0)
			}
			for i, t := range v.Ticks {
				o.tickView(c, addr, t, v.New[i], v.Pitch, chat.Jump)
			}
		})
	})
}

// tickView builds one tick: a button with the 20×10 hit area, or as high as the pitch when it
// is smaller, its tick bar right-aligned, and the card of the message. A click on a tick of the
// chat also scrolls the chat to it (jump).
func (o *Outline) tickView(c *ui.Context, addr string, t OutlineTick, isNew bool, pitch float32, jump func(string)) {
	p := theme.Current()
	label := t.Headline
	if t.Kind != "" {
		label = t.Kind + ": " + label
	}
	if isNew {
		label += ", new"
	}
	b := ui.ButtonBase(c.Key("outline-tick-"+t.ID)).Label(label).Description(outlineCardText(t)).
		Size(outlineHitWidth, min(outlineHitHeight, pitch)).Shrink(0).Justify(ui.End).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).FocusRing(false)
	b = b.OnClick(func() {
		o.Read(addr, t)
		o.open = t.ID
		if jump != nil {
			jump(t.ID)
		}
		o.Env.Redraw()
	})
	b.Margin(0, 0, max(0, pitch-outlineHitHeight), 0) // the 11px pitch: 10px of hit area and 1px more
	grow := b.Hovered() || b.FocusVisible()
	width := t.Width
	if grow {
		width = outlineHover
	}
	ink := tickInk(p, t, isNew)
	b.Children(func() {
		bar := ui.Box(c).Size(width, theme.SizeTickHeight).Background(ink).Radius(theme.Radius1)
		if b.FocusVisible() {
			// The focus ring is 1px, 2px outside (spec README, Focus).
			bar.Border(1, p.FocusRing)
		}
	})
	o.card(c, b, t)
}

// tickInk is the color of a tick: `blocked` for one that waits for the owner, new or not, else
// ink-100 when new and `tick` when read (D270).
func tickInk(p *theme.Palette, t OutlineTick, isNew bool) ui.Color {
	switch {
	case t.Blocked:
		return p.Blocked
	case isNew:
		return p.Ink100
	}
	return p.Tick
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
	s := t.Headline + ". " + cardDetail(t)
	if !t.Pinned.IsZero() {
		s += ". " + outlineClock(t.Pinned)
	}
	return s
}

// cardDetail is the quiet line of the card: the kind of a tick of the chat, else the detail.
func cardDetail(t OutlineTick) string {
	if t.Kind != "" {
		return t.Kind
	}
	return t.Detail
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
		head := ui.Text(c, t.Headline).MinWidth(0).TextColor(p.Ink100).
			Font(q.Stack()).FontSize(q.Size).FontWeight(q.Weight)
		if t.Kind != "" {
			head.MaxLines(2) // the first words of a tick of the chat (D270)
		} else {
			head.SingleLine()
		}
		ui.Box(c).Row().Gap(theme.Space16).AlignItems(ui.End).Children(func() {
			ui.Text(c, cardDetail(t)).SingleLine().Grow(1).MinWidth(0).TextColor(p.Ink42).
				Font(detail.Stack()).FontSize(detail.Size).FontWeight(detail.Weight)
			if !t.Pinned.IsZero() {
				ui.Text(c, outlineClock(t.Pinned)).SingleLine().Shrink(0).TextColor(p.Ink30).
					Font(meta.Stack()).FontSize(meta.Size).FontWeight(meta.Weight)
			}
		})
	})
}
