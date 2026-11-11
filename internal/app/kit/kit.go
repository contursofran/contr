// Package kit holds contr's shared view parts, built on MyGo's own
// components: a selectable list row, +/- counts, a switch, and a code view
// that shows a whole file with its changes. Views use these parts and do not
// build them again (#161).
package kit

import (
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/app/theme"
)

// RowHeight is the height of a one-line row; a row with Sub is two lines.
const RowHeight = 26

// RowOpts describes a list row.
type RowOpts struct {
	// Label names the row for assistive technology and tests. Text by default.
	Label string
	// Mark is a short leading mark, such as a file status (M, A, D).
	Mark      string
	MarkColor ui.Color // p.Ink48 by default (the zero Color)
	// Text is the main line; Sub, if set, a second line in the meta color.
	// Both clip with an ellipsis.
	Text, Sub string
	// Counts shows +Added -Removed at the end.
	Counts         bool
	Added, Removed int
	Selected       bool
	OnClick        func()
	// Lead draws a leading part, such as a status icon, before Mark, and returns it: a flat
	// row puts its key chip on it (Hint).
	Lead func() ui.Element

	// Flat gives the row the token look of the sidebar (flatrow.go). It uses
	// Label, Text, Meta, Selected, OnClick, Lead, Trail and the fields below;
	// the other fields are for the card look.
	Flat bool
	// Meta is the text at the right of a flat row.
	Meta string
	// NameInk and MetaInk replace the ink of the name (ink-64) and of the
	// meta (ink-48). The zero color keeps the default.
	NameInk, MetaInk ui.Color
	// NameStyle replaces the type style of the name (body).
	NameStyle theme.TextStyle
	// Expanded, when set, tells assistive technology that the row folds.
	Expanded *bool
	// Trail draws a trailing part, such as an icon button, after the meta.
	Trail func()
	// Compact makes a flat row show Lead only, centered, with Tip as its
	// tooltip (D128). The name, the meta and Trail are not drawn.
	Compact bool
	Tip     string
	// Menu fills the context menu of a flat row. Nil: no menu.
	Menu func(m *ui.Menu)
	// Hint is the key chip of a flat row while ⌘ is held, on the element of Lead (KeyHint).
	// Empty: none.
	Hint string
}

// HeightOf is the height Row gives to a row with these options.
func HeightOf(o RowOpts) float32 {
	if o.Flat {
		return theme.SizeRowHeight
	}
	if o.Sub != "" {
		return 2*theme.LineHeight + 6
	}
	return RowHeight
}

// Row builds a selectable list row, as the sidebar's rows look: a card
// background and border when selected, the same border color as the
// background otherwise, so selecting moves nothing.
func Row(c *ui.Context, o RowOpts) {
	if o.Flat {
		flatRow(c, o)
		return
	}
	label := o.Label
	if label == "" {
		label = o.Text
	}
	height := HeightOf(o)
	p := theme.Current()
	border := ui.Color{} // clear: selecting moves nothing
	if o.Selected {
		border = p.SelectedLine
	}
	row := ui.ButtonBase(c).Label(label).Height(height).Shrink(0).
		AlignItems(ui.Center).Gap(theme.SpacesGap).PaddingX(theme.SidebarPadX-theme.CardInsetX-1).
		MarginX(theme.CardInsetX).Radius(theme.CardRadius).Border(1, border).
		Cursor(ui.CursorPointer).Selected(o.Selected).Transition(FadeTransition(theme.MotionSelect))
	if o.OnClick != nil {
		row = row.OnClick(o.OnClick)
	}
	if o.Selected {
		row = row.Background(p.Selected)
	}
	row.Children(func() {
		if o.Lead != nil {
			o.Lead()
		}
		if o.Mark != "" {
			color := o.MarkColor
			if color == (ui.Color{}) {
				color = p.Ink48
			}
			ui.Text(c, o.Mark).TextColor(color).NoWrap().Shrink(0)
		}
		// SingleLine cuts with an ellipsis; NoWrap would keep the line whole.
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, o.Text).Label(label + " text").Height(theme.LineHeight).TextColor(p.Ink88).SingleLine().MinWidth(0)
			if o.Sub != "" {
				ui.Text(c, o.Sub).Height(theme.LineHeight).TextColor(p.Ink48).SingleLine().MinWidth(0)
			}
		})
		if o.Counts {
			Counts(c, o.Added, o.Removed)
		}
	})
}

// CountsText is the text of Counts, as "+5 −1".
func CountsText(added, removed int) string {
	return "+" + strconv.Itoa(added) + " −" + strconv.Itoa(removed)
}

// Counts shows "+added −removed": added and removed in ink-48
// (docs/spec/DiffSummary.md).
func Counts(c *ui.Context, added, removed int) {
	p := theme.Current()
	CountsWith(c, added, removed, CountsLook{Added: p.Ink48, Removed: p.Ink48, Gap: 4})
}

