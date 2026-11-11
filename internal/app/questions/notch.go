package questions

// The questions notch (docs/spec/QuestionsNotch.md, D124): a small glass tab
// that hangs from the top of the terminal while the open project has a
// question, a to-do or a blocked worker's menu. It sits in the terminal
// overlay, so it shows over the terminal of the orchestrator and of every
// worker of the project. A click or ⌘J opens the questions card
// (questions.go), which hangs from the notch.
//
// The data is the agent state: it reads owner.json on a goroutine, after a
// file watch event (agentstate), so the notch reads no file on the UI thread.

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/shortcut"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelNotchLayer names the notch layer of the terminal overlay.
const LabelNotchLayer = "questions notch"

// LabelNotch is the part of the screen-reader label after the count.
const LabelNotch = "Open"

// NotchLabel is the text of the notch for items: "1 needs you", "2 need
// you" (D258). Questions, to-dos and blocked menus count the same. It is
// empty for no items.
func NotchLabel(items []agentstate.NeedTab) string {
	switch n := len(items); n {
	case 0:
		return ""
	case 1:
		return "1 needs you"
	default:
		return strconv.Itoa(n) + " need you"
	}
}

// NotchNewest is the text of the newest item, which the notch shows after the
// label (D258): its title, else its key. The newest has the latest Since;
// on a tie, or with no times, the last in items. Empty for no items.
func NotchNewest(items []agentstate.NeedTab) string {
	if len(items) == 0 {
		return ""
	}
	newest := items[0]
	for _, t := range items[1:] {
		if !t.Since.Before(newest.Since) {
			newest = t
		}
	}
	return cmp.Or(newest.Title, newest.Key)
}

// NotchAccessible is the screen-reader label: the text, a period and "Open".
func NotchAccessible(items []agentstate.NeedTab) string {
	if len(items) == 0 {
		return ""
	}
	return NotchLabel(items) + ". " + LabelNotch
}

// Notch is the questions notch. Its methods run on the UI thread.
type Notch struct {
	// Snapshot reads the agent state; nil means nothing waits.
	Snapshot func() agentstate.Snapshot
	// Project returns the project of the terminal on screen. An empty project
	// shows no notch: items of other projects never count.
	Project func() string
	// Open shows the card for an item; Close hides the card; Opened tells
	// whether it shows. Each may be nil.
	Open   func(agentstate.NeedTab)
	Close  func()
	Opened func() bool
	// Panel builds the card anchored to the notch while it is open; may be nil.
	Panel func(c *ui.Context, anchor ui.Element, items []agentstate.NeedTab)
	// Hints shows the key chip of ⌘J on the notch's dot: ⌘ is held
	// (docs/spec/KeyHints.md). The frame sets it on each frame.
	Hints bool

	// arrivals tells when the count of the open project goes up (D240).
	arrivals kit.Arrivals
	// rect is the box of the notch (or of the file problem in its place) as last painted, zero
	// while none shows (Bounds).
	rect ui.Rect
}

// Bounds is the box of the notch, or of the file problem in its place, as last painted, in
// window DIPs; false while none shows. The chat's pinned prompt ends before it (D266).
func (n *Notch) Bounds() (ui.Rect, bool) { return n.rect, n.rect.W > 0 }

// painted keeps the box r of the notch as it is painted. A box that changed asks for one more
// frame, so a view that reads Bounds while it builds sees it.
func (n *Notch) painted(c *ui.Context, r ui.Rect) {
	if r != n.rect {
		n.rect = r
		c.Invalidate()
	}
}

// hidden forgets the box of the notch when none shows, and asks for one more frame when it had one.
func (n *Notch) hidden(c *ui.Context) {
	if n.rect != (ui.Rect{}) {
		n.rect = ui.Rect{}
		c.Invalidate()
	}
}

// Items returns the open items of the project: questions, then to-dos, then
// blocked menus (D124, D140). It returns none while the owner file is broken (the model logs the
// error and the notch shows the problem, see problemView) or no project is open. Items that the
// owner resolved in this run do not count.
func (n *Notch) Items() []agentstate.NeedTab {
	if n.Snapshot == nil || n.Project == nil {
		return nil
	}
	project := n.Project()
	if project == "" {
		return nil
	}
	snap := n.Snapshot()
	if snap.OwnerProblem != "" {
		return nil
	}
	var open []agentstate.NeedTab
	for _, t := range agentstate.NeedItemsOf(snap, project) {
		if !t.Resolved {
			open = append(open, t)
		}
	}
	slices.SortStableFunc(open, func(a, b agentstate.NeedTab) int { return cardRank(a.Kind) - cardRank(b.Kind) })
	return open
}

