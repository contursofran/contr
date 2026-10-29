package terminal

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Helpers shared by the view tests of all M5.7 slices.

// paneID is the pane that a surface from shownSurface names.
const paneID = "p1"

// settled is longer than resizeWait, so a pending size change is sent.
const settled = 200 * time.Millisecond

// fakeLink is a [Link] that records the messages the view queues, as the real
// link would send them. refuse makes every call return that error.
type fakeLink struct {
	msgs   []endpoint.ClientMsg
	refuse error
	latest *endpoint.Surface
}

func (f *fakeLink) SendInput(pane string, events []endpoint.InputEvent) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.msgs = append(f.msgs, endpoint.InputMsg{PaneID: pane, Events: events})
	return nil
}

func (f *fakeLink) Resize(size endpoint.GridSize, cell endpoint.CellPx) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.msgs = append(f.msgs, endpoint.ResizeMsg{Size: size, Cell: cell})
	return nil
}

func (f *fakeLink) SetFocus(focused bool) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.msgs = append(f.msgs, endpoint.FocusMsg{Focused: focused})
	return nil
}

// TakeLatest gives the surface set in latest, once.
func (f *fakeLink) TakeLatest() *endpoint.Surface {
	s := f.latest
	f.latest = nil
	return s
}

// queued returns the messages queued so far, and forgets them.
func (f *fakeLink) queued() []endpoint.ClientMsg {
	m := f.msgs
	f.msgs = nil
	return m
}

// newTestView returns a view in an 800 x 600 tester, with the link off. The
// counter is how often the view asked for a frame.
func newTestView(t *testing.T) (*View, *ui.Tester, *int) {
	t.Helper()
	return newTestViewWith(t, Options{})
}

// newTestViewWith is newTestView with options set before the first frame: the
// link starts on the first layout, so a test that enables it cannot do that
// afterwards. Invalidate is the counter's.
func newTestViewWith(t *testing.T, o Options) (*View, *ui.Tester, *int) {
	t.Helper()
	invalidations := new(int)
	o.Invalidate = func() { *invalidations++ }
	v := NewView(o)
	t.Cleanup(v.Close)
	return v, ui.NewTester(v.Build, 800, 600), invalidations
}

// settle sizes the window to w x h and lets the view settle: the layout runs
// and a size change has waited out resizeWait. Afterwards v.sends holds the
// sizes herdr has.
func settle(tt *ui.Tester, w, h int) {
	tt.SetSize(w, h)
	tt.Frame()
	tt.Advance(settled)
}

// attach gives the view a fake link and a surface of the size herdr has, with
// the focused pane paneID. Nothing is queued when it returns.
func attach(v *View, tt *ui.Tester) *fakeLink {
	size := endpoint.GridSize{Cols: 3, Rows: 2}
	if v.hasSent {
		size = v.sent.size
	}
	f := &fakeLink{}
	v.link = f
	v.acceptSurface(shownSurface(1, size, paneID))
	tt.Frame()
	if q := f.queued(); len(q) != 0 {
		panic("attach queued messages")
	}
	return f
}

// attachPanes gives the view a fake link and panes as the shown surface, at the
// grid size of the last layout.
func attachPanes(v *View, tt *ui.Tester, panes []endpoint.SurfacePane) *fakeLink {
	f := &fakeLink{}
	v.link = f
	v.acceptSurface(shownPanes(1, v.want.size, panes))
	tt.Frame()
	return f
}

func cellText(text string) endpoint.Cell { return endpoint.Cell{Text: text} }

// surface is a 3 x 2 surface whose first cell is first.
func surface(revision uint64, first string) *endpoint.Surface {
	return &endpoint.Surface{
		BootID:             "boot",
		ProjectionRevision: 1,
		Revision:           revision,
		Size:               endpoint.GridSize{Cols: 3, Rows: 2},
		Rows: []endpoint.Row{
			{cellText(first), cellText("b"), cellText("c")},
			{cellText("d"), cellText("e"), cellText("f")},
		},
		Cursor: &endpoint.Cursor{X: 1, Y: 0, Visible: true},
	}
}

// styled is a surface with one row per entry of attrs. Each row is one "a"
// cell with those attributes.
func styled(revision uint64, attrs []uint16) *endpoint.Surface {
	rows := make([]endpoint.Row, len(attrs))
	for i, a := range attrs {
		rows[i] = endpoint.Row{{Text: "a", Attrs: a}}
	}
	return &endpoint.Surface{
		BootID:             "boot",
		ProjectionRevision: 1,
		Revision:           revision,
		Size:               endpoint.GridSize{Cols: 1, Rows: uint16(len(attrs))},
		Rows:               rows,
	}
}

