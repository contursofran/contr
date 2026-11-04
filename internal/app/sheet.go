package app

// The context sheet (⌘J, D108): an overlay over the top of the terminal area
// that shows one open question with its full text and its options. The owner
// pages through the open questions with ‹ and ›; the paging stops at the ends.
// An answer goes to the coordinator that asked, through [AgentSource.Answer],
// as the answer card does. The sheet is an overlay, so the terminal keeps its
// size (D67). Esc closes it only while it has the focus (D72).

import (
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/owner"
)

// Labels of the context sheet.
const (
	LabelSheet     = "context sheet"
	SheetPrev      = "Previous item"
	SheetNext      = "Next item"
	SheetEsc       = "esc"
	SheetWhy       = "Why it asks"
	SheetOptions   = "Options"
	SheetAnswer    = "Answer"
	SheetRecommend = "recommended"
)

// Sheet is the context sheet. Its methods run on the UI thread.
type Sheet struct {
	Source AgentSource
	Run    Runner
	// Scope returns the open project, or "" for all projects; may be nil.
	Scope func() string
	// Invalidate redraws the window; may be nil.
	Invalidate func()
	// Anchor is a strip with no height at the top of the terminal area. The
	// frame sets it before it builds the sheet.
	Anchor ui.Element

	now    func() time.Time
	open   bool
	key    string // id of the question shown
	err    string
	busy   bool
	handle ui.Handle
	focus  bool
}

// Toggle opens the sheet, or closes it when it is open.
func (s *Sheet) Toggle() {
	if s.open {
		s.Close()
		return
	}
	s.open, s.err, s.busy, s.focus = true, "", false, true
	s.redraw()
}

// Close hides the sheet.
func (s *Sheet) Close() {
	s.open = false
	s.redraw()
}

// Opened tells whether the sheet shows.
func (s *Sheet) Opened() bool { return s.open }

func (s *Sheet) redraw() {
	if s.Invalidate != nil {
		s.Invalidate()
	}
}

// items are the open questions of the scope, in the order of the needs-you
// row (D69).
func (s *Sheet) items(snap agentstate.Snapshot) []owner.Question {
	project := ""
	if s.Scope != nil {
		project = s.Scope()
	}
	var out []owner.Question
	for _, t := range needItems(snap, project) {
		if t.Kind != NeedQuestion || t.Resolved {
			continue
		}
		if i := slices.IndexFunc(snap.Questions, func(q owner.Question) bool { return q.ID == t.Key }); i >= 0 {
			out = append(out, snap.Questions[i])
		}
	}
	return out
}

// index is the place of the shown question in items. When the question left
// the list (answered), the sheet shows the first item.
func (s *Sheet) index(items []owner.Question) int {
	if i := slices.IndexFunc(items, func(q owner.Question) bool { return q.ID == s.key }); i >= 0 {
		return i
	}
	return 0
}

// step moves by d places and stops at the ends.
func (s *Sheet) step(items []owner.Question, d int) {
	i := s.index(items) + d
	if i < 0 || i >= len(items) {
		return
	}
	s.key, s.err = items[i].ID, ""
	s.redraw()
}

// send answers question id off the UI thread. A failure stays in the foot.
func (s *Sheet) send(id, answer string) {
	if !s.open || s.busy || answer == "" || resolvedID(s.Source.Snapshot(), id) {
		return
	}
	s.busy = true
	s.Run.Do(func() func() {
		err := s.Source.Answer(id, answer)
		return func() {
			s.busy = false
			if err != nil {
				s.err = err.Error()
			} else {
				s.err = ""
			}
			s.redraw()
		}
	})
}

