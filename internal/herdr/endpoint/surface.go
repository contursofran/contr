// Pane surfaces of herdr's endpoint socket: the full surface of the focused
// tab (PaneSurface, tag 13) and the row spans that change it
// (PaneSurfacePatch, tag 19).
// Written from herdr's wire layout; see NOTICE.

package endpoint

import "errors"

// Cell is one cell. Fg and Bg are herdr's packed colours: the top byte is the
// kind (0 named, 1 indexed, 2 RGB) and the low bits are the value.
type Cell struct {
	Text string
	Fg   uint32
	Bg   uint32
	// Attrs holds style bits: herdr's modifier mask (BOLD 1 … STRIKETHROUGH
	// 256) and the underline style.
	Attrs uint16
	// WideTail is the second column of a wide glyph. The cell before it
	// draws the glyph.
	WideTail bool
}

// Cursor is the cursor position and shape inside the surface.
type Cursor struct {
	X       uint16
	Y       uint16
	Visible bool
	// Shape is the DECSCUSR parameter: 0–2 block, 3–4 underline, 5–6 bar.
	Shape uint8
}

// Rect is a rectangle in surface cells.
type Rect struct {
	X, Y, Width, Height uint16
}

// ScrollMetrics is herdr's scroll position of a pane: rows above the bottom,
// the most that can be scrolled up, and the rows shown.
type ScrollMetrics struct {
	OffsetFromBottom    uint64
	MaxOffsetFromBottom uint64
	ViewportRows        uint64
}

// SurfacePane is one pane of the shown tab, in surface coordinates.
type SurfacePane struct {
	PaneID          string
	Rect            Rect
	InnerRect       Rect
	Focused         bool
	AlternateScreen bool
	// MouseReporting is true when the program reads the mouse, so a wheel
	// reaches it as mouse reports.
	MouseReporting bool
	// Scroll is nil when herdr sends no scroll metrics for the pane.
	Scroll *ScrollMetrics
}

// Row is one row of cells. A patch never changes a Row in place: it puts a
// new slice in the surface, so a view can keep the old one.
type Row = []Cell

// Surface is the rendered surface of the focused tab: what the terminal view
// paints.
type Surface struct {
	BootID             string
	ProjectionRevision uint64
	Revision           uint64
	Size               GridSize
	Rows               []Row
	// Cursor is nil when herdr shows no cursor.
	Cursor *Cursor
	Panes  []SurfacePane
}

// FocusedPane returns the focused pane, or nil when no pane has focus.
func (s *Surface) FocusedPane() *SurfacePane {
	for i := range s.Panes {
		if s.Panes[i].Focused {
			return &s.Panes[i]
		}
	}
	return nil
}

// ApplyPatch applies p when it continues this surface. On an error nothing
// changes.
func (s *Surface) ApplyPatch(p SurfacePatch) error {
	if p.BootID != s.BootID || p.ProjectionRevision != s.ProjectionRevision || p.BaseRevision != s.Revision {
		return ErrStale
	}
	// Check every span before the first write, so a bad patch changes nothing.
	for _, sp := range p.Spans {
		y := int(sp.Y)
		if y >= len(s.Rows) || int(sp.X)+len(sp.Cells) > len(s.Rows[y]) {
			return ErrOutOfRange
		}
	}
	for _, sp := range p.Spans {
		row := make(Row, len(s.Rows[sp.Y]))
		copy(row, s.Rows[sp.Y])
		copy(row[sp.X:], sp.Cells)
		s.Rows[sp.Y] = row
	}
	for _, pane := range p.Panes {
		found := false
		for i := range s.Panes {
			if s.Panes[i].PaneID == pane.PaneID {
				s.Panes[i] = pane
				found = true
				break
			}
		}
		if !found {
			s.Panes = append(s.Panes, pane)
		}
	}
	s.Cursor = p.Cursor
	s.Revision = p.Revision
	return nil
}

// SurfaceFrame is a decoded PaneSurface frame.
type SurfaceFrame struct {
	Surface Surface
}

// Span is a run of cells that starts at column X of row Y.
type Span struct {
	X, Y  uint16
	Cells []Cell
}

