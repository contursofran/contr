package terminal

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// wheelView is a settled view with a link and one pane over the whole grid.
func wheelView(t *testing.T, mouse bool) (*View, *ui.Tester, *fakeLink) {
	t.Helper()
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := attachPanes(v, tt, []endpoint.SurfacePane{gridPane(paneID, v.want.size, mouse, false)})
	f.queued()
	return v, tt, f
}

func wheelMsg(pane string, events ...endpoint.InputEvent) endpoint.ClientMsg {
	return endpoint.InputMsg{PaneID: pane, Events: events}
}

func wantMsgs(t *testing.T, f *fakeLink, want ...endpoint.ClientMsg) {
	t.Helper()
	if got := f.queued(); !reflect.DeepEqual(got, want) {
		t.Errorf("queued = %#v, want %#v", got, want)
	}
}

// relative turns a window point into the grid-relative point of an event.
func relative(tt *ui.Tester, x, y float32) (float32, float32) {
	grid, _ := tt.Find(LabelGrid)
	return x - grid.X, y - grid.Y
}

func TestATrackpadSwipeAddsUpToWholeLines(t *testing.T) {
	v, tt, f := wheelView(t, false)
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	dy := -0.6 * v.metrics.CellH
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f)
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelUp, 1, 1, 1)))
	// The rest (0.2 line) is kept.
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f)
	// Swiping back drops the rest and scrolls down.
	tt.Scroll(x, y, 0, -dy)
	tt.Scroll(x, y, 0, -dy)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelDown, 1, 1, 1)))
}

func TestAWheelNotchScrollsThreeLinesInAShell(t *testing.T) {
	v, tt, f := wheelView(t, false)
	x, y := cellPoint(v, tt, 0, 0, 0.5)
	tt.Wheel(x, y, 0, -40)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelUp, 0, 0, 3)))
	tt.Wheel(x, y, 0, 40)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelDown, 0, 0, 3)))
}

func TestAProgramWithMouseReportsGetsOneEventPerNotch(t *testing.T) {
	v, tt, f := wheelView(t, true)
	x, y := cellPoint(v, tt, 2, 1, 0.5)
	tt.Wheel(x, y, 0, -40)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelUp, 2, 1, 3)))
	// Two notches in one event are two wheel events.
	tt.Wheel(x, y, 0, -80)
	e := wheelEvent(endpoint.WheelUp, 2, 1, 3)
	wantMsgs(t, f, wheelMsg(paneID, e, e))
}

func TestTheWheelGoesToThePaneUnderThePointerAtItsCell(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := attachPanes(v, tt, twoPanesFocused(1, "p1").Panes)
	f.queued()
	// Cell (8, 2) is in p2, whose inner rect starts at (7, 1).
	x, y := cellPoint(v, tt, 8, 2, 0.5)
	tt.Wheel(x, y, 0, -40)
	wantMsgs(t, f, wheelMsg("p2", wheelEvent(endpoint.WheelUp, 1, 1, 3)))
	// Cell (2, 0) is in p1's border: the cell clamps to its inner rect.
	x, y = cellPoint(v, tt, 2, 0, 0.5)
	tt.Wheel(x, y, 0, -40)
	wantMsgs(t, f, wheelMsg("p1", wheelEvent(endpoint.WheelUp, 1, 0, 3)))
}

func TestASidewaysSwipeScrollsAProgramButNotAShell(t *testing.T) {
	v, tt, f := wheelView(t, true)
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	tt.Scroll(x, y, -3*v.metrics.CellW, 0)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelLeft, 1, 1, 3)))
	tt.Scroll(x, y, 3*v.metrics.CellW, 0)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelRight, 1, 1, 3)))

	v, tt, f = wheelView(t, false)
	x, y = cellPoint(v, tt, 1, 1, 0.5)
	tt.Scroll(x, y, -3*v.metrics.CellW, 0)
	wantMsgs(t, f)
	// The scroll is still the terminal's.
	rx, ry := relative(tt, x, y)
	if !v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DX: -1, Precise: true}) {
		t.Error("a sideways scroll over a shell was not taken")
	}
}

