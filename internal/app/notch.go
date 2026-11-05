package app

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
	"strconv"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelNotchLayer names the notch layer of the terminal overlay.
const LabelNotchLayer = "questions notch"

// LabelNotch is the part of the screen-reader label after the count.
const LabelNotch = "Open"

// NotchLabel is the text of the notch for items: "1 question", "2 questions",
// and "3 items" once a to-do or a blocked menu is among them (D124; "1 item"
// for a single one). It is
// empty for no items.
func NotchLabel(items []NeedTab) string {
	n := len(items)
	if n == 0 {
		return ""
	}
	allQuestions := true
	for _, t := range items {
		allQuestions = allQuestions && t.Kind == NeedQuestion
	}
	switch {
	case !allQuestions && n == 1:
		return "1 item"
	case !allQuestions:
		return strconv.Itoa(n) + " items"
	case n == 1:
		return "1 question"
	}
	return strconv.Itoa(n) + " questions"
}

// NotchAccessible is the screen-reader label: the text, a period and "Open".
func NotchAccessible(items []NeedTab) string {
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
	Open   func(NeedTab)
	Close  func()
	Opened func() bool
	// Panel builds the card anchored to the notch while it is open; may be nil.
	Panel func(c *ui.Context, anchor ui.Element, items []NeedTab)
}

// Items returns the open items of the project: blocked menus, questions and
// to-dos, in the order of the needs-you row. It returns none while the owner
// file is broken (the model logs the error) or no project is open. Items that
// the owner resolved in this run, and that the row keeps struck through, do
// not count.
func (n *Notch) Items() []NeedTab {
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
	var open []NeedTab
	for _, t := range needItems(snap, project) {
		if !t.Resolved {
			open = append(open, t)
		}
	}
	return open
}

// Toggle closes the card while it shows, else opens it for the first item.
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
	if len(items) == 0 {
		// Nothing waits any more: the card closes with the notch, and does not
		// open again by itself when something new comes.
		if open && n.Close != nil {
			n.Close()
		}
		return
	}
	p := theme.Current()
	ui.Row(c).Absolute().Top(0).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
		b := ui.ButtonBase(c).Label(NotchAccessible(items)).Height(theme.SizeNotchHeight).Shrink(0).
			Radius(0, 0, theme.Radius12, theme.Radius12).Cursor(ui.CursorPointer).
			Material(glass.Blur{Radius: theme.GlassBlur}).FocusRing(false).OnClick(n.Toggle)
		for _, l := range p.ShadowNotch {
			if l.Inset {
				b = b.InsetShadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			} else {
				b = b.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			}
		}
		focus := b.FocusVisible()
		b = b.DrawOver(func(pt *ui.Painter, r ui.Rect) {
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
			// glass-float over the blur, with float-line on the sides and the
			// bottom (no top border).
			ui.Row(c).Height(theme.SizeNotchHeight).AlignItems(ui.Center).Gap(theme.Space9).
				Padding(0, theme.Space16, 0, theme.Space12).
				Radius(0, 0, theme.Radius12, theme.Radius12).Background(p.GlassFloat).
				BorderWidth(0, 1, 1, 1).BorderColor(p.FloatLine).Children(func() {
				kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphNeedsYou, Small: true})
				ui.Text(c, NotchLabel(items)).NoWrap().TextColor(p.Ink100).
					FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
			})
		})
	})
}