// SurfacePatch is a decoded PaneSurfacePatch: the changes from BaseRevision
// to Revision.
type SurfacePatch struct {
	BootID             string
	ProjectionRevision uint64
	BaseRevision       uint64
	Revision           uint64
	Spans              []Span
	Panes              []SurfacePane
	Cursor             *Cursor
}

var (
	// ErrStale: the patch belongs to another boot, projection or base
	// revision. The link has lost sync and the caller must reconnect.
	ErrStale = errors.New("surface patch does not continue the surface")
	// ErrOutOfRange: a span does not fit in the surface.
	ErrOutOfRange = errors.New("surface patch span is outside the surface")
)

// DecodeSurfaceFrame reads the body of a PaneSurface (tag 13, already read).
// The fields after panes (splits, popup, graphics) are not read: the frame
// length bounds the payload and contr does not use them yet.
func DecodeSurfaceFrame(d *Dec) (SurfaceFrame, error) {
	var zero SurfaceFrame
	bootID, err := d.Str()
	if err != nil {
		return zero, err
	}
	projection, err := d.Vu()
	if err != nil {
		return zero, err
	}
	revision, err := d.Vu()
	if err != nil {
		return zero, err
	}
	body, err := readFrameData(d)
	if err != nil {
		return zero, err
	}
	if len(body.cells) != int(body.width)*int(body.height) {
		return zero, NewInvalid("frame size")
	}
	panes, err := readPanes(d)
	if err != nil {
		return zero, err
	}
	return SurfaceFrame{Surface{
		BootID:             bootID,
		ProjectionRevision: projection,
		Revision:           revision,
		Size:               GridSize{Cols: body.width, Rows: body.height},
		Rows:               intoRows(body.cells, body.width, body.height),
		Cursor:             body.cursor,
		Panes:              panes,
	}}, nil
}

// DecodeSurfacePatch reads the body of a PaneSurfacePatch (tag 19, already
// read).
func DecodeSurfacePatch(d *Dec) (SurfacePatch, error) {
	var zero SurfacePatch
	var p SurfacePatch
	var err error
	if p.BootID, err = d.Str(); err != nil {
		return zero, err
	}
	if p.ProjectionRevision, err = d.Vu(); err != nil {
		return zero, err
	}
	if p.BaseRevision, err = d.Vu(); err != nil {
		return zero, err
	}
	if p.Revision, err = d.Vu(); err != nil {
		return zero, err
	}
	n, err := d.Len()
	if err != nil {
		return zero, err
	}
	p.Spans = make([]Span, 0, n)
	for i := 0; i < n; i++ {
		x, err := d.Vu16()
		if err != nil {
			return zero, err
		}
		y, err := d.Vu16()
		if err != nil {
			return zero, err
		}
		cs, err := readCells(d)
		if err != nil {
			return zero, err
		}
		p.Spans = append(p.Spans, Span{X: x, Y: y, Cells: cs})
	}
	if p.Panes, err = readPanes(d); err != nil {
		return zero, err
	}
	if p.Cursor, err = readCursor(d); err != nil {
		return zero, err
	}
	return p, nil
}

// frameData is a FrameData without its hyperlinks and graphics, which are
// read past.
type frameData struct {
	cells         []Cell
	width, height uint16
	cursor        *Cursor
}

func readFrameData(d *Dec) (frameData, error) {
	var f frameData
	var err error
	if f.cells, err = readCells(d); err != nil {
		return f, err
	}
	if f.width, err = d.Vu16(); err != nil {
		return f, err
	}
	if f.height, err = d.Vu16(); err != nil {
		return f, err
	}
	if f.cursor, err = readCursor(d); err != nil {
		return f, err
	}
	n, err := d.Len()
	if err != nil {
		return f, err
	}
	for i := 0; i < n; i++ {
		if err := d.SkipBytes(); err != nil { // hyperlink URI
			return f, err
		}
	}
	if err := d.SkipBytes(); err != nil { // graphics bytes
		return f, err
	}
	return f, nil
}

// maxCells is herdr's largest surface in cells (its limit on cols × rows).
const maxCells = 1_000_000

