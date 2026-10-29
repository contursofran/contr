// Adapted from Herdr UI (Apache-2.0); changes: italic bit and face; underlines drawn per cell; selection tint between the backgrounds and the glyphs; ported to Go.

package terminal

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Paints a surface: cell backgrounds, the selection tint, box glyphs as quads,
// text, underlines and the cursor. Text is shaped per row and kept while herdr
// sends the same row again (the same backing array), so a patch that changes
// one row shapes one row. The span, run, tint and cursor rules are pure
// functions, so the tests need no window.

const (
	// cursorRGBA is the cursor: 50 % white (docs/DESIGN.md greys, and white
	// only for what the owner acts on).
	cursorRGBA uint32 = 0xFFFFFF80
	// selectionRGBA is the selection tint: white at 25 %, painted over the
	// cells so their own colors stay (D84).
	selectionRGBA uint32 = 0xFFFFFF40
	// cursorLine is the thickness of a bar or underline cursor, in logical
	// pixels.
	cursorLine float32 = 2
)

// Highlight is a selected part of a row: columns Start to End, End excluded.
type Highlight struct {
	Row, Start, End uint16
}

// Typing is what the view adds to a paint: whether it has focus, and the text
// the IME is composing ("" for none).
type Typing struct {
	Focused bool
	Marked  string
}

// rowKey identifies a row of cells by its backing array. herdr never changes
// a row in place (a patch puts a new slice in the surface), so the same key
// means the same cells. An empty row has no key and is never kept.
type rowKey struct {
	first *endpoint.Cell
	n     int
}

func keyOf(row endpoint.Row) rowKey {
	if len(row) == 0 {
		return rowKey{}
	}
	return rowKey{first: &row[0], n: len(row)}
}

// rowEntry is a cached row: the row it was made from and its shaped lines.
type rowEntry[L any] struct {
	key   rowKey
	lines []L
}

// refreshRows brings cache to rows, one entry per row, and truncates it to the
// row count. A row with the key of its cached entry keeps its lines. Any other
// row is shaped by shapeRow.
func refreshRows[L any](cache []rowEntry[L], rows []endpoint.Row, shapeRow func([]endpoint.Cell) []L) []rowEntry[L] {
	if len(cache) > len(rows) {
		clear(cache[len(rows):])
		cache = cache[:len(rows)]
	}
	for y, row := range rows {
		key := keyOf(row)
		if y < len(cache) && key.first != nil && cache[y].key == key {
			continue
		}
		entry := rowEntry[L]{key: key, lines: shapeRow(row)}
		if y < len(cache) {
			cache[y] = entry
		} else {
			cache = append(cache, entry)
		}
	}
	return cache
}

// line is the shaped text of one span: its glyphs, placed from the span's
// column, and the color to paint them in.
type line struct {
	col    int
	glyphs []ui.Glyph
	color  uint32
	// width is the span's extent in pixels; strike draws a line through it.
	width  float32
	strike bool
}

// memoLimit bounds the shaped spans kept by content. A full memo is dropped and
// refilled, so memory stays flat when every row changes.
const memoLimit = 2048

type shapeKey struct {
	text         string
	bold, italic bool
}

// memo keeps shaped glyphs by content. Text that moves to another row, as in a
// scroll, or that herdr sends again in a new row, is not shaped again. The color
// is not part of the key: glyphs have none until they are painted.
type memo struct {
	glyphs map[shapeKey][]ui.Glyph
}

func (m *memo) get(key shapeKey, make func() []ui.Glyph) []ui.Glyph {
	if g, ok := m.glyphs[key]; ok {
		return g
	}
	if m.glyphs == nil || len(m.glyphs) >= memoLimit {
		m.glyphs = map[shapeKey][]ui.Glyph{}
	}
	g := make()
	m.glyphs[key] = g
	return g
}

// Cache keeps the shaped text of the rows between paints. The zero value is
// ready. Reset it when the font size or the display scale changes.
type Cache struct {
	rows  []rowEntry[line]
	memo  memo
	cellW float32
	scale float32
	// ascent is the font's ascent; it is zero until the first paint.
	ascent, height float32
}

// Reset drops every shaped row.
func (c *Cache) Reset() { c.rows, c.memo, c.ascent = nil, memo{}, 0 }

// Style is the look of a run of text. Two cells with equal styles join one
// span. Underlines are not part of it: they are drawn per cell from
// underlineRuns.
type Style struct {
	Fg     uint32
	Bold   bool
	Italic bool
	Strike bool
}

// Span is text on one row, from column Col. An ASCII span has one column per
// character.
type Span struct {
	Col   int
	Text  string
	Style Style
}

