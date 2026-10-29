// Adapted from Herdr UI (Apache-2.0); changes: ported to Go, cell and pane lookup on the surface grid, one
// event per notch carrying the notch's lines, horizontal motion only to programs that report the
// mouse, unit tests.

package terminal

import (
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Wheel and trackpad motion to herdr input events. herdr keeps the scrollback:
// contr sends only wheel steps, and the server decides whether they scroll its
// history or reach the program.
//
// Everything here works on plain numbers, so the mapping is unit tested
// without a window.

const (
	// WheelLines is the lines one wheel notch scrolls in herdr's history
	// (D88). herdr turns a wheel event for a program into one mouse report or
	// one arrow key and ignores the count there.
	WheelLines float32 = 3

	// MaxWheelLines is the most lines one event may carry. herdr closes the
	// connection when a batch asks for more than 4096 lines in all, so this
	// stays well below that.
	MaxWheelLines = 512

	// MaxWheelEvents is the most events one batch may carry for a program,
	// which gets one event per notch. At WheelLines each, these are 192 lines
	// at most.
	MaxWheelEvents = 64
)

// Delta is the motion of one scroll event. Up and left are positive. Pixels
// is true for a trackpad, which reports pixels; a mouse wheel reports lines.
type Delta struct {
	X, Y   float32
	Pixels bool
}

func finite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// DeltaLines returns the lines a Delta asks for, up being positive. A
// trackpad reports pixels, one cell height per line; a mouse wheel reports
// lines. It returns 0 when the cell height is not a positive, finite number.
func DeltaLines(d Delta, cellH float32) float32 {
	if !finite(cellH) || cellH <= 0 {
		return 0
	}
	lines := d.Y * WheelLines
	if d.Pixels {
		lines = d.Y / cellH
	}
	if !finite(lines) {
		return 0
	}
	return lines
}

// DeltaColumns returns the columns a Delta asks for, left being positive, as
// DeltaLines computes lines.
func DeltaColumns(d Delta, cellW float32) float32 {
	columns := d.X * WheelLines
	if d.Pixels {
		columns = d.X / cellW
	}
	if finite(columns) && finite(cellW) && cellW > 0 {
		return columns
	}
	return 0
}

// Sideways reports whether the event moves mostly sideways. A sideways delta
// is a horizontal wheel and not just drift; a mostly vertical swipe's sideways
// drift is not a horizontal step.
func Sideways(d Delta) bool {
	x, y := float64(d.X), float64(d.Y)
	return finite(d.X) && finite(d.Y) && math.Abs(x) > math.Abs(y)
}

// WheelAccum is the sub-line motion kept between events, for one pane. A
// change of pane, or motion the other way, drops the remainder first: a swipe
// that turns around is not a step. The zero value is ready to use.
type WheelAccum struct {
	lines   float32
	columns float32
	pane    string
	has     bool
}

// Add adds lines of motion, up being positive, for pane.
func (w *WheelAccum) Add(pane string, lines float32) {
	w.retarget(pane)
	addRemainder(&w.lines, lines)
}

// AddColumns adds columns of motion, left being positive, for pane.
func (w *WheelAccum) AddColumns(pane string, columns float32) {
	w.retarget(pane)
	addRemainder(&w.columns, columns)
}

// Take returns the whole units in the vertical remainder, toward zero. The
// rest is kept.
func (w *WheelAccum) Take(unit float32) int {
	return takeRemainder(&w.lines, unit)
}

// TakeColumns returns the whole units in the horizontal remainder, toward
// zero. The rest is kept.
func (w *WheelAccum) TakeColumns(unit float32) int {
	return takeRemainder(&w.columns, unit)
}

// Clear drops the remainder, for when no pane takes the wheel.
func (w *WheelAccum) Clear() { *w = WheelAccum{} }

// retarget drops the remainder when pane is not the pane it belongs to.
func (w *WheelAccum) retarget(pane string) {
	if !w.has || w.pane != pane {
		*w = WheelAccum{pane: pane, has: true}
	}
}

func sign(v float32) float32 {
	if math.Signbit(float64(v)) {
		return -1
	}
	return 1
}

func addRemainder(total *float32, delta float32) {
	if !finite(delta) {
		return
	}
	if delta != 0 && sign(delta) != sign(*total) {
		*total = 0
	}
	*total += delta
}

func takeRemainder(total *float32, unit float32) int {
	if !finite(*total) {
		*total = 0
		return 0
	}
	if !finite(unit) || unit <= 0 {
		return 0
	}
	steps := float32(math.Trunc(float64(*total / unit)))
	if !finite(steps) || math.Abs(float64(steps)) > math.MaxInt32 {
		*total = 0
		return 0
	}
	*total -= steps * unit
	return int(steps)
}

// CellAt returns the surface cell under pos, a window point, with origin the
// grid's top-left corner in the same coordinates. ok is false in the padding,
// outside the grid, or with a point or cell that has no size.
func CellAt(pos, origin [2]float32, m Metrics, size endpoint.GridSize) (col, row uint16, ok bool) {
	if !finite(m.CellW) || !finite(m.CellH) || m.CellW <= 0 || m.CellH <= 0 {
		return 0, 0, false
	}
	x := pos[0] - origin[0] - m.Pad
	y := pos[1] - origin[1] - m.Pad
	c := math.Floor(float64(x / m.CellW))
	r := math.Floor(float64(y / m.CellH))
	// A NaN or infinite point fails these checks too.
	if !(c >= 0 && c < float64(size.Cols)) || !(r >= 0 && r < float64(size.Rows)) {
		return 0, 0, false
	}
	return uint16(c), uint16(r), true
}

// PaneAtCell returns the pane whose rect holds the cell, or nil. The rect has
// the pane's border and its scrollbar column, so the wheel over the scrollbar
// scrolls the pane.
func PaneAtCell(s *endpoint.Surface, col, row uint16) *endpoint.SurfacePane {
	for i := range s.Panes {
		if holds(s.Panes[i].Rect, col, row) {
			return &s.Panes[i]
		}
	}
	return nil
}

// PaneCell returns the cell relative to the pane's inner rect, clamped into
// it. The wheel event carries this position, which herdr reads as the cell
// under the pointer inside the pane.
func PaneCell(p *endpoint.SurfacePane, col, row uint16) (uint16, uint16) {
	inside := func(v, start, width uint16) uint16 {
		var rel uint16
		if v > start {
			rel = v - start
		}
		if width > 0 && rel > width-1 {
			rel = width - 1
		} else if width == 0 {
			rel = 0
		}
		return rel
	}
	return inside(col, p.InnerRect.X, p.InnerRect.Width), inside(row, p.InnerRect.Y, p.InnerRect.Height)
}

// Reports reports whether the pane hands the wheel to its program: it reads
// the mouse, or it is on the alternate screen, where herdr sends an arrow key.
// Otherwise the wheel scrolls herdr's history.
func Reports(p *endpoint.SurfacePane) bool {
	return p.MouseReporting || p.AlternateScreen
}

// WheelEvents returns the wheel events for steps lines, up being positive. A
// program that takes the wheel gets one event per notch, as herdr's own client
// sends; the history gets one event with the whole count.
func WheelEvents(p *endpoint.SurfacePane, col, row uint16, steps int, mods uint8) []endpoint.InputEvent {
	return axisEvents(p, col, row, steps, mods, endpoint.WheelUp, endpoint.WheelDown)
}

// WheelColumnEvents returns the wheel events for steps columns, left being
// positive. herdr turns a horizontal wheel into a mouse report only, so a pane
// that does not read the mouse gets nothing.
func WheelColumnEvents(p *endpoint.SurfacePane, col, row uint16, steps int, mods uint8) []endpoint.InputEvent {
	if !Reports(p) {
		return nil
	}
	return axisEvents(p, col, row, steps, mods, endpoint.WheelLeft, endpoint.WheelRight)
}

func axisEvents(p *endpoint.SurfacePane, col, row uint16, steps int, mods uint8, positive, negative endpoint.WheelDir) []endpoint.InputEvent {
	if steps == 0 {
		return nil
	}
	dir := negative
	count := -int64(steps)
	if steps > 0 {
		dir = positive
		count = int64(steps)
	}
	events, lines := 1, uint16(min(count, MaxWheelLines))
	if Reports(p) {
		events, lines = int(min(count, MaxWheelEvents)), uint16(WheelLines)
	}
	out := make([]endpoint.InputEvent, events)
	for i := range out {
		out[i] = endpoint.WheelEvent{Dir: dir, Column: col, Row: row, Mods: mods, Lines: lines}
	}
	return out
}

// WheelMods returns the modifier bits of a wheel event, as herdr reads them.
func WheelMods(m ui.Modifiers) uint8 {
	var flags uint8
	if m&ui.Shift != 0 {
		flags |= endpoint.ModShift
	}
	if m&ui.Ctrl != 0 {
		flags |= endpoint.ModControl
	}
	if m&ui.Alt != 0 {
		flags |= endpoint.ModAlt
	}
	return flags
}