func readCells(d *Dec) ([]Cell, error) {
	n, err := d.Len()
	if err != nil {
		return nil, err
	}
	if n > maxCells {
		return nil, NewInvalid("more cells than herdr's 1,000,000 limit")
	}
	out := make([]Cell, 0, n)
	for i := 0; i < n; i++ {
		c, err := readCell(d)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func readCell(d *Dec) (Cell, error) {
	var c Cell
	var err error
	if c.Text, err = d.Str(); err != nil {
		return c, err
	}
	if c.Fg, err = d.Vu32(); err != nil {
		return c, err
	}
	if c.Bg, err = d.Vu32(); err != nil {
		return c, err
	}
	if c.Attrs, err = d.Vu16(); err != nil {
		return c, err
	}
	if c.WideTail, err = d.Bool(); err != nil {
		return c, err
	}
	some, err := d.Option()
	if err != nil {
		return c, err
	}
	if some {
		// Hyperlink index: the hyperlink list is not kept.
		if _, err := d.Vu32(); err != nil {
			return c, err
		}
	}
	return c, nil
}

func readCursor(d *Dec) (*Cursor, error) {
	some, err := d.Option()
	if err != nil || !some {
		return nil, err
	}
	var c Cursor
	if c.X, err = d.Vu16(); err != nil {
		return nil, err
	}
	if c.Y, err = d.Vu16(); err != nil {
		return nil, err
	}
	if c.Visible, err = d.Bool(); err != nil {
		return nil, err
	}
	if c.Shape, err = d.U8(); err != nil {
		return nil, err
	}
	return &c, nil
}

func readRect(d *Dec) (Rect, error) {
	var r Rect
	var err error
	if r.X, err = d.Vu16(); err != nil {
		return r, err
	}
	if r.Y, err = d.Vu16(); err != nil {
		return r, err
	}
	if r.Width, err = d.Vu16(); err != nil {
		return r, err
	}
	if r.Height, err = d.Vu16(); err != nil {
		return r, err
	}
	return r, nil
}

// readPane reads a PaneSurfacePane. Its scrollbar rect, sgr pixel mouse and
// pixel fields are read past.
func readPane(d *Dec) (SurfacePane, error) {
	var p SurfacePane
	var err error
	if p.PaneID, err = d.Str(); err != nil {
		return p, err
	}
	if _, err = d.Vu(); err != nil { // content_revision
		return p, err
	}
	if p.Rect, err = readRect(d); err != nil {
		return p, err
	}
	if p.InnerRect, err = readRect(d); err != nil {
		return p, err
	}
	some, err := d.Option()
	if err != nil {
		return p, err
	}
	if some { // scrollbar_rect
		if _, err = readRect(d); err != nil {
			return p, err
		}
	}
	if some, err = d.Option(); err != nil {
		return p, err
	}
	if some {
		var m ScrollMetrics
		if m.OffsetFromBottom, err = d.Vu(); err != nil {
			return p, err
		}
		if m.MaxOffsetFromBottom, err = d.Vu(); err != nil {
			return p, err
		}
		if m.ViewportRows, err = d.Vu(); err != nil {
			return p, err
		}
		p.Scroll = &m
	}
	if p.Focused, err = d.Bool(); err != nil {
		return p, err
	}
	if p.MouseReporting, err = d.Bool(); err != nil {
		return p, err
	}
	if _, err = d.Bool(); err != nil { // sgr_pixel_mouse
		return p, err
	}
	if p.AlternateScreen, err = d.Bool(); err != nil {
		return p, err
	}
	if _, err = d.Vu32(); err != nil { // pixel_width
		return p, err
	}
	if _, err = d.Vu32(); err != nil { // pixel_height
		return p, err
	}
	return p, nil
}

func readPanes(d *Dec) ([]SurfacePane, error) {
	n, err := d.Len()
	if err != nil {
		return nil, err
	}
	out := make([]SurfacePane, 0, n)
	for i := 0; i < n; i++ {
		p, err := readPane(d)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// intoRows cuts the row-major cells into height rows of width cells. The
// caller has checked that the count is width * height.
func intoRows(cells []Cell, width, height uint16) []Row {
	w := int(width)
	rows := make([]Row, 0, int(height))
	for i := 0; i < int(height); i++ {
		rows = append(rows, cells[i*w:(i+1)*w:(i+1)*w])
	}
	return rows
}