// quadGlyph returns the box or block glyph that a cell paints as quads, if
// any. A wide tail or a hidden cell paints nothing.
func quadGlyph(cell endpoint.Cell) (BoxGlyph, bool) {
	if cell.WideTail || cell.Attrs&Hidden != 0 {
		return BoxGlyph{}, false
	}
	r := []rune(cell.Text)
	if len(r) != 1 {
		return BoxGlyph{}, false
	}
	return Segments(r[0])
}

// textSpans returns the spans of a row, in column order. Wide glyph tails, hidden
// cells and box glyphs make no span: box glyphs are drawn as quads. A span of
// blanks makes none either, unless it is struck through.
func textSpans(row []endpoint.Cell) []Span {
	var out []Span
	for col, cell := range row {
		if cell.WideTail || cell.Attrs&Hidden != 0 {
			continue
		}
		if _, box := quadGlyph(cell); box {
			continue
		}
		style := styleOf(cell)
		switch {
		case isASCIIChar(cell.Text):
			// An ASCII span has one column per byte, so it ends at col+len.
			if n := len(out); n > 0 {
				last := &out[n-1]
				if last.Style == style && isASCII(last.Text) && last.Col+len(last.Text) == col {
					last.Text += cell.Text
					continue
				}
			}
			out = append(out, Span{Col: col, Text: cell.Text, Style: style})
		case cell.Text != "":
			out = append(out, Span{Col: col, Text: cell.Text, Style: style})
		}
	}
	kept := out[:0]
	for _, s := range out {
		if !isBlank(s) {
			kept = append(kept, s)
		}
	}
	return kept
}

// bgRun is a run of one background color in a row.
type bgRun struct {
	Col, Cols int
	Color     uint32
}

// backgroundRuns returns the runs of one background color in a row. Cells with
// the default background make no run.
func backgroundRuns(row []endpoint.Cell) []bgRun {
	var runs []bgRun
	for col, cell := range row {
		_, color, ok := CellColors(cell)
		if !ok {
			continue
		}
		if n := len(runs); n > 0 {
			last := &runs[n-1]
			if last.Color == color && last.Col+last.Cols == col {
				last.Cols++
				continue
			}
		}
		runs = append(runs, bgRun{Col: col, Cols: 1, Color: color})
	}
	return runs
}

// cursorCell returns the cell the cursor is in, clamped to the grid. herdr
// may place it one past the last column.
func cursorCell(c endpoint.Cursor, size endpoint.GridSize) (x, y uint16) {
	return min(c.X, max(size.Cols, 1)-1), min(c.Y, max(size.Rows, 1)-1)
}

// cursorRect returns the part of a cell the cursor fills: the whole cell for a
// block (shapes 0-2), a bar at its left edge for shapes 5-6, and a line at its
// bottom for shapes 3-4.
func cursorRect(cell ui.Rect, shape uint8) ui.Rect {
	switch shape {
	case 3, 4:
		return ui.Rect{X: cell.X, Y: cell.Y + cell.H - cursorLine, W: cell.W, H: cursorLine}
	case 5, 6:
		return ui.Rect{X: cell.X, Y: cell.Y, W: cursorLine, H: cell.H}
	}
	return cell
}

// highlightRect returns the square one selection tint covers: the cells
// start..end of row. ok is false for an empty span or a row the surface does
// not hold.
func highlightRect(origin ui.Point, m Metrics, h Highlight, rows int) (ui.Rect, bool) {
	if h.Start >= h.End || int(h.Row) >= rows {
		return ui.Rect{}, false
	}
	return ui.Rect{
		X: origin.X + float32(h.Start)*m.CellW,
		Y: origin.Y + float32(h.Row)*m.CellH,
		W: float32(h.End-h.Start) * m.CellW,
		H: m.CellH,
	}, true
}

