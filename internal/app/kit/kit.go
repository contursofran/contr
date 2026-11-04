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
	MarkColor uint32 // theme.TextDim by default
	// Text is the main line; Sub, if set, a second line in the meta color.
	// Both clip with an ellipsis.
	Text, Sub string
	// Counts shows +Added -Removed at the end.
	Counts         bool
	Added, Removed int
	Selected       bool
	OnClick        func()
	// Lead draws a leading part, such as a status icon, before Mark.
	Lead func()
}

// HeightOf is the height Row gives to a row with these options.
func HeightOf(o RowOpts) float32 {
	if o.Sub != "" {
		return 2*theme.LineHeight + 6
	}
	return RowHeight
}

// Row builds a selectable list row, as the sidebar's rows look: a card
// background and border when selected, the same border color as the
// background otherwise, so selecting moves nothing.
func Row(c *ui.Context, o RowOpts) {
	label := o.Label
	if label == "" {
		label = o.Text
	}
	height := HeightOf(o)
	border := uint32(theme.BgWindow)
	if o.Selected {
		border = theme.CardBorder
	}
	row := ui.ButtonBase(c).Label(label).Height(height).Shrink(0).
		AlignItems(ui.Center).Gap(theme.SpacesGap).PaddingX(theme.SidebarPadX-theme.CardInsetX-1).
		MarginX(theme.CardInsetX).Radius(theme.CardRadius).Border(1, theme.Color(border)).
		Cursor(ui.CursorPointer).Selected(o.Selected)
	if o.OnClick != nil {
		row = row.OnClick(o.OnClick)
	}
	if o.Selected {
		row = row.Background(theme.Color(theme.BgCard))
	}
	row.Children(func() {
		if o.Lead != nil {
			o.Lead()
		}
		if o.Mark != "" {
			color := o.MarkColor
			if color == 0 {
				color = theme.TextDim
			}
			ui.Text(c, o.Mark).TextColor(theme.Color(color)).NoWrap().Shrink(0)
		}
		// SingleLine cuts with an ellipsis; NoWrap would keep the line whole.
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, o.Text).Label(label + " text").Height(theme.LineHeight).TextColor(theme.Color(theme.Text)).SingleLine().MinWidth(0)
			if o.Sub != "" {
				ui.Text(c, o.Sub).Height(theme.LineHeight).TextColor(theme.Color(theme.TextDim2)).SingleLine().MinWidth(0)
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

// Counts shows "+added −removed" in the diff colors.
func Counts(c *ui.Context, added, removed int) {
	ui.Row(c).Label(CountsText(added, removed)).Gap(4).Shrink(0).Children(func() {
		ui.Text(c, "+"+strconv.Itoa(added)).TextColor(theme.Color(theme.DiffAdded)).NoWrap()
		ui.Text(c, "−"+strconv.Itoa(removed)).TextColor(theme.Color(theme.DiffDeleted)).NoWrap()
	})
}

// Switch is a segmented control: choice is the index of the chosen option.
// Look (DESIGN.md): a #1A1A1A track, radius 7, padding 2; the chosen segment
// has a #2A2A2A background and the primary text, the others secondary text.
// Each segment is labelled with its option.
func Switch(c *ui.Context, label string, choice *int, options ...string) ui.Element {
	seg := ui.SegmentedBase(c, choice, len(options))
	seg.Track.Label(label).Row().Padding(theme.SegmentPad).Radius(theme.SegmentRadius).
		Background(theme.Color(theme.SegmentTrack)).Children(func() {
		for i, o := range options {
			s := seg.Segment(i).Label(o).PaddingY(3).PaddingX(10).Radius(theme.SegmentRadius - theme.SegmentPad).
				Cursor(ui.CursorPointer).TextColor(theme.Color(theme.Text2))
			if choice != nil && i == *choice {
				s = s.Background(theme.Color(theme.SegmentSelected)).TextColor(theme.Color(theme.Text))
			}
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
	t.Background = theme.Color(theme.BgWindow)
	t.Added = theme.Color(theme.DiffAdded)
	t.Deleted = theme.Color(theme.DiffDeleted)
	return t
}

// CodeView builds the code view of d with contr's theme, taken from the
// window's theme on each build, so a change of light or dark shows at once.
func CodeView(c *ui.Context, d *editor.DiffState) ui.Element {
	d.Theme = CodeTheme(c)
	return editor.DiffView(c, d)
}
