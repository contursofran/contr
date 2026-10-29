package terminal

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Wheel and trackpad.

const (
	// gestureGap is the quiet time that ends a scroll gesture (D18). MyGo gives
	// no touch phase, so a scroll that comes this long after the last one, and
	// is not momentum, starts a new gesture and drops the old remainder.
	gestureGap = 250 * time.Millisecond

	// lineDIPs is how many DIPs MyGo reports for one wheel line.
	lineDIPs = 40
)

// onScroll sends a wheel or trackpad scroll to the pane under the pointer. It
// is false when the scroll is not the terminal's: no cell metrics, surface,
// link, cell or pane. Then the remainder is dropped. Otherwise it is true,
// also when no event was sent. The size is not sent again (Rust D12).
func (v *View) onScroll(ev ui.InputEvent) bool {
	if !ev.Momentum && !v.lastScroll.IsZero() && ev.Time.Sub(v.lastScroll) >= gestureGap {
		v.wheel.Clear()
	}
	v.lastScroll = ev.Time

	s := v.surface
	if !v.hasMetrics || s == nil || v.link == nil {
		v.wheel.Clear()
		return false
	}
	col, row, ok := CellAt([2]float32{ev.X, ev.Y}, [2]float32{}, v.metrics, s.Size)
	if !ok {
		v.wheel.Clear()
		return false
	}
	pane := PaneAtCell(s, col, row)
	if pane == nil {
		v.wheel.Clear()
		return false
	}

	// MyGo multiplied AppKit's lines by lineDIPs for a wheel; the page moves
	// the other way to the delta (up is positive for the terminal).
	d := Delta{X: -ev.DX, Y: -ev.DY, Pixels: ev.Precise}
	if !ev.Precise {
		d.X /= lineDIPs
		d.Y /= lineDIPs
	}
	unit := float32(1)
	if Reports(pane) {
		unit = WheelLines
	}
	if Sideways(d) {
		v.wheel.AddColumns(pane.PaneID, DeltaColumns(d, v.metrics.CellW))
	} else {
		v.wheel.Add(pane.PaneID, DeltaLines(d, v.metrics.CellH))
	}
	lines, columns := v.wheel.Take(unit), v.wheel.TakeColumns(unit)

	pc, pr := PaneCell(pane, col, row)
	mods := WheelMods(ev.Mods)
	var events []endpoint.InputEvent
	events = append(events, WheelEvents(pane, pc, pr, lines, mods)...)
	events = append(events, WheelColumnEvents(pane, pc, pr, columns, mods)...)
	if len(events) > 0 {
		v.sendTo(pane.PaneID, events)
	}
	return true
}