// Paint paints s with its top-left cell at origin. highlights are the selected
// cells, tinted between the backgrounds and the glyphs. typing.Focused draws
// the cursor solid; without it the cursor is an outline. The caller paints the
// terminal background first.
func Paint(p *ui.Painter, origin ui.Point, m Metrics, s *endpoint.Surface, highlights []Highlight, typing Typing, cache *Cache) {
	if cache.cellW != m.CellW || cache.scale != p.Scale() {
		cache.Reset()
		cache.cellW, cache.scale = m.CellW, p.Scale()
	}
	vis := p.Visible()
	shown := func(y int) bool {
		top := origin.Y + float32(y)*m.CellH
		return top+m.CellH > vis.Y && top < vis.Y+vis.H
	}
	for y, row := range s.Rows {
		if !shown(y) {
			continue
		}
		top := origin.Y + float32(y)*m.CellH
		for _, run := range backgroundRuns(row) {
			p.Fill(ui.Rect{X: origin.X + float32(run.Col)*m.CellW, Y: top, W: float32(run.Cols) * m.CellW, H: m.CellH}, theme.Color(run.Color), 0)
		}
	}
	tint := rgbaColor(selectionRGBA)
	for _, h := range highlights {
		if r, ok := highlightRect(origin, m, h, len(s.Rows)); ok {
			p.Fill(r, tint, 0)
		}
	}
	paintQuadGlyphs(p, origin, m, s, shown)

	if cache.ascent == 0 {
		fm := terminalFont(Style{}).Metrics()
		cache.ascent, cache.height = fm.Ascent, fm.Ascent+fm.Descent
	}
	cache.rows = refreshRows(cache.rows, s.Rows, func(row []endpoint.Cell) []line {
		var out []line
		for _, sp := range textSpans(row) {
			g := cache.memo.get(shapeKey{sp.Text, sp.Style.Bold, sp.Style.Italic}, func() []ui.Glyph {
				return shape(sp.Text, sp.Style, m.CellW)
			})
			out = append(out, line{col: sp.Col, glyphs: g, color: sp.Style.Fg, width: extent(g), strike: sp.Style.Strike})
		}
		return out
	})
	for y, entry := range cache.rows {
		if !shown(y) {
			continue
		}
		base := origin.Y + float32(y)*m.CellH + (m.CellH-cache.height)/2 + cache.ascent
		for _, l := range entry.lines {
			x := origin.X + float32(l.col)*m.CellW
			p.Glyphs(l.glyphs, x, base, theme.Color(l.color))
			if l.strike {
				mid := origin.Y + float32(y)*m.CellH + m.CellH/2
				p.Fill(ui.Rect{X: x, Y: mid, W: l.width, H: 1}, theme.Color(l.color), 0)
			}
		}
	}
	paintUnderlines(p, origin, m, s, shown)

	if c := s.Cursor; c != nil {
		if typing.Marked != "" {
			paintMarked(p, origin, m, *c, s.Size, typing.Marked, cache)
		}
		if c.Visible {
			paintCursor(p, origin, m, *c, s.Size, typing.Focused)
		}
	}
}

// paintQuadGlyphs paints the box and block glyphs of the surface as quads,
// each in its own cell. Every edge is computed from the cell's row or column
// index, so neighbouring cells share it exactly. Shades are translucent. The
// quads paint before the text, which has no span for these cells.
func paintQuadGlyphs(p *ui.Painter, origin ui.Point, m Metrics, s *endpoint.Surface, shown func(int) bool) {
	scale := p.Scale()
	for y, row := range s.Rows {
		if !shown(y) {
			continue
		}
		y0, y1 := origin.Y+float32(y)*m.CellH, origin.Y+float32(y+1)*m.CellH
		for col, cell := range row {
			glyph, ok := quadGlyph(cell)
			if !ok {
				continue
			}
			x0, x1 := origin.X+float32(col)*m.CellW, origin.X+float32(col+1)*m.CellW
			fg, _, _ := CellColors(cell)
			paint := theme.Color(fg).Alpha(glyph.Alpha())
			Quads(glyph, Edges{X0: x0, Y0: y0, X1: x1, Y1: y1}, scale, func(q Rect) {
				p.Fill(ui.Rect{X: q.X, Y: q.Y, W: q.W, H: q.H}, paint, 0)
			})
		}
	}
}

// paintMarked paints the text the IME is composing, underlined, at the cursor
// cell. Its background is the terminal's, so the glyphs under it do not show
// through.
func paintMarked(p *ui.Painter, origin ui.Point, m Metrics, c endpoint.Cursor, grid endpoint.GridSize, text string, cache *Cache) {
	x, y := cursorCell(c, grid)
	at := ui.Point{X: origin.X + float32(x)*m.CellW, Y: origin.Y + float32(y)*m.CellH}
	glyphs := shape(text, Style{Fg: theme.Text}, m.CellW)
	width := extent(glyphs)
	p.Fill(ui.Rect{X: at.X, Y: at.Y, W: width, H: m.CellH}, theme.Color(theme.BgTerminal), 0)
	base := at.Y + (m.CellH-cache.height)/2 + cache.ascent
	p.Glyphs(glyphs, at.X, base, theme.Color(theme.Text))
	x0 := float32(x) * m.CellW
	for _, mk := range marks(UnderlineSingle, x0, x0+width, m.CellH) {
		paintMark(p, origin, float32(y)*m.CellH, mk, theme.Text)
	}
}