// Shown tells whether the notch shows: an open item waits, or the questions file cannot be read
// (problemView).
func (n *Notch) Shown() bool {
	if len(n.Items()) > 0 {
		return true
	}
	return n.Snapshot != nil && n.Snapshot().OwnerProblem != ""
}

// cardRank orders the chips of the questions card: questions, to-dos, blocked menus.
func cardRank(k agentstate.NeedKind) int {
	switch k {
	case agentstate.NeedQuestion:
		return 0
	case agentstate.NeedTodo:
		return 1
	}
	return 2
}

// Toggle closes the card while it shows, else opens it for the first item: the
// first question when there is one.
// It does nothing when no item waits and the card is closed.
func (n *Notch) Toggle() {
	if n.Opened != nil && n.Opened() {
		if n.Close != nil {
			n.Close()
		}
		return
	}
	items := n.Items()
	if len(items) == 0 || n.Open == nil {
		return
	}
	n.Open(items[0])
}

// View builds the notch, or nothing when no item waits. It is absolute, at
// the top center of the terminal; the strip around it lets the pointer through
// to the terminal.
func (n *Notch) View(c *ui.Context) {
	items := n.Items()
	open := n.Opened != nil && n.Opened()
	// A new item starts the attention effect (D240). While the card shows, the
	// owner already sees the items: a new one starts nothing.
	var arrival kit.Arrival
	if n.Project != nil {
		arrival = n.arrivals.Observe(n.Project(), len(items), c.Now(), !open)
	}
	if len(items) == 0 {
		// Nothing waits any more: the card closes with the notch, and does not
		// open again by itself when something new comes.
		if open && n.Close != nil {
			n.Close()
		}
		n.problemView(c)
		return
	}
	p := theme.Current()
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
		b := ui.ButtonBase(c).Label(NotchAccessible(items)).Height(theme.SizeNotchHeight).Shrink(1).MinWidth(0).
			Radius(0, 0, theme.Radius12, theme.Radius12).Cursor(ui.CursorPointer).
			FocusRing(false).OnClick(n.Toggle)
		b = notchShadow(b, p)
		// The effect plays on the button, which stays while the card shows.
		look := kit.NotchAttention(c, b, arrival)
		if open {
			look = kit.Still
		}
		if look.Scale != 1 {
			b = b.Scale(look.Scale, look.Scale).TransformOrigin(0.5, 0)
		}
		focus := b.FocusVisible()
		b = b.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			n.painted(c, r)
			if focus {
				// 1px ring, 2px outside the box (docs/spec/README.md, Focus).
				const gap, width = 2, 1
				pt.Stroke(ui.Rect{X: r.X - gap - width, Y: r.Y - gap - width, W: r.W + 2*(gap+width), H: r.H + 2*(gap+width)},
					p.FocusRing, theme.Radius12+gap+width, width)
			}
		})
		if open && n.Panel != nil {
			// The card takes the notch's place: the tab shows nothing, and stays
			// the anchor of the card.
			b.Children(func() {})
			n.Panel(c, b, items)
			return
		}
		b.Children(func() {
			var dot ui.Element
			// glass-float over the blur, with float-line on the sides and the
			// bottom (no top border).
			// The newest item gives way first in a narrow terminal: it
			// shrinks to an ellipsis, the dot and the label keep their width.
			kit.GlassFloat(ui.Row(c).Height(theme.SizeNotchHeight).MinWidth(0).AlignItems(ui.Center).Gap(theme.Space9).
				Padding(0, theme.Space16, 0, theme.Space12).
				Radius(0, 0, theme.Radius12, theme.Radius12), p).
				BorderWidth(0, 1, 1, 1).BorderColor(p.FloatLine).Children(func() {
				dot = notchDot(c, p, look.Pulse)
				ui.Text(c, NotchLabel(items)).NoWrap().Shrink(0).TextColor(p.Ink100).
					FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
				if s := NotchNewest(items); s != "" {
					ui.Text(c, s).SingleLine().MinWidth(0).MaxWidth(notchNewestMax).
						TextColor(p.Ink62).FontSize(theme.StyleLabel.Size)
				}
			})
			if look.Glow > 0 {
				notchGlow(c, p, look.Glow)
			}
			kit.KeyHint(c, shortcut.Of(shortcut.Questions).HintIf(n.Hints), b, dot)
		})
	})
}

