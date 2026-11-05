package app

// Search in the terminal (docs/spec/Terminal.md, D130): ⌘F or the title bar button opens MyGo's
// FindBar over the terminal text. The plugin does the search: it reads its own screen and
// scrollback, highlights the matches, scrolls to the selected one and goes through a long
// scrollback in the frames after, a few milliseconds at a time (Terminal.Search). Nothing goes to
// hc, so the terminal's program sees no input. The methods run on the UI thread.

import (
	"strconv"

	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the search parts.
const (
	// LabelSearch names the title bar button.
	LabelSearch = "Search"
	// LabelFindLayer names the find bar layer of the terminal overlay.
	LabelFindLayer = "find bar layer"
	// LabelFind is the label MyGo gives its find bar.
	LabelFind = "Find"
)

// termFind is the state of the find bar.
type termFind struct {
	// open tells that the bar shows; query is its field.
	open  bool
	query string
	// searched is the query the terminal searches for now; "" when none.
	searched string
	// shown tells that a bar was built since the last end, so a search may run.
	shown bool
	// status is how the terminal says its search goes (Options.OnSearch).
	status gterm.SearchStatus
	// gen changes each time ⌘F asks for a bar that shows already: a new key resets the
	// bar, which then takes the focus and selects its query.
	gen int
	// refocus asks the terminal view to take the focus back.
	refocus bool
}

// openFind shows the find bar. While it shows, ⌘F gives its field the focus again. It does
// nothing without a terminal on screen.
func (f *Frame) openFind() {
	if f.term == nil || f.reviewOpen {
		return
	}
	if f.find.open {
		f.find.gen++
	}
	f.find.open = true
	if f.invalidate != nil {
		f.invalidate()
	}
}

// endFind ends the search and gives the focus back to the terminal. It does nothing when no
// bar was shown.
func (f *Frame) endFind() {
	if !f.find.shown {
		return
	}
	f.find.shown, f.find.searched, f.find.status = false, "", gterm.SearchStatus{}
	if f.term != nil {
		f.term.EndSearch()
		f.find.refocus = true
	}
}

// resetFind drops the bar and its search with the terminal it searched. The terminal is closed
// already, so only the state goes.
func (f *Frame) resetFind() {
	f.find = termFind{gen: f.find.gen}
}

// onSearch takes the terminal's search status, on the UI thread.
func (f *Frame) onSearch(s gterm.SearchStatus) {
	f.find.status = s
	if f.invalidate != nil {
		f.invalidate()
	}
}

// findView is the layer of the terminal overlay that holds ⌘F and the find bar. It builds
// nothing while the bar is closed, so it takes no key from the terminal.
func (f *Frame) findView(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		f.openFind()
	}
	fd := &f.find
	if !fd.open || f.term == nil {
		f.endFind()
		return
	}
	fd.shown = true
	if fd.query != fd.searched {
		f.term.Search(fd.query)
		fd.searched = fd.query
	}
	// The status is the terminal's for the query that shows; "Selected" counts from the newest.
	n, cur := 0, 0
	if fd.query != "" && fd.status.Text == fd.query {
		n, cur = fd.status.Total, max(fd.status.Selected-1, 0)
	}
	was := cur
	p := theme.Current()
	var bar ui.Element
	ui.Row(c.Key("find" + strconv.Itoa(fd.gen))).Absolute().Top(theme.Space10).Left(0).Right(0).
		PaddingEnd(theme.Space14).Justify(ui.End).PassThrough().Children(func() {
		bar = ui.FindBar(c, &fd.open, &fd.query, n, &cur)
		bar.Radius(theme.Radius12).Background(p.GlassFloat).
			BorderWidth(1).BorderColor(p.FloatLine)
	})
	if bar.Changed() && n > 0 {
		// The bar steps by one and wraps. A step up (+1) is the next match; one down is the previous
		// match, which is a step of n-1 once there are more than two.
		if d := (cur - was + n) % n; n > 2 && d == n-1 {
			f.term.SearchPrevious()
		} else {
			f.term.SearchNext()
		}
	}
}