// CountsLook is how CountsWith draws the counts. Colors come from the
// palette (a token, never a value of a view).
type CountsLook struct {
	Added, Removed ui.Color
	// Gap is the space between the two counts.
	Gap float32
	// Style is the text style; the zero value keeps the window's text.
	Style theme.TextStyle
	// Only drops one count: "+N" only for a new file, "−N" only for a deleted one.
	Only CountsOnly
}

// CountsOnly picks which counts show.
type CountsOnly int

// The values of CountsLook.Only.
const (
	CountsBoth CountsOnly = iota
	CountsAddedOnly
	CountsRemovedOnly
)

// CountsWith shows the counts in the look. The row is named by the counts
// that show, for the screen reader.
func CountsWith(c *ui.Context, added, removed int, look CountsLook) {
	label := CountsText(added, removed)
	switch look.Only {
	case CountsAddedOnly:
		label = "+" + strconv.Itoa(added)
	case CountsRemovedOnly:
		label = "−" + strconv.Itoa(removed)
	}
	text := func(s string, color ui.Color) {
		t := ui.Text(c, s).TextColor(color).NoWrap()
		if look.Style.Size > 0 {
			t.Font(look.Style.Stack()).FontSize(look.Style.Size).FontWeight(look.Style.Weight)
		}
	}
	ui.Row(c).Label(label).Gap(look.Gap).Shrink(0).Children(func() {
		if look.Only != CountsRemovedOnly {
			text("+"+strconv.Itoa(added), look.Added)
		}
		if look.Only != CountsAddedOnly {
			text("−"+strconv.Itoa(removed), look.Removed)
		}
	})
}

// Switch is a segmented control: choice is the index of the chosen option.
// Look: a hover track, radius 7, padding 2; the chosen segment has the
// selected fill and ink-100, the others ink-64. Tokens of the current theme.
// Each segment is labelled with its option.
func Switch(c *ui.Context, label string, choice *int, options ...string) ui.Element {
	p := theme.Current()
	seg := ui.SegmentedBase(c, choice, len(options))
	seg.Track.Label(label).Row().Padding(theme.SegmentPad).Radius(theme.SegmentRadius).
		Background(p.Hover).Children(func() {
		for i, o := range options {
			s := seg.Segment(i).Label(o).PaddingY(3).PaddingX(10).Radius(theme.SegmentRadius - theme.SegmentPad).
				Cursor(ui.CursorPointer).Transition(FadeTransition(theme.MotionSelect))
			ink := p.Ink64
			if choice != nil && i == *choice {
				s, ink = s.Background(p.Selected), p.Ink100
			}
			s = s.TextColor(FadeInk(s, "ink", ink, theme.MotionSelect))
			s.Children(func() { ui.Text(c, o).NoWrap() })
		}
	})
	return seg.Track
}

// NewCode returns the state of a code view of one file: the new text with
// the deleted lines in place (unified), + and − before changed lines, and
// the whole file shown. name picks the syntax highlighting.
func NewCode(name, before, after string) *editor.DiffState {
	lang := editor.LanguageFor(name)
	old, cur := editor.NewBuffer(before), editor.NewBuffer(after)
	old.SetLanguage(lang)
	cur.SetLanguage(lang)
	d := &editor.DiffState{Old: old, New: cur, Layout: editor.DiffUnified, Indicators: editor.DiffClassic,
		Font: ui.Font{Family: theme.FontFamily, Size: theme.FontSize},
		// LineHeight is a multiple of the font size: 18 / 12 = 1.5.
		LineHeight: float32(theme.LineHeight) / theme.FontSize}
	d.ExpandAll()
	return d
}

// SetWhole shows the whole file, or only the changes with a few lines around
// them.
func SetWhole(d *editor.DiffState, whole bool) {
	if whole {
		d.ExpandAll()
	} else {
		d.CollapseAll()
	}
}

// CodeTheme is the editor theme of code views: MyGo's theme for the window,
// with contr's background and diff colors.
func CodeTheme(c *ui.Context) *editor.Theme {
	t := editor.DefaultTheme(c.Theme())
	p := theme.Current()
	t.Background = p.Ground
	// Greys, not hues (AGENTS.md): the editor mixes these into the line
	// backgrounds, so they are flattened over the ground first.
	t.Added = theme.Over(p.Ink48, p.Ground)
	t.Deleted = theme.Over(p.Ink30, p.Ground)
	return t
}

// CodeView builds the code view of d with contr's theme, taken from the
// window's theme on each build, so a change of light or dark shows at once.
func CodeView(c *ui.Context, d *editor.DiffState) ui.Element {
	d.Theme = CodeTheme(c)
	return editor.DiffView(c, d)
}