// Sizes of the notch dot (D240): 8px, with 3px at each side, so it takes the
// 14px of the small glyph.
const (
	notchDotSize   = 8
	notchDotMargin = 3
)

// notchNewestMax is the widest the newest item gets, so a long title does not
// stretch the notch across the terminal.
const notchNewestMax = 320

// LabelNotchDot names the dot of the notch.
const LabelNotchDot = "needs you"

// notchDot is the solid `blocked` dot of the notch. pulse (0 to 1) dims it to
// opacity 0.3 and shrinks it to 0.8 during the pulse after an arrival (D240).
func notchDot(c *ui.Context, p *theme.Palette, pulse float32) ui.Element {
	d := ui.Box(c).Label(LabelNotchDot).Size(notchDotSize, notchDotSize).Shrink(0).MarginX(notchDotMargin).
		Radius(notchDotSize / 2).Background(p.Blocked)
	if pulse > 0 {
		s := 1 - 0.2*pulse
		d = d.Opacity(1-0.7*pulse).Scale(s, s).TransformOrigin(0.5, 0.5)
	}
	return d
}

// notchGlow is the `blocked` glow around the notch after an arrival: a 1px
// ring and a soft shadow, at strength g (D240). It takes no pointer.
func notchGlow(c *ui.Context, p *theme.Palette, g float32) {
	r := float32(theme.Radius12 + 1)
	ui.Box(c).Absolute().Top(-1).Left(-1).Right(-1).Bottom(-1).PassThrough().
		Radius(0, 0, r, r).Border(1, p.Blocked.Alpha(0.6*g)).Shadow(0, 0, 18, 3, p.Blocked.Alpha(0.42*g))
}

// notchShadow gives b the shadow-notch token.
func notchShadow(b ui.Element, p *theme.Palette) ui.Element {
	for _, l := range p.ShadowNotch {
		if l.Inset {
			b = b.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		} else {
			b = b.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
	return b
}

// Texts of the owner file problem (D146).
const (
	// TextOwnerProblem is the notch text while the questions file cannot be read.
	TextOwnerProblem = "Questions file unreadable"
	// LabelOwnerProblem names the notch of the problem.
	LabelOwnerProblem = "questions file problem"
)

// problemView shows that the questions file (owner.json) cannot be read, or is locked, in the
// place of the notch (D146). Without it the notch would only hide and the owner would not know
// that questions can wait. It is in every terminal like the notch, shows the error in a tooltip,
// takes no click, and is in `blocked`, the hue for what needs the owner. It builds nothing while
// the file reads well.
func (n *Notch) problemView(c *ui.Context) {
	if n.Snapshot == nil {
		n.hidden(c)
		return
	}
	prob := n.Snapshot().OwnerProblem
	if prob == "" {
		n.hidden(c)
		return
	}
	p := theme.Current()
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
		b := ui.Box(c).Label(LabelOwnerProblem).Height(theme.SizeNotchHeight).Shrink(0).
			Radius(0, 0, theme.Radius12, theme.Radius12).Tooltip(prob).
			DrawOver(func(_ *ui.Painter, r ui.Rect) { n.painted(c, r) })
		notchShadow(b, p).Children(func() {
			kit.GlassFloat(ui.Row(c).Height(theme.SizeNotchHeight).AlignItems(ui.Center).Gap(theme.Space9).
				Padding(0, theme.Space16, 0, theme.Space12).
				Radius(0, 0, theme.Radius12, theme.Radius12), p).
				BorderWidth(0, 1, 1, 1).BorderColor(p.FloatLine).Children(func() {
				kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphNeedsYou, Small: true})
				ui.Text(c, TextOwnerProblem).NoWrap().TextColor(p.Blocked).
					FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
			})
		})
	})
}
