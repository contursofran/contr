// Adapted from Herdr UI (Apache-2.0); changes: ported to Go, cell lookup over the pane's inner rect, not its outer rect; no popup rule; unit tests.

package terminal

import (
	"math"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// PaneAt returns the pane whose inner rect holds the cell under the point
// (x, y), or nil. Both are in pixels from the top-left corner of cell (0, 0),
// so a negative value lies in the padding. A point on a pane's border, or in a
// cell that no pane holds, gives nil. So does a point outside the surface.
func PaneAt(s *endpoint.Surface, m Metrics, x, y float32) *endpoint.SurfacePane {
	col := math.Floor(float64(x / m.CellW))
	row := math.Floor(float64(y / m.CellH))
	// A NaN or infinite point, or a zero cell size, fails these checks too.
	if !(col >= 0 && col < float64(s.Size.Cols)) || !(row >= 0 && row < float64(s.Size.Rows)) {
		return nil
	}
	for i := range s.Panes {
		if holds(s.Panes[i].InnerRect, uint16(col), uint16(row)) {
			return &s.Panes[i]
		}
	}
	return nil
}

// holds reports whether cell (col, row) lies inside r.
func holds(r endpoint.Rect, col, row uint16) bool {
	c, rw := uint32(col), uint32(row)
	return c >= uint32(r.X) && c < uint32(r.X)+uint32(r.Width) &&
		rw >= uint32(r.Y) && rw < uint32(r.Y)+uint32(r.Height)
}