// paintUnderlines paints the underlines of s over its text, one run of cells
// at a time.
func paintUnderlines(p *ui.Painter, origin ui.Point, m Metrics, s *endpoint.Surface, shown func(int) bool) {
	for y, row := range s.Rows {
		if !shown(y) {
			continue
		}
		rowTop := float32(y) * m.CellH
		for _, run := range underlineRuns(row) {
			x0 := float32(run.Col) * m.CellW
			x1 := float32(run.Col+run.Cols) * m.CellW
			for _, mk := range marks(run.Kind, x0, x1, m.CellH) {
				paintMark(p, origin, rowTop, mk, run.Color)
			}
		}
	}
}

// paintMark paints one underline mark. rowTop is the top of its row, in pixels
// from origin.
func paintMark(p *ui.Painter, origin ui.Point, rowTop float32, mk mark, color uint32) {
	c := theme.Color(color)
	x0, x1, y := origin.X+mk.X0, origin.X+mk.X1, origin.Y+rowTop+mk.Y
	if !mk.Wave {
		p.Fill(ui.Rect{X: x0, Y: y, W: x1 - x0, H: 1}, c, 0)
		return
	}
	// A wave is three pixels high. Its line runs along the middle of that
	// band, up and down every two pixels.
	var path ui.Path
	up := false
	path.MoveTo(x0, y+2.5)
	for x := x0 + 2; ; x += 2 {
		at := min(x, x1)
		if up {
			path.LineTo(at, y+2.5)
		} else {
			path.LineTo(at, y+0.5)
		}
		up = !up
		if at >= x1 {
			break
		}
	}
	p.StrokePath(&path, 1, c)
}

func paintCursor(p *ui.Painter, origin ui.Point, m Metrics, c endpoint.Cursor, grid endpoint.GridSize, focused bool) {
	x, y := cursorCell(c, grid)
	cell := ui.Rect{X: origin.X + float32(x)*m.CellW, Y: origin.Y + float32(y)*m.CellH, W: m.CellW, H: m.CellH}
	color := rgbaColor(cursorRGBA)
	if focused {
		p.Fill(cursorRect(cell, c.Shape), color, 0)
		return
	}
	p.Stroke(cell, color, 0, 1)
}

// terminalFont returns the terminal font in the face style asks for: bold takes
// the bold weight and italic the italic style. Ligature features are off, so
// each cell draws its own character and `->` and `!=` stay separate glyphs.
func terminalFont(style Style) ui.Font {
	f := ui.Font{
		Family:   theme.FontFamily,
		Size:     theme.TerminalFontSize,
		Weight:   400,
		Features: "calt=0,clig=0,liga=0",
		Italic:   style.Italic,
	}
	if style.Bold {
		f.Weight = 700
	}
	return f
}

// shape shapes text in the terminal font in the look style gives. The
// underline is not part of it: paintUnderlines draws it. An ASCII text gets
// one cell per character, so its glyphs stay on the grid whatever the font's
// advance is.
func shape(text string, style Style, cellW float32) []ui.Glyph {
	glyphs := ui.Shape(text, terminalFont(style))
	if isASCII(text) {
		for i := range glyphs {
			glyphs[i].X = float32(glyphs[i].Cluster) * cellW
		}
	}
	return glyphs
}

// extent is the width of shaped glyphs: the end of the last one.
func extent(glyphs []ui.Glyph) float32 {
	if n := len(glyphs); n > 0 {
		return glyphs[n-1].X + glyphs[n-1].Advance
	}
	return 0
}

func styleOf(cell endpoint.Cell) Style {
	fg, _, _ := CellColors(cell)
	return Style{
		Fg:     fg,
		Bold:   cell.Attrs&Bold != 0,
		Italic: cell.Attrs&Italic != 0,
		Strike: cell.Attrs&Strikethrough != 0,
	}
}

// rgbaColor turns a packed 0xRRGGBBAA value into a color.
func rgbaColor(v uint32) ui.Color {
	return ui.Color{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// isASCIIChar is true for one printable ASCII character (space included).
func isASCIIChar(text string) bool {
	return len(text) == 1 && text[0] >= ' ' && text[0] <= '~'
}

func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			return false
		}
	}
	return true
}

// isBlank is true when a span is only whitespace and has no strikethrough to
// draw.
func isBlank(s Span) bool {
	return strings.TrimSpace(s.Text) == "" && !s.Style.Strike
}