// age is how long ago t was, in the form "5m".
func (s *Sheet) age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	d := max(now().Sub(t), 0)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// View builds the sheet as a popover at the top of [Sheet.Anchor], or nothing
// while it is closed or has no anchor. With no open question it closes.
func (s *Sheet) View(c *ui.Context) {
	if !s.open || s.Anchor == (ui.Element{}) {
		return
	}
	snap := s.Source.Snapshot()
	items := s.items(snap)
	if len(items) == 0 {
		s.open = false
		return
	}
	i := s.index(items)
	q := items[i]
	s.key = q.ID
	shown := true
	pop := ui.PopoverBase(c, s.Anchor, &shown, func(panel ui.Element) {
		panel = panel.AttachTo(s.Anchor, ui.AnchorTop, ui.AnchorTop).Width(max(s.Anchor.Bounds().W, 0))
		panel = panel.Label(LabelSheet).Column().Height(theme.SheetHeight).Gap(theme.SheetGap).
			Padding(theme.SheetPadTop, theme.SheetPadX, theme.SheetPadBottom, theme.SheetPadX).
			Background(theme.Color(theme.BgSheet)).BorderWidth(0, 0, 1, 0).BorderColor(theme.Color(theme.LineStrong)).
			Focusable().FocusRing(false).Bind(&s.handle)
		if s.focus {
			s.focus = false
			s.handle.Focus(c)
		}
		panel.Children(func() {
			s.head(c, q, items, i)
			ui.Text(c, SheetWhy).Shrink(0).TextColor(theme.Color(theme.TextDim))
			ui.Row(c).Grow(1).MinHeight(0).Gap(theme.SheetGap).Children(func() {
				ui.Box(c).Grow(1).MinWidth(0).MinHeight(0).Clip().
					Background(theme.Color(theme.BgSheetBox)).Border(1, theme.Color(theme.SheetBoxBorder)).
					Radius(theme.SheetBoxRadius).Padding(theme.SheetBoxPadY, theme.SheetBoxPadX).Children(func() {
					ui.Text(c, q.Question).TextColor(theme.Color(theme.Text2)).Wrap()
				})
				s.options(c, q)
			})
			if msg := cmpOr(s.err, snap.Problem); msg != "" {
				ui.Text(c, msg).SingleLine().Shrink(0).TextColor(theme.Color(theme.TextDim))
			}
		})
	})
	if pop.Dismissed() {
		s.open = false
		s.redraw()
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// head is the header: icon, id and title, from and age, paging, esc.
func (s *Sheet) head(c *ui.Context, q owner.Question, items []owner.Question, i int) {
	title := q.Title
	if title == "" {
		title = oneLine(q.Question)
	}
	meta := cmpOr(q.From, q.Project)
	if a := s.age(q.Asked); a != "" {
		meta += "  " + a
	}
	icon := icons.Decide
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).Gap(theme.NeedGap).Children(func() {
		ui.Box(c).Shrink(0).Children(func() { icons.Sized(c, icon, theme.IconSizeSmall) })
		ui.Text(c, q.ID+"  "+title).Bold().SingleLine().Grow(1).MinWidth(0).TextColor(theme.Color(theme.Text))
		ui.Text(c, meta).SingleLine().Shrink(0).TextColor(theme.Color(theme.TextDim))
		cardButton(c, SheetPrev, ButtonPlain, theme.TextDim, s.pager(items, -1, i > 0))
		ui.Text(c, strconv.Itoa(i+1)+" of "+strconv.Itoa(len(items))).Shrink(0).TextColor(theme.Color(theme.TextDim))
		cardButton(c, SheetNext, ButtonPlain, theme.TextDim, s.pager(items, 1, i < len(items)-1))
		cardButton(c, SheetEsc, ButtonPlain, theme.TextDim, s.Close)
	})
}

// pager is the click of a paging button, or nil at an end.
func (s *Sheet) pager(items []owner.Question, d int, ok bool) ClickFn {
	if !ok {
		return nil
	}
	return func() { s.step(items, d) }
}

// options is the right column: each option with Answer; the recommended one
// first and marked.
func (s *Sheet) options(c *ui.Context, q owner.Question) {
	ui.Column(c).Width(theme.SheetOptionsWidth).Shrink(0).Gap(theme.SheetGap).Children(func() {
		ui.Text(c, SheetOptions).Shrink(0).TextColor(theme.Color(theme.TextDim))
		for _, o := range CardOptions(q) {
			ui.Column(c).Shrink(0).Gap(4).BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).PaddingY(theme.SheetOptionPadY).Children(func() {
				ui.Text(c, o.Text).Wrap().TextColor(theme.Color(theme.Text))
				if o.Recommended {
					ui.Text(c, SheetRecommend).Shrink(0).TextColor(theme.Color(theme.TextDim))
				}
				cardButton(c, SheetAnswer+" "+o.Text, OptionStyle(o.Recommended), theme.Text, func() { s.send(q.ID, o.Text) })
			})
		}
	})
}
