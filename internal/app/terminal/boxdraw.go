package terminal

// Box drawing and block characters as rectangles. The terminal font leaves a
// gap between rows when the line height is larger than the glyph, so a column
// of │ breaks. These characters are drawn as quads instead. Every edge lands
// on a device pixel, so rows and columns meet with no gap. Rounded corners
// (╭ ╮ ╯ ╰) and every other character stay text.
//
// The geometry is pure: logical pixels in, logical pixels out. The tests need
// no window.
//
// Float math stays in float32, as in the Rust code. Each product is wrapped in
// float32(...) so the compiler cannot fuse it with an add.

// Weight is the weight of one line: one logical pixel (light), two (heavy), or
// two parallel lines one logical pixel wide, two logical pixels apart centre
// to centre (double). The zero value is no line.
type Weight uint8

// Line weights.
const (
	None Weight = iota
	Light
	Heavy
	Double
)

// Arms are the arms of a line glyph: one for each side that has a line running
// to the cell edge. None means no arm.
type Arms struct {
	Up, Down, Left, Right Weight
}

// Block is a block glyph: the part of the cell it fills, as fractions of the
// cell (0 to 1), at Alpha.
type Block struct {
	X0, Y0, X1, Y1, Alpha float32
}

// BoxGlyph is what a character paints as quads: line arms, or a block when
// IsBlock is set.
type BoxGlyph struct {
	IsBlock bool
	Arms    Arms
	Block   Block
}

// Alpha is the opacity of the quads. Shades are translucent; lines and solid
// blocks are opaque.
func (g BoxGlyph) Alpha() float32 {
	if g.IsBlock {
		return g.Block.Alpha
	}
	return 1
}

// Rect is a rectangle in logical pixels, with its top-left corner at (X, Y).
type Rect struct {
	X, Y, W, H float32
}

// Edges are the edges of a cell in logical pixels: (X0, Y0) is its top-left
// corner and (X1, Y1) its bottom-right one. The caller computes each edge from
// the cell's index, so two cells that touch share their edge exactly.
type Edges struct {
	X0, Y0, X1, Y1 float32
}

func lines(up, down, left, right Weight) (BoxGlyph, bool) {
	return BoxGlyph{Arms: Arms{Up: up, Down: down, Left: left, Right: right}}, true
}

func block(x0, y0, x1, y1, alpha float32) (BoxGlyph, bool) {
	return BoxGlyph{IsBlock: true, Block: Block{x0, y0, x1, y1, alpha}}, true
}

// Segments returns the glyph that paints c as quads. ok is false when the font
// draws c.
func Segments(c rune) (g BoxGlyph, ok bool) {
	const (
		n = None
		l = Light
		h = Heavy
		d = Double
	)
	// Arms are in the order: up, down, left, right.
	switch c {
	case '─':
		return lines(n, n, l, l)
	case '│':
		return lines(l, l, n, n)
	case '┌':
		return lines(n, l, n, l)
	case '┐':
		return lines(n, l, l, n)
	case '└':
		return lines(l, n, n, l)
	case '┘':
		return lines(l, n, l, n)
	case '├':
		return lines(l, l, n, l)
	case '┤':
		return lines(l, l, l, n)
	case '┬':
		return lines(n, l, l, l)
	case '┴':
		return lines(l, n, l, l)
	case '┼':
		return lines(l, l, l, l)
	case '╴':
		return lines(n, n, l, n)
	case '╵':
		return lines(l, n, n, n)
	case '╶':
		return lines(n, n, n, l)
	case '╷':
		return lines(n, l, n, n)
	case '━':
		return lines(n, n, h, h)
	case '┃':
		return lines(h, h, n, n)
	case '┏':
		return lines(n, h, n, h)
	case '┓':
		return lines(n, h, h, n)
	case '┗':
		return lines(h, n, n, h)
	case '┛':
		return lines(h, n, h, n)
	case '┣':
		return lines(h, h, n, h)
	case '┫':
		return lines(h, h, h, n)
	case '┳':
		return lines(n, h, h, h)
	case '┻':
		return lines(h, n, h, h)
	case '╋':
		return lines(h, h, h, h)
	case '═':
		return lines(n, n, d, d)
	case '║':
		return lines(d, d, n, n)
	case '╔':
		return lines(n, d, n, d)
	case '╗':
		return lines(n, d, d, n)
	case '╚':
		return lines(d, n, n, d)
	case '╝':
		return lines(d, n, d, n)
	case '╠':
		return lines(d, d, n, d)
	case '╣':
		return lines(d, d, d, n)
	case '╦':
		return lines(n, d, d, d)
	case '╩':
		return lines(d, n, d, d)
	case '╬':
		return lines(d, d, d, d)
	case '█':
		return block(0, 0, 1, 1, 1)
	case '▀':
		return block(0, 0, 1, 0.5, 1)
	case '▄':
		return block(0, 0.5, 1, 1, 1)
	case '▌':
		return block(0, 0, 0.5, 1, 1)
	case '▐':
		return block(0.5, 0, 1, 1, 1)
	case '░':
		return block(0, 0, 1, 1, 0.25)
	case '▒':
		return block(0, 0, 1, 1, 0.5)
	case '▓':
		return block(0, 0, 1, 1, 0.75)
	}
	return BoxGlyph{}, false
}

