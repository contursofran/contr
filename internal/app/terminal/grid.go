package terminal

import (
	"math"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Metrics is the cell size in logical pixels, and the padding kept on every
// side of the grid.
type Metrics struct {
	CellW, CellH, Pad float32
}

// GridFor returns the grid that fits a view of widthPx x heightPx logical
// pixels once Pad is kept on every side. ok is false when fewer than 2 columns
// or 2 rows fit, or when a cell has no size. The grid is clamped to herdr's
// limits: at most MaxGridSide per side and MaxGridCells in all. Rows give way
// first, so a very wide view keeps all its columns.
func GridFor(widthPx, heightPx float32, m Metrics) (size endpoint.GridSize, ok bool) {
	if !hasSize(m.CellW) || !hasSize(m.CellH) {
		return endpoint.GridSize{}, false
	}
	cols := floor32((widthPx - 2*m.Pad) / m.CellW)
	rows := floor32((heightPx - 2*m.Pad) / m.CellH)
	// NaN fails these tests too.
	if !(cols >= 2) || !(rows >= 2) {
		return endpoint.GridSize{}, false
	}
	// Both are at least 2 here. Clamped to the side limit they are finite, and
	// cols is at most MaxGridSide, so the cell limit leaves at least 244 rows.
	c := uint32(min(cols, float32(endpoint.MaxGridSide)))
	r := min(uint32(min(rows, float32(endpoint.MaxGridSide))), endpoint.MaxGridCells/c)
	return endpoint.GridSize{Cols: uint16(c), Rows: uint16(r)}, true
}

// CellPx returns the cell size in device pixels at scale, rounded to whole
// pixels. herdr takes this at resize. Each side is at least 1 and at most
// MaxCellPx.
func CellPx(m Metrics, scale float32) endpoint.CellPx {
	return endpoint.CellPx{
		Width:  devicePx(m.CellW * scale),
		Height: devicePx(m.CellH * scale),
	}
}

// devicePx rounds a side, then keeps it within 1..=MaxCellPx. A NaN becomes 1.
func devicePx(side float32) uint32 {
	r := round32(side)
	switch {
	case r != r, r < 1:
		return 1
	case r > endpoint.MaxCellPx:
		return endpoint.MaxCellPx
	}
	return uint32(r)
}

// hasSize is true for a finite, positive size. NaN and infinity count as no
// size.
func hasSize(size float32) bool {
	return size > 0 && !math.IsInf(float64(size), 0)
}

func floor32(v float32) float32 { return float32(math.Floor(float64(v))) }

// round32 rounds half away from zero, as Rust's f32::round does.
func round32(v float32) float32 { return float32(math.Round(float64(v))) }
