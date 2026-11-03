// Package term holds the terminal types that the terminal view uses: cells,
// the screen, input events and the link contract. It has no UI imports and
// knows no wire format. The cell attr bits are herdr's modifier bits (see
// internal/app/terminal/flags.go); any source of screens writes the same bits.
package term

import "errors"

// Limits on a resize: columns and rows each in 1..=MaxGridSide, their product
// at most MaxGridCells, and a cell side at most MaxCellPx device pixels.
const (
	MaxGridSide  = 4096
	MaxGridCells = 1_000_000
	MaxCellPx    = 4096
)

// GridSize is a size in terminal cells.
type GridSize struct {
	Cols uint16
	Rows uint16
}

// CellPx is the size of one cell in pixels.
type CellPx struct {
	Width  uint32
	Height uint32
}

// Cell is one cell. Fg and Bg are packed colours: the top byte is the kind
// (0 named, 1 indexed, 2 RGB) and the low bits are the value.
type Cell struct {
	Text string
	Fg   uint32
	Bg   uint32
	// Attrs holds style bits: the modifier mask (BOLD 1 … STRIKETHROUGH 256)
	// and the underline style.
	Attrs uint16
	// WideTail is the second column of a wide glyph. The cell before it
	// draws the glyph.
	WideTail bool
}

// Cursor is the cursor position and shape inside the screen.
type Cursor struct {
	X       uint16
	Y       uint16
	Visible bool
	// Shape is the DECSCUSR parameter: 0–2 block, 3–4 underline, 5–6 bar.
	Shape uint8
}

// Rect is a rectangle in screen cells.
type Rect struct {
	X, Y, Width, Height uint16
}

// ScrollMetrics is the scroll position of a pane: rows above the bottom, the
// most that can be scrolled up, and the rows shown.
type ScrollMetrics struct {
	OffsetFromBottom    uint64
	MaxOffsetFromBottom uint64
	ViewportRows        uint64
}

// Pane is one pane of the shown screen, in screen coordinates.
type Pane struct {
	PaneID          string
	Rect            Rect
	InnerRect       Rect
	Focused         bool
	AlternateScreen bool
	// MouseReporting is true when the program reads the mouse, so a wheel
	// reaches it as mouse reports.
	MouseReporting bool
	// Scroll is nil when the source sends no scroll metrics for the pane.
	Scroll *ScrollMetrics
}

// Row is one row of cells. A row is never changed in place: a new screen has
// a new slice, so a view can keep the old one.
type Row = []Cell

// Screen is the rendered screen: what the terminal view paints.
type Screen struct {
	BootID             string
	ProjectionRevision uint64
	Revision           uint64
	Size               GridSize
	Rows               []Row
	// Cursor is nil when no cursor shows.
	Cursor *Cursor
	Panes  []Pane
}

// FocusedPane returns the focused pane, or nil when no pane has focus.
func (s *Screen) FocusedPane() *Pane {
	for i := range s.Panes {
		if s.Panes[i].Focused {
			return &s.Panes[i]
		}
	}
	return nil
}

// ErrNotConnected: the link has no live connection. Input sent now is not
// kept.
var ErrNotConnected = errors.New("terminal link is not connected")