func TestAWheelWithNoLinkOrOutsideThePanesSendsNothing(t *testing.T) {
	// The mods ride along: Shift is a flag on the event.
	v, tt, f := wheelView(t, true)
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	rx, ry := relative(tt, x, y)
	ev := ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40, Mods: ui.Shift}
	if !v.onScroll(ev) {
		t.Error("scroll over a pane was not taken")
	}
	wantMsgs(t, f, wheelMsg(paneID, endpoint.WheelEvent{Dir: endpoint.WheelUp, Column: 1, Row: 1, Mods: endpoint.ModShift, Lines: 3}))

	// A pane that covers part of the grid: elsewhere there is no pane.
	small := gridPane(paneID, endpoint.GridSize{Cols: 2, Rows: 2}, false, false)
	v.acceptSurface(shownPanes(2, v.want.size, []endpoint.SurfacePane{small}))
	ox, oy := cellPoint(v, tt, 4, 4, 0.5)
	rx, ry = relative(tt, ox, oy)
	if v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40}) {
		t.Error("scroll outside the panes was taken")
	}
	// The padding is no cell.
	if v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: 0, Y: 0, DY: -40}) {
		t.Error("scroll in the padding was taken")
	}
	wantMsgs(t, f)

	// No surface.
	v.surface = nil
	if v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40}) {
		t.Error("scroll with no surface was taken")
	}
	// No link.
	v.acceptSurface(shownPanes(9, v.want.size, []endpoint.SurfacePane{gridPane(paneID, v.want.size, false, false)}))
	v.link = nil
	x, y = cellPoint(v, tt, 1, 1, 0.5)
	rx, ry = relative(tt, x, y)
	if v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40}) {
		t.Error("scroll with no link was taken")
	}
	// No cell metrics.
	v.link = f
	v.hasMetrics = false
	if v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40}) {
		t.Error("scroll with no metrics was taken")
	}
	wantMsgs(t, f)
}

func TestAWheelTheLinkRefusesIsStillTaken(t *testing.T) {
	v, tt, f := wheelView(t, false)
	f.refuse = endpoint.ErrLinkNotConnected
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	rx, ry := relative(tt, x, y)
	if !v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: -40}) {
		t.Error("a refused scroll was not taken")
	}
	wantMsgs(t, f)
}

func TestANewGestureDropsTheOldRemainder(t *testing.T) {
	v, tt, f := wheelView(t, false)
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	dy := -0.6 * v.metrics.CellH
	tt.Scroll(x, y, 0, dy)
	tt.Advance(gestureGap)
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f)

	// A pause shorter than the gap is the same gesture.
	tt.Advance(gestureGap / 2)
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelUp, 1, 1, 1)))

	// Momentum after the gap is the same gesture: it keeps the remainder.
	rx, ry := relative(tt, x, y)
	t0 := time.Unix(100, 0)
	scroll := func(at time.Time, momentum bool) {
		v.onScroll(ui.InputEvent{Kind: ui.InputScroll, X: rx, Y: ry, DY: dy, Precise: true, Momentum: momentum, Time: at})
	}
	v.wheel.Clear()
	scroll(t0, false)
	scroll(t0.Add(gestureGap), true)
	wantMsgs(t, f, wheelMsg(paneID, wheelEvent(endpoint.WheelUp, 1, 1, 1)))
	// A touch after the gap that is not momentum starts over.
	scroll(t0.Add(2*gestureGap), false)
	wantMsgs(t, f)
}

func TestALostLinkForgetsTheWheelRemainder(t *testing.T) {
	v, tt, f := wheelView(t, false)
	x, y := cellPoint(v, tt, 1, 1, 0.5)
	dy := -0.6 * v.metrics.CellH
	tt.Scroll(x, y, 0, dy)
	v.setState(endpoint.LinkState{Kind: endpoint.LinkNotRunning})
	f = attachPanes(v, tt, []endpoint.SurfacePane{gridPane(paneID, v.want.size, false, false)})
	f.queued()
	tt.Scroll(x, y, 0, dy)
	wantMsgs(t, f)
}