// shownSurface is a surface of size with no rows, whose focused pane is pane
// when pane is not "".
func shownSurface(revision uint64, size endpoint.GridSize, pane string) *endpoint.Surface {
	s := &endpoint.Surface{BootID: "boot", ProjectionRevision: 1, Revision: revision, Size: size}
	if pane != "" {
		s.Panes = []endpoint.SurfacePane{{PaneID: pane, Focused: true}}
	}
	return s
}

// shownPanes is a surface of size with no rows and panes.
func shownPanes(revision uint64, size endpoint.GridSize, panes []endpoint.SurfacePane) *endpoint.Surface {
	return &endpoint.Surface{BootID: "boot", ProjectionRevision: 1, Revision: revision, Size: size, Panes: panes}
}

// gridPane is a focused pane that covers the whole grid of size, with the
// given mouse flags.
func gridPane(id string, size endpoint.GridSize, mouseReporting, alternateScreen bool) endpoint.SurfacePane {
	all := endpoint.Rect{Width: size.Cols, Height: size.Rows}
	return endpoint.SurfacePane{
		PaneID: id, Rect: all, InnerRect: all, Focused: true,
		MouseReporting: mouseReporting, AlternateScreen: alternateScreen,
	}
}

// twoPanesFocused is two panes side by side in a 12 x 4 surface, p1 at the left
// and p2 at the right. Each inner rect leaves a one-cell border. focused names
// the pane herdr focuses, or "" for none.
func twoPanesFocused(revision uint64, focused string) *endpoint.Surface {
	pane := func(id string, x uint16) endpoint.SurfacePane {
		return endpoint.SurfacePane{
			PaneID:    id,
			Rect:      endpoint.Rect{X: x, Width: 6, Height: 4},
			InnerRect: endpoint.Rect{X: x + 1, Y: 1, Width: 4, Height: 2},
			Focused:   focused == id,
		}
	}
	return &endpoint.Surface{
		BootID: "boot", ProjectionRevision: 1, Revision: revision,
		Size:  endpoint.GridSize{Cols: 12, Rows: 4},
		Panes: []endpoint.SurfacePane{pane("p1", 0), pane("p2", 6)},
	}
}

// page is a surface the size of the view's grid with one pane (paneID) whose
// inner rect is inner. lines sit at the top, padded with blanks to the grid's
// width.
func page(v *View, revision uint64, lines []string, inner endpoint.Rect) *endpoint.Surface {
	size := v.want.size
	blank := func() endpoint.Row {
		row := make(endpoint.Row, size.Cols)
		for i := range row {
			row[i] = cellText(" ")
		}
		return row
	}
	rows := make([]endpoint.Row, 0, size.Rows)
	for _, line := range lines {
		row := blank()
		for i, r := range []rune(line) {
			row[i] = cellText(string(r))
		}
		rows = append(rows, row)
	}
	for len(rows) < int(size.Rows) {
		rows = append(rows, blank())
	}
	return &endpoint.Surface{
		BootID: "boot", ProjectionRevision: 1, Revision: revision, Size: size, Rows: rows,
		Panes: []endpoint.SurfacePane{{
			PaneID:    paneID,
			Rect:      endpoint.Rect{Width: size.Cols, Height: size.Rows},
			InnerRect: inner,
			Focused:   true,
		}},
	}
}

// fullPage is a page whose single pane holds every cell.
func fullPage(v *View, revision uint64, lines []string) *endpoint.Surface {
	size := v.want.size
	return page(v, revision, lines, endpoint.Rect{Width: size.Cols, Height: size.Rows})
}

// cellPoint is the window point a fraction of the way across cell (col, row),
// at its mid height, as laid out by the last frame. Below a half is the cell's
// left half, and above it the right one.
func cellPoint(v *View, tt *ui.Tester, col, row int, fraction float32) (x, y float32) {
	grid, _ := tt.Find(LabelGrid)
	m := v.metrics
	return grid.X + m.Pad + (float32(col)+fraction)*m.CellW, grid.Y + m.Pad + (float32(row)+0.5)*m.CellH
}

// drag drags from the left part of one cell to the right part of another and
// releases.
func drag(v *View, tt *ui.Tester, from, to [2]int) {
	x0, y0 := cellPoint(v, tt, from[0], from[1], 0.2)
	x1, y1 := cellPoint(v, tt, to[0], to[1], 0.8)
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	tt.Release(x1, y1)
}

// liveState is a connected link state.
var liveState = endpoint.LinkState{Kind: endpoint.LinkLive, ServerVersion: "0.9.3"}
