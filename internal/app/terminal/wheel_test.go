package terminal

import (
	"math"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

var (
	nanF  = float32(math.NaN())
	pinfF = float32(math.Inf(1))
)

// wheelCell has 8 pixels of padding.
var wheelCell = Metrics{CellW: 10, CellH: 20, Pad: 8}

func wheelPane(id string, mouse, alt bool) endpoint.SurfacePane {
	return endpoint.SurfacePane{
		PaneID:          id,
		Rect:            endpoint.Rect{X: 0, Y: 0, Width: 6, Height: 4},
		InnerRect:       endpoint.Rect{X: 1, Y: 1, Width: 4, Height: 2},
		MouseReporting:  mouse,
		AlternateScreen: alt,
	}
}

func wheelSurface() *endpoint.Surface {
	p2 := wheelPane("p2", false, false)
	p2.Rect.X = 6
	p2.InnerRect.X = 7
	return &endpoint.Surface{
		BootID:             "boot",
		ProjectionRevision: 1,
		Revision:           1,
		Size:               endpoint.GridSize{Cols: 12, Rows: 4},
		Panes:              []endpoint.SurfacePane{wheelPane("p1", false, false), p2},
	}
}

func wheelEvent(dir endpoint.WheelDir, col, row, lines uint16) endpoint.InputEvent {
	return endpoint.WheelEvent{Dir: dir, Column: col, Row: row, Lines: lines}
}

func close32(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-5 }

func TestDeltaLinesReadsPixelsAndLines(t *testing.T) {
	for _, c := range []struct {
		d    Delta
		want float32
	}{
		{Delta{Y: 40, Pixels: true}, 2},
		{Delta{Y: -10, Pixels: true}, -0.5},
		{Delta{Y: 1}, 3},
		{Delta{Y: -2}, -6},
	} {
		if got := DeltaLines(c.d, 20); !close32(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.d, got, c.want)
		}
	}
}

func TestDeltaLinesWithoutACellHeightIsZero(t *testing.T) {
	d := Delta{Y: 40, Pixels: true}
	for _, h := range []float32{0, -20, nanF, pinfF} {
		if got := DeltaLines(d, h); got != 0 {
			t.Errorf("cell height %v: %v", h, got)
		}
	}
	if got := DeltaLines(Delta{X: nanF, Y: pinfF, Pixels: true}, 20); got != 0 {
		t.Errorf("broken delta: %v", got)
	}
}

func TestDeltaColumnsReadsPixelsAndLines(t *testing.T) {
	if got := DeltaColumns(Delta{X: 30, Pixels: true}, 10); !close32(got, 3) {
		t.Errorf("pixels: %v", got)
	}
	if got := DeltaColumns(Delta{X: -1}, 10); !close32(got, -3) {
		t.Errorf("lines: %v", got)
	}
	if got := DeltaColumns(Delta{X: 1}, 0); got != 0 {
		t.Errorf("no cell width: %v", got)
	}
}

func TestASidewaysEventIsMostlyHorizontal(t *testing.T) {
	if !Sideways(Delta{X: 2, Y: 1}) {
		t.Error("2,1 is sideways")
	}
	if Sideways(Delta{X: 1, Y: 2}) {
		t.Error("1,2 is not sideways")
	}
	if Sideways(Delta{X: 1, Y: 1}) {
		t.Error("1,1 is not sideways")
	}
	if !Sideways(Delta{X: -30, Y: 10, Pixels: true}) {
		t.Error("-30,10 px is sideways")
	}
	if Sideways(Delta{X: nanF, Pixels: true}) {
		t.Error("NaN is not sideways")
	}
}

func TestATrackpadRemainderAddsUpToWholeLines(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 0.6)
	if got := w.Take(1); got != 0 {
		t.Errorf("first take: %d", got)
	}
	if !close32(w.lines, 0.6) {
		t.Errorf("lines %v", w.lines)
	}
	w.Add("p1", 0.6)
	if got := w.Take(1); got != 1 {
		t.Errorf("second take: %d", got)
	}
	if !close32(w.lines, 0.2) {
		t.Errorf("lines %v", w.lines)
	}
	// The kept fifth of a line finishes the next one.
	w.Add("p1", 0.8)
	if got := w.Take(1); got != 1 {
		t.Errorf("third take: %d", got)
	}
	if !close32(w.lines, 0) {
		t.Errorf("lines %v", w.lines)
	}
}

func TestAChangeOfDirectionDropsTheRemainder(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 0.9)
	w.Add("p1", -0.4)
	// Only the motion after the turn counts.
	if !close32(w.lines, -0.4) {
		t.Errorf("lines %v", w.lines)
	}
	if got := w.Take(1); got != 0 {
		t.Errorf("take: %d", got)
	}
	w.Add("p1", -0.6)
	if got := w.Take(1); got != -1 {
		t.Errorf("take: %d", got)
	}
}

