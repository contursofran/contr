package kit

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// DiffSummaryOpts describes a DiffSummary (docs/spec/DiffSummary.md).
type DiffSummaryOpts struct {
	Added, Removed, Files int
	// OnClick opens the Review view.
	OnClick func()
}

// Hidden tells that the summary shows nothing: no changed line and no file.
func (o DiffSummaryOpts) Hidden() bool { return o.Added == 0 && o.Removed == 0 && o.Files == 0 }

// DiffSummaryParts are the texts of the three counts, "+120", "−126" and "9 files". The minus
// is U+2212. The removed count is "" when there is none: only additions.
func DiffSummaryParts(o DiffSummaryOpts) (added, removed, files string) {
	added = "+" + strconv.Itoa(o.Added)
	if o.Removed > 0 {
		removed = "−" + strconv.Itoa(o.Removed)
	}
	return added, removed, plural(o.Files, "file")
}

// DiffSummaryLabel is the screen-reader label: "120 lines added, 126 removed, 9 files. Open review".
func DiffSummaryLabel(o DiffSummaryOpts) string {
	s := plural(o.Added, "line") + " added"
	if o.Removed > 0 {
		s += ", " + strconv.Itoa(o.Removed) + " removed"
	}
	return s + ", " + plural(o.Files, "file") + ". Open review"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// DiffSummary builds the title bar button that counts the changed lines and files of the
// project. It builds nothing when there is no change. The counts are greys, not diff colors:
// the diff colors belong to the diff view. It is a ButtonBase, so it keeps the keyboard (Space
// and Enter), the focus and the screen-reader label.
func DiffSummary(c *ui.Context, o DiffSummaryOpts) {
	if o.Hidden() {
		return
	}
	p := theme.Current()
	added, removed, files := DiffSummaryParts(o)
	b := ui.ButtonBase(c).Label(DiffSummaryLabel(o)).Shrink(0).Padding(theme.Space4, theme.Space8).
		Radius(theme.Radius7).Gap(theme.Space8).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if o.OnClick != nil {
		b = b.OnClick(o.OnClick)
	}
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b.Children(func() {
		value(ui.Text(c, added)).TextColor(p.Ink48)
		if removed != "" {
			value(ui.Text(c, removed)).TextColor(p.Ink48)
		}
		value(ui.Text(c, files)).TextColor(p.Ink48)
	})
}

// value gives a text the "value" style: mono 11.5 on a 16 line (docs/spec/tokens.md).
func value(t ui.Element) ui.Element {
	s := theme.StyleValue
	return t.Font(s.Stack()).FontSize(s.Size).FixedLineHeight(s.Line).FontWeight(s.Weight).NoWrap()
}
