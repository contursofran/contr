// Adapted from Herdr UI (Apache-2.0); changes: ported to Go, one pane per selection, bounded by the pane's inner rect; no popups or scrollback; no hyperlink or URL words; trailing spaces trimmed on every line; no select-all unit; wide glyphs read from the wide-tail flag.

package terminal

import (
	"errors"
	"hash/maphash"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// The selection model: the cells a pointer chooses in one pane, and the text a
// copy takes.
//
// Selection is client-local. herdr owns the terminal, and nothing here sends
// input to it. A selection lies in one pane: its edges are surface cells, and a
// drag clamps to the pane's inner rect, so it never leaves the pane where it
// started. Nothing here paints or copies. The view does both, with the answers
// this model gives.

// MaxSelectionBytes is the most text one copy takes. A longer copy is refused,
// not cut short.
const MaxSelectionBytes = 4 << 20

var (
	// ErrStale means the pane is no longer on screen, or the surface lacks one
	// of its rows.
	ErrStale = errors.New("selection: pane is stale")
	// ErrTooLarge means the text is longer than MaxSelectionBytes.
	ErrTooLarge = errors.New("selection: text is too large")
)

// side is the half of a cell the pointer sat in. Anchoring on the half, as
// terminal emulators do, lets a single cell be selected, while a plain click,
// which never leaves its half, selects nothing.
type side uint8

const (
	sideLeft side = iota
	sideRight
)

// cellEdge is a pointer position on the cell grid. It orders by reading order, so
// the two ends of a drag sort into a start and an end whichever way it was
// made.
type cellEdge struct {
	row, col int
	side     side
}

func (e cellEdge) less(o cellEdge) bool {
	if e.row != o.row {
		return e.row < o.row
	}
	if e.col != o.col {
		return e.col < o.col
	}
	return e.side < o.side
}

func maxEdge(a, b cellEdge) cellEdge {
	if b.less(a) {
		return a
	}
	return b
}

// region is the cells of one pane's inner rect, in surface coordinates, with
// exclusive ends. It is never empty.
type region struct {
	colStart, colEnd int
	rowStart, rowEnd int
}

// regionOf returns the region of r, or false for a rect with no cells. The
// ends saturate at 65535, so a rect past the wire's limit cannot wrap when a
// row span is cast to uint16.
func regionOf(r endpoint.Rect) (region, bool) {
	if r.Width == 0 || r.Height == 0 {
		return region{}, false
	}
	return region{
		colStart: int(r.X), colEnd: min(int(r.X)+int(r.Width), math.MaxUint16),
		rowStart: int(r.Y), rowEnd: min(int(r.Y)+int(r.Height), math.MaxUint16),
	}, true
}

func (r region) contains(col, row int) bool {
	return col >= r.colStart && col < r.colEnd && row >= r.rowStart && row < r.rowEnd
}

func (r region) lastRow() int { return r.rowEnd - 1 }
func (r region) lastCol() int { return r.colEnd - 1 }

// edgeAt returns the cell edge under a point, clamped into the region. A point
// above, below, left of or right of it gives the edge on its boundary, so a
// drag out of the pane selects up to the pane's edge. ok is false for a point
// that is not finite, or a cell with no size.
func (r region) edgeAt(x, y float32, m Metrics) (e cellEdge, ok bool) {
	if !usable(x, y, m) {
		return cellEdge{}, false
	}
	row := floor32(y / m.CellH)
	if row < float32(r.rowStart) {
		return cellEdge{row: r.rowStart, col: r.colStart, side: sideLeft}, true
	}
	if row > float32(r.lastRow()) {
		return cellEdge{row: r.lastRow(), col: r.lastCol(), side: sideRight}, true
	}
	rowI := int(row)
	col := x / m.CellW
	if col < float32(r.colStart) {
		return cellEdge{row: rowI, col: r.colStart, side: sideLeft}, true
	}
	if col >= float32(r.colEnd) {
		return cellEdge{row: rowI, col: r.lastCol(), side: sideRight}, true
	}
	whole := floor32(col)
	s := sideRight
	if col-whole < 0.5 {
		s = sideLeft
	}
	return cellEdge{row: rowI, col: int(whole), side: s}, true
}

// usable is true when a point and the cell size give a usable edge: a finite
// point and a positive size.
func usable(x, y float32, m Metrics) bool {
	return isFinite(x) && isFinite(y) && hasSize(m.CellW) && hasSize(m.CellH)
}

func isFinite(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

// unit is how much one press takes: the half-cell under it for a single click,
// the word for a double click, and the row of the pane for a triple click. A
// drag that follows grows the selection by the same unit.
type unit uint8

const (
	unitCell unit = iota
	unitWord
	unitLine
)

// unitFor returns the unit that a click count chooses.
func unitFor(clicks int) unit {
	switch {
	case clicks <= 1:
		return unitCell
	case clicks == 2:
		return unitWord
	}
	return unitLine
}

// span returns the first and last edge of the unit around e, in r.
func (u unit) span(s *endpoint.Surface, r region, e cellEdge) (first, last cellEdge) {
	var cols [2]int
	switch u {
	case unitCell:
		return e, e
	case unitLine:
		cols = [2]int{r.colStart, r.colEnd}
	default:
		start, end, ok := word(s, r, e.row, e.col)
		if !ok {
			start, end = e.col, e.col+1
		}
		cols = [2]int{start, end}
	}
	return cellEdge{row: e.row, col: cols[0], side: sideLeft},
		cellEdge{row: e.row, col: cols[1] - 1, side: sideRight}
}

// cellsOf returns the cells of surface row row in [start, end), or false when
// the surface does not hold them.
func cellsOf(s *endpoint.Surface, row, start, end int) ([]endpoint.Cell, bool) {
	if row < 0 || row >= len(s.Rows) {
		return nil, false
	}
	cells := s.Rows[row]
	if start < 0 || start > end || end > len(cells) {
		return nil, false
	}
	return cells[start:end], true
}

// separates is true for characters that end a word even though they are
// printed: quotes, brackets, and the separators and borders that sit between
// words in terminal output.
func separates(c rune) bool {
	if unicode.IsSpace(c) || unicode.IsControl(c) {
		return true
	}
	switch c {
	case '"', '\'', '`', '(', ')', '[', ']', '{', '}', '<', '>', '|', ',', ';':
		return true
	}
	return c >= 0x2500 && c <= 0x259f
}

// shown is what a cell reads as. A concealed cell reads as a blank, and so
// does an empty cell.
func shown(c *endpoint.Cell) string {
	if c.Attrs&Hidden != 0 || c.Text == "" {
		return " "
	}
	return c.Text
}

// lineText is the text of one row span: each cell's text, with a wide glyph's
// second cell skipped and the trailing spaces removed.
func lineText(cells []endpoint.Cell) string {
	var b strings.Builder
	for i := range cells {
		if !cells[i].WideTail {
			b.WriteString(shown(&cells[i]))
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// word returns the columns [start, end) of the word under the cell (row, col),
// or false when that cell is a separator. A word runs to the nearest
// separator, so src/main.rs:12 comes out whole, without the full stop that
// closes a sentence after it.
func word(s *endpoint.Surface, r region, row, col int) (start, end int, ok bool) {
	if !r.contains(col, row) {
		return 0, 0, false
	}
	cells, ok := cellsOf(s, row, r.colStart, r.colEnd)
	if !ok {
		return 0, 0, false
	}
	hit := col - r.colStart
	// A wide glyph's second cell reads as the glyph that drew it.
	glyph := func(i int) int {
		for j := i; j >= 0; j-- {
			if !cells[j].WideTail {
				return j
			}
		}
		return i
	}
	firstChar := func(i int) rune {
		for _, c := range shown(&cells[glyph(i)]) {
			return c
		}
		return ' '
	}
	if separates(firstChar(hit)) {
		return 0, 0, false
	}
	first, last := hit, hit
	for first > 0 && !separates(firstChar(first-1)) {
		first--
	}
	for last+1 < len(cells) && !separates(firstChar(last+1)) {
		last++
	}
	// Punctuation that ends a sentence is not part of the word before it,
	// unless it was clicked.
	for last+r.colStart > col && strings.ContainsRune(".:!?", firstChar(last)) {
		last--
	}
	return r.colStart + first, r.colStart + last + 1, true
}

// RowSpan is the selected columns [Start, End) of one surface row.
type RowSpan struct {
	Row, Start, End uint16
}

type rowHash struct {
	row  int
	hash uint64
}

var hashSeed = maphash.MakeSeed()

// Selection is the cells one pointer chose in one pane. The pane is looked up
// in the surface on every use, so a pane that closed, moved or changed size
// neither paints nor copies stale cells: its cells are not the ones the
// pointer chose.
type Selection struct {
	paneID string
	// rect is the pane's inner rect when the press landed. A pane whose rect
	// changed no longer holds these cells, so the selection is stale.
	rect endpoint.Rect
	unit unit
	// origin is the unit the press chose. A drag never gives it up.
	origin   [2]cellEdge
	anchor   cellEdge
	head     cellEdge
	dragging bool
	// seen is the hash of each selected row's text when the drag ended, sorted
	// by row. See TextUnchanged.
	seen []rowHash
}

// BeginSelection starts a drag at the pointer, in the pane under it. x and y
// are in pixels from the top-left corner of cell (0, 0). clicks is the press's
// click count: a double click starts on the word under the pointer, and a
// triple click on its row. It returns nil when no pane holds the pointer, so a
// click on a border or in the padding selects nothing.
func BeginSelection(s *endpoint.Surface, m Metrics, x, y float32, clicks int) *Selection {
	pane := PaneAt(s, m, x, y)
	if pane == nil {
		return nil
	}
	r, ok := regionOf(pane.InnerRect)
	if !ok {
		return nil
	}
	e, ok := r.edgeAt(x, y, m)
	if !ok {
		return nil
	}
	u := unitFor(clicks)
	first, last := u.span(s, r, e)
	return &Selection{
		paneID:   pane.PaneID,
		rect:     pane.InnerRect,
		unit:     u,
		origin:   [2]cellEdge{first, last},
		anchor:   first,
		head:     last,
		dragging: true,
	}
}

// Equal reports whether s and o select the same cells in the same state. The
// remembered text (seen) is left out: it is set only on release.
func (s *Selection) Equal(o *Selection) bool {
	if s == nil || o == nil {
		return s == o
	}
	return s.paneID == o.paneID && s.rect == o.rect && s.unit == o.unit &&
		s.origin == o.origin && s.anchor == o.anchor && s.head == o.head &&
		s.dragging == o.dragging
}

// PaneID returns the pane the selection lies in.
func (s *Selection) PaneID() string { return s.paneID }

// Dragging is true until the drag ends.
func (s *Selection) Dragging() bool { return s.dragging }

// Extend follows the pointer, clamped to the pane. It does nothing once the
// drag has ended. It returns true when the selected cells changed.
func (s *Selection) Extend(surface *endpoint.Surface, m Metrics, x, y float32) bool {
	if !s.dragging {
		return false
	}
	r, ok := s.region(surface)
	if !ok {
		return false
	}
	e, ok := r.edgeAt(x, y, m)
	if !ok {
		return false
	}
	start, end := s.unit.span(surface, r, e)
	// A drag back past the unit the press chose keeps that unit's far end, so
	// the selection never loses what the press took.
	var anchor, head cellEdge
	if start.less(s.origin[0]) {
		anchor, head = s.origin[1], start
	} else {
		anchor, head = s.origin[0], maxEdge(end, s.origin[1])
	}
	changed := s.anchor != anchor || s.head != head
	s.anchor, s.head = anchor, head
	return changed
}

// Release ends the drag, and remembers the text of the selected rows so
// TextUnchanged can tell later whether the program has written over them. It
// returns true when this call ended the drag, and false when it had already
// ended.
func (s *Selection) Release(surface *endpoint.Surface) bool {
	if !s.dragging {
		return false
	}
	s.dragging = false
	s.RememberText(surface)
	return true
}

// Rows returns the selected columns of each row the pane shows, as surface
// rows. It is empty for a click that did not leave its half-cell, and for a
// pane that moved, changed size or is no longer on screen.
func (s *Selection) Rows(surface *endpoint.Surface) []RowSpan {
	r, ok := s.region(surface)
	if !ok {
		return nil
	}
	return s.spans(r)
}

// Text returns the selected text, one line per row, with the trailing spaces
// removed from each line. A wide glyph is written once. A concealed cell is
// written as a space. It returns ErrStale when the pane moved, changed size or
// one of its rows is gone, so a selection never copies part of a pane. It
// returns ErrTooLarge past MaxSelectionBytes.
func (s *Selection) Text(surface *endpoint.Surface) (string, error) {
	r, ok := s.region(surface)
	if !ok {
		return "", ErrStale
	}
	var b strings.Builder
	for i, sp := range s.spans(r) {
		cells, ok := cellsOf(surface, int(sp.Row), int(sp.Start), int(sp.End))
		if !ok {
			return "", ErrStale
		}
		line := lineText(cells)
		sep := 0
		if i > 0 {
			sep = 1
		}
		if b.Len()+sep+len(line) > MaxSelectionBytes {
			return "", ErrTooLarge
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

// RememberText remembers the text of the selected rows. Release calls this, so
// a view does not have to.
func (s *Selection) RememberText(surface *endpoint.Surface) {
	s.seen = s.rowHashes(surface)
}

// TextUnchanged is false once a remembered row shows other text: the program
// wrote over the selected rows, and the highlight would mark cells the user did
// not choose. A row that is no longer on screen is no evidence either way.
func (s *Selection) TextUnchanged(surface *endpoint.Surface) bool {
	for _, now := range s.rowHashes(surface) {
		i, found := slices.BinarySearchFunc(s.seen, now.row, func(h rowHash, row int) int {
			return h.row - row
		})
		if found && s.seen[i].hash != now.hash {
			return false
		}
	}
	return true
}

// rowHashes returns a hash of the text in each selected row, by row. It hashes
// text only: a recolour, or the cursor passing, leaves the choice valid.
func (s *Selection) rowHashes(surface *endpoint.Surface) []rowHash {
	r, ok := s.region(surface)
	if !ok {
		return nil
	}
	var out []rowHash
	for _, sp := range s.spans(r) {
		cells, ok := cellsOf(surface, int(sp.Row), int(sp.Start), int(sp.End))
		if !ok {
			continue
		}
		out = append(out, rowHash{int(sp.Row), maphash.String(hashSeed, lineText(cells))})
	}
	return out
}

// region returns the pane's region, or false once the surface no longer shows
// the pane, or shows it with another rect.
func (s *Selection) region(surface *endpoint.Surface) (region, bool) {
	for i := range surface.Panes {
		p := &surface.Panes[i]
		if p.PaneID != s.paneID {
			continue
		}
		if p.InnerRect != s.rect {
			return region{}, false
		}
		return regionOf(p.InnerRect)
	}
	return region{}, false
}

// ends returns the first and last edge, in reading order.
func (s *Selection) ends() (first, last cellEdge) {
	if !s.head.less(s.anchor) {
		return s.anchor, s.head
	}
	return s.head, s.anchor
}

// spans returns the selected columns of each row in r. The two ends cut their
// own rows at the pointer. The rows between them run the full width of the
// region.
func (s *Selection) spans(r region) []RowSpan {
	start, end := s.ends()
	bound := func(e cellEdge) int {
		if e.side == sideLeft {
			return e.col
		}
		return e.col + 1
	}
	var out []RowSpan
	for row := max(start.row, r.rowStart); row <= min(end.row, r.lastRow()); row++ {
		first, stop := r.colStart, r.colEnd
		if row == start.row {
			first = max(bound(start), r.colStart)
		}
		if row == end.row {
			stop = min(bound(end), r.colEnd)
		}
		if first < stop {
			out = append(out, RowSpan{uint16(row), uint16(first), uint16(stop)})
		}
	}
	return out
}