func TestAChangeOfPaneDropsTheRemainder(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 0.9)
	w.Add("p2", 0.4)
	if got := w.Take(1); got != 0 {
		t.Errorf("take: %d", got)
	}
	if !close32(w.lines, 0.4) {
		t.Errorf("lines %v", w.lines)
	}
}

func TestTakeCountsWholeUnitsTowardZero(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 7)
	if got := w.Take(3); got != 2 {
		t.Errorf("take: %d", got)
	}
	if !close32(w.lines, 1) {
		t.Errorf("lines %v", w.lines)
	}
	if got := w.Take(3); got != 0 {
		t.Errorf("take: %d", got)
	}

	w.Add("p1", -7)
	if got := w.Take(3); got != -2 {
		t.Errorf("take: %d", got)
	}
	if !close32(w.lines, -1) {
		t.Errorf("lines %v", w.lines)
	}
}

func TestTakeWithoutAUnitKeepsTheRemainder(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 2)
	for _, unit := range []float32{0, -1, nanF} {
		if got := w.Take(unit); got != 0 {
			t.Errorf("unit %v: %d", unit, got)
		}
		if !close32(w.lines, 2) {
			t.Errorf("unit %v: lines %v", unit, w.lines)
		}
	}
	w.Clear()
	if got := w.Take(1); got != 0 {
		t.Errorf("after clear: %d", got)
	}
}

func TestColumnsAreKeptApartFromLines(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 1.5)
	w.AddColumns("p1", 1.5)
	if got := w.Take(3); got != 0 {
		t.Errorf("take: %d", got)
	}
	if got := w.TakeColumns(3); got != 0 {
		t.Errorf("take columns: %d", got)
	}
	w.AddColumns("p1", 1.5)
	if got := w.TakeColumns(3); got != 1 {
		t.Errorf("take columns: %d", got)
	}
	if !close32(w.lines, 1.5) {
		t.Errorf("lines %v", w.lines)
	}
}

func TestClearDropsBothRemainders(t *testing.T) {
	var w WheelAccum
	w.Add("p1", 2.5)
	w.AddColumns("p1", 2.5)
	w.Clear()
	if w.Take(1) != 0 || w.TakeColumns(1) != 0 {
		t.Error("clear kept a remainder")
	}
	w.Add("p1", 0.5)
	if got := w.Take(1); got != 0 {
		t.Errorf("take: %d", got)
	}
}

func cellAtGrid(x, y float32) (uint16, uint16, bool) {
	return CellAt([2]float32{x, y}, [2]float32{100, 50}, wheelCell, endpoint.GridSize{Cols: 12, Rows: 4})
}

func TestCellAtTakesThePaddingOffThePoint(t *testing.T) {
	// The grid starts at (100, 50); the padding is 8 pixels.
	for _, c := range []struct {
		x, y     float32
		col, row uint16
	}{
		{108, 58, 0, 0},
		// The last pixel of a cell belongs to it; the next one starts the cell after.
		{117.9, 77.9, 0, 0},
		{118, 78, 1, 1},
		{108 + 110, 58 + 20, 11, 1},
	} {
		col, row, ok := cellAtGrid(c.x, c.y)
		if !ok || col != c.col || row != c.row {
			t.Errorf("(%v, %v): %d, %d, %v", c.x, c.y, col, row, ok)
		}
	}
}

func TestCellAtIsNoneInThePaddingAndOutsideTheGrid(t *testing.T) {
	for _, p := range [][2]float32{
		{100, 58}, {107.9, 58}, {108 + 120, 58}, {108, 58 + 80}, {nanF, 58}, {108, pinfF},
	} {
		if _, _, ok := cellAtGrid(p[0], p[1]); ok {
			t.Errorf("(%v, %v) gave a cell", p[0], p[1])
		}
	}
}

func TestCellAtWithoutACellSizeIsNone(t *testing.T) {
	size := endpoint.GridSize{Cols: 12, Rows: 4}
	noWidth := wheelCell
	noWidth.CellW = 0
	nanHeight := wheelCell
	nanHeight.CellH = nanF
	for _, m := range []Metrics{noWidth, nanHeight} {
		if _, _, ok := CellAt([2]float32{108, 58}, [2]float32{100, 50}, m, size); ok {
			t.Errorf("%+v gave a cell", m)
		}
	}
}