// Quads calls emit with each rectangle that paints glyph in cell, when the
// window has scale device pixels to each logical pixel. Cells that touch share
// their edge pixels, so they meet with no gap. A glyph emits at most eight line
// rectangles or one block.
func Quads(glyph BoxGlyph, cell Edges, scale float32, emit func(Rect)) {
	if glyph.IsBlock {
		blockQuad(glyph.Block, cell, scale, emit)
		return
	}
	drawLines(glyph.Arms, cell, scale, emit)
}

// edge moves a logical edge to the device pixel grid.
func edge(v, scale float32) float32 { return round32(float32(v * scale)) }

// lerp is the point t of the way from a to b. Exact at both ends, so a shared
// edge stays exact.
func lerp(a, b, t float32) float32 {
	return float32(a*(1-t)) + float32(b*t)
}

// span is a (start, end) pair in device pixels.
type span [2]float32

// push emits [xa, xb) x [ya, yb) in device pixels as a logical rectangle. An
// empty one emits nothing.
func push(emit func(Rect), x, y span, scale float32) {
	if x[1] > x[0] && y[1] > y[0] {
		emit(Rect{
			X: x[0] / scale,
			Y: y[0] / scale,
			W: (x[1] - x[0]) / scale,
			H: (y[1] - y[0]) / scale,
		})
	}
}

func blockQuad(b Block, cell Edges, scale float32, emit func(Rect)) {
	x := func(f float32) float32 { return edge(lerp(cell.X0, cell.X1, f), scale) }
	y := func(f float32) float32 { return edge(lerp(cell.Y0, cell.Y1, f), scale) }
	push(emit, span{x(b.X0), x(b.X1)}, span{y(b.Y0), y(b.Y1)}, scale)
}

// strokes returns the strokes of one axis of a line, in device pixels: (start,
// end) of each, across the axis, and how many there are. Light and heavy lines
// are one stroke centred on centre. A double line is two light strokes,
// centred one logical pixel either side of it.
func strokes(w Weight, centre, scale float32) ([2]span, int) {
	light := max(round32(scale), 1)
	var thick float32
	offsets := [2]float32{}
	n := 1
	switch w {
	case Light:
		thick = light
	case Heavy:
		thick = max(round32(float32(2*scale)), 1)
	case Double:
		thick, offsets, n = light, [2]float32{-scale, scale}, 2
	default:
		return [2]span{}, 0
	}
	var out [2]span
	for i := 0; i < n; i++ {
		start := round32(centre + offsets[i] - thick/2)
		out[i] = span{start, start + thick}
	}
	return out, n
}

// band returns the stroke at i, or the last one when i is past the end. It
// returns the zero span for no strokes.
func band(s []span, i int) span {
	if i < len(s) {
		return s[i]
	}
	return last(s)
}

func last(s []span) span {
	if len(s) == 0 {
		return span{}
	}
	return s[len(s)-1]
}

// fromEnd is index k counted from the far end of n strokes: the first stroke
// is the last one.
func fromEnd(n, k int) int { return max(n-(1+k), 0) }

