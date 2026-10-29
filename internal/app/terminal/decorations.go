// Adapted from Herdr UI (Apache-2.0); changes: all five underline styles as grid marks; dash phase kept on the grid; ported to Go.

package terminal

import (
	"math"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Underlines: which cells a style covers and the marks it paints. The rules
// are pure functions, so the tests need no window.

// Dash and dot lengths, in logical pixels: the mark, then the gap after it.
const (
	dash    float32 = 4
	dashGap float32 = 3
	dot     float32 = 1
	dotGap  float32 = 1
)

// UnderlineKind is the style of an underline. UnderlineNone is no underline.
// The name differs from the Rust enum because Underline is the cell flag.
type UnderlineKind uint8

// Underline styles.
const (
	UnderlineNone UnderlineKind = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// underlineOf returns the underline of a cell. herdr keeps the style in bits
// 12-15: 2 double, 3 curly, 4 dotted and 5 dashed. 0 and 1 are single, and so
// is any other value, as herdr sends none. A hidden cell is not underlined.
func underlineOf(cell endpoint.Cell) UnderlineKind {
	if cell.Attrs&Hidden != 0 || cell.Attrs&(Underline|UnderlineStyle) == 0 {
		return UnderlineNone
	}
	switch (cell.Attrs & UnderlineStyle) >> UnderlineStyleShift {
	case 2:
		return UnderlineDouble
	case 3:
		return UnderlineCurly
	case 4:
		return UnderlineDotted
	case 5:
		return UnderlineDashed
	}
	return UnderlineSingle
}

// underlineRun is a set of cells in one row that share an underline style and
// color: the first column, the number of columns, the style and the color
// (the foreground, with reverse and dim applied).
type underlineRun struct {
	Col, Cols int
	Kind      UnderlineKind
	Color     uint32
}

// underlineRuns returns the underline runs of a row, in column order. A cell
// with no underline, or a hidden cell, ends a run. Blank cells and the tail of
// a wide glyph are underlined like any other cell.
func underlineRuns(row []endpoint.Cell) []underlineRun {
	var runs []underlineRun
	for col, cell := range row {
		kind := underlineOf(cell)
		if kind == UnderlineNone {
			continue
		}
		color, _, _ := CellColors(cell)
		if n := len(runs); n > 0 {
			last := &runs[n-1]
			if last.Kind == kind && last.Color == color && last.Col+last.Cols == col {
				last.Cols++
				continue
			}
		}
		runs = append(runs, underlineRun{Col: col, Cols: 1, Kind: kind, Color: color})
	}
	return runs
}

// mark is a line an underline paints. X0 and X1 are pixels from the grid's
// left edge. Y is the top of the mark, in pixels from the top of its row. A
// bar is straight and one pixel thick. A wave is three pixels high and is
// drawn by the window's wavy underline.
type mark struct {
	X0, X1, Y float32
	Wave      bool
}

// marks returns what kind paints from x0 to x1, in a row cellH pixels tall.
// Nothing paints for UnderlineNone or for an empty span.
func marks(kind UnderlineKind, x0, x1, cellH float32) []mark {
	if x1 <= x0 {
		return nil
	}
	// The bottom bar is one pixel thick, ending one pixel above the row's
	// bottom edge.
	bottom := cellH - 2
	bottomBars := func(on, gap float32) []mark {
		var out []mark
		for _, s := range dashes(x0, x1, on, gap) {
			out = append(out, mark{X0: s[0], X1: s[1], Y: bottom})
		}
		return out
	}
	switch kind {
	case UnderlineSingle:
		return []mark{{X0: x0, X1: x1, Y: bottom}}
	case UnderlineDouble:
		return []mark{{X0: x0, X1: x1, Y: bottom - 2}, {X0: x0, X1: x1, Y: bottom}}
	case UnderlineCurly:
		return []mark{{X0: x0, X1: x1, Y: bottom - 2, Wave: true}}
	case UnderlineDotted:
		return bottomBars(dot, dotGap)
	case UnderlineDashed:
		return bottomBars(dash, dashGap)
	}
	return nil
}

// dashes returns the on-spans of a dashed line from left to right. Each is on
// pixels long, and one starts every on+off pixels from the grid's left edge.
// The phase is the grid's, not the run's, so the dashes of neighbouring runs
// form one even line.
func dashes(left, right, on, off float32) [][2]float32 {
	period := on + off
	first := float32(math.Floor(float64(left / period)))
	var out [][2]float32
	for n := float32(0); ; n++ {
		start := float32(first+n) * period
		s := max(start, left)
		e := min(start+on, right)
		if s >= right {
			break
		}
		if s < e {
			out = append(out, [2]float32{s, e})
		}
	}
	return out
}