func TestPaneAtHoldsTheScrollbarColumn(t *testing.T) {
	s := wheelSurface()
	for _, c := range []struct {
		col, row uint16
		want     string
	}{
		// p1's last rect cell is column 5, its last inner column 4.
		{5, 2, "p1"}, {4, 2, "p1"},
		// Column 6 is p2's rect, column 7 its first inner column.
		{6, 2, "p2"}, {7, 2, "p2"},
		// Below the panes.
		{2, 4, ""},
	} {
		got := ""
		if p := PaneAtCell(s, c.col, c.row); p != nil {
			got = p.PaneID
		}
		if got != c.want {
			t.Errorf("(%d, %d): %q, want %q", c.col, c.row, got, c.want)
		}
	}
}

func TestPaneCellIsRelativeToTheInnerRectAndClamped(t *testing.T) {
	s := wheelSurface()
	p1 := &s.Panes[0]
	for _, c := range []struct{ col, row, wantCol, wantRow uint16 }{
		{1, 1, 0, 0},
		{4, 2, 3, 1},
		// The border and the scrollbar column clamp to the first and last inner cells.
		{0, 0, 0, 0},
		{5, 3, 3, 1},
		{math.MaxUint16, math.MaxUint16, 3, 1},
	} {
		col, row := PaneCell(p1, c.col, c.row)
		if col != c.wantCol || row != c.wantRow {
			t.Errorf("(%d, %d): (%d, %d), want (%d, %d)", c.col, c.row, col, row, c.wantCol, c.wantRow)
		}
	}
}

func TestAHistoryPaneGetsOneEventWithTheWholeCount(t *testing.T) {
	p := wheelPane("p1", false, false)
	want := []endpoint.InputEvent{endpoint.WheelEvent{Dir: endpoint.WheelUp, Column: 2, Row: 3, Mods: endpoint.ModShift, Lines: 4}}
	if got := WheelEvents(&p, 2, 3, 4, endpoint.ModShift); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	// The event is capped well below herdr's batch limit; the excess is dropped.
	want = []endpoint.InputEvent{wheelEvent(endpoint.WheelUp, 0, 0, MaxWheelLines)}
	if got := WheelEvents(&p, 0, 0, 700, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	want = []endpoint.InputEvent{wheelEvent(endpoint.WheelDown, 0, 0, 4)}
	if got := WheelEvents(&p, 0, 0, -4, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := WheelEvents(&p, 0, 0, 0, 0); len(got) != 0 {
		t.Errorf("zero steps: %v", got)
	}
}

func TestAReportingPaneGetsOneEventPerNotch(t *testing.T) {
	notch := endpoint.WheelEvent{Dir: endpoint.WheelUp, Column: 2, Row: 3, Mods: endpoint.ModControl, Lines: 3}
	for _, p := range []endpoint.SurfacePane{wheelPane("p1", true, false), wheelPane("p1", false, true)} {
		want := []endpoint.InputEvent{notch, notch}
		if got := WheelEvents(&p, 2, 3, 2, endpoint.ModControl); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
		want = []endpoint.InputEvent{wheelEvent(endpoint.WheelDown, 0, 0, 3)}
		if got := WheelEvents(&p, 0, 0, -1, 0); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
		// One batch at most, whatever the count.
		if got := WheelEvents(&p, 0, 0, 100, 0); len(got) != MaxWheelEvents {
			t.Errorf("batch of %d", len(got))
		}
	}
}

func TestAHorizontalWheelNeedsAPaneThatReportsTheMouse(t *testing.T) {
	reporting := wheelPane("p1", true, false)
	want := []endpoint.InputEvent{wheelEvent(endpoint.WheelLeft, 2, 3, 3), wheelEvent(endpoint.WheelLeft, 2, 3, 3)}
	if got := WheelColumnEvents(&reporting, 2, 3, 2, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	want = []endpoint.InputEvent{wheelEvent(endpoint.WheelRight, 0, 0, 3), wheelEvent(endpoint.WheelRight, 0, 0, 3)}
	if got := WheelColumnEvents(&reporting, 0, 0, -2, 0); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := WheelColumnEvents(&reporting, 0, 0, 0, 0); len(got) != 0 {
		t.Errorf("zero steps: %v", got)
	}
	history := wheelPane("p1", false, false)
	if got := WheelColumnEvents(&history, 0, 0, 2, 0); len(got) != 0 {
		t.Errorf("history pane: %v", got)
	}
}

func TestWheelModsCarriesShiftControlAndAlt(t *testing.T) {
	if got := WheelMods(0); got != 0 {
		t.Errorf("none: %d", got)
	}
	if got := WheelMods(ui.Shift); got != endpoint.ModShift {
		t.Errorf("shift: %d", got)
	}
	if got := WheelMods(ui.Shift | ui.Ctrl | ui.Alt); got != endpoint.ModShift|endpoint.ModControl|endpoint.ModAlt {
		t.Errorf("all: %d", got)
	}
	// Super is not a wheel modifier.
	if got := WheelMods(ui.Super); got != 0 {
		t.Errorf("super: %d", got)
	}
}