// drawLines emits the rectangles of a line glyph. Each arm runs from its cell
// edge to the centre. Where it meets a perpendicular arm, it runs on to that
// arm's far edge, so the corner has no notch:
//   - an arm with the opposite arm on its axis runs through the whole cell;
//   - a T or a corner joins the stroke that the other arm continues from (the
//     inner one for a double T, and the outer one for a double corner);
//   - a lone arm stops at the cell centre.
//
// A double axis has a channel between its two strokes. Where the other axis is
// double too, a stroke that runs through the channel stops at it, so the
// channel stays open through the junction. In ╠ only the inner stem line
// breaks; in ╬ every stroke breaks, and the four pixels next to the centre
// stay open.
func drawLines(arms Arms, cell Edges, scale float32, emit func(Rect)) {
	x0, x1 := edge(cell.X0, scale), edge(cell.X1, scale)
	y0, y1 := edge(cell.Y0, scale), edge(cell.Y1, scale)
	fx := float32(lerp(cell.X0, cell.X1, 0.5) * scale)
	fy := float32(lerp(cell.Y0, cell.Y1, 0.5) * scale)
	cx, cy := round32(fx), round32(fy)
	// Horizontal arms are drawn as rows, vertical arms as columns. Both arms of
	// one axis share a weight in every glyph of the table; a glyph with mixed
	// weights on one axis needs its own rule.
	rowBuf, nRows := strokes(pick(arms.Left, arms.Right), fy, scale)
	colBuf, nCols := strokes(pick(arms.Up, arms.Down), fx, scale)
	rows, cols := rowBuf[:nRows], colBuf[:nCols]
	up, down := arms.Up != None, arms.Down != None
	left, right := arms.Left != None, arms.Right != None
	// A channel exists only where both axes are double (╠ ╣ ╦ ╩ ╬). A light or
	// heavy axis has one stroke and no channel to keep open.
	double := nRows == 2 && nCols == 2

	for k, s := range rows {
		top, bottom := s[0], s[1]
		// The stroke on the side of a vertical arm stops at the channel of the
		// vertical axis.
		cut := double && ((up && k == 0) || (down && k == 1))
		if left {
			var end float32
			switch {
			case right && cut:
				end = band(cols, 0)[1]
			case right:
				end = x1
			case up && down:
				end = band(cols, 0)[1]
			case down:
				end = band(cols, fromEnd(nCols, k))[1]
			case up:
				end = band(cols, k)[1]
			default:
				end = cx
			}
			push(emit, span{x0, end}, span{top, bottom}, scale)
		}
		if right {
			var start float32
			switch {
			case left && cut:
				start = band(cols, 1)[0]
			case left:
				start = x0
			case up && down:
				start = last(cols)[0]
			case down:
				start = band(cols, k)[0]
			case up:
				start = band(cols, fromEnd(nCols, k))[0]
			default:
				start = cx
			}
			push(emit, span{start, x1}, span{top, bottom}, scale)
		}
	}

	for k, s := range cols {
		lft, rgt := s[0], s[1]
		// The stroke on the side of a horizontal arm stops at the channel of the
		// horizontal axis.
		cut := double && ((left && k == 0) || (right && k == 1))
		if up {
			var end float32
			switch {
			case down && cut:
				end = band(rows, 0)[1]
			case down:
				end = y1
			case left && right:
				end = band(rows, 0)[1]
			case right:
				end = band(rows, fromEnd(nRows, k))[1]
			case left:
				end = band(rows, k)[1]
			default:
				end = cy
			}
			push(emit, span{lft, rgt}, span{y0, end}, scale)
		}
		if down {
			var start float32
			switch {
			case up && cut:
				start = band(rows, 1)[0]
			case up:
				start = y0
			case left && right:
				start = last(rows)[0]
			case right:
				start = band(rows, k)[0]
			case left:
				start = band(rows, fromEnd(nRows, k))[0]
			default:
				start = cy
			}
			push(emit, span{lft, rgt}, span{start, y1}, scale)
		}
	}
}

// pick returns a, or b when a is none: the weight of an axis.
func pick(a, b Weight) Weight {
	if a != None {
		return a
	}
	return b
}
