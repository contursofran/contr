package terminal

import (
	"math"
	"testing"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// cellPx: cells are 10 x 20 pixels, with no padding.
var cellPx = Metrics{CellW: 10, CellH: 20}

// twoPanes is two panes side by side in a 12 x 4 surface. p1 holds cols 1-4
// and p2 holds cols 7-10, rows 1-2.
func twoPanes() *endpoint.Surface {
	pane := func(id string, x uint16) endpoint.SurfacePane {
		return endpoint.SurfacePane{
			PaneID:    id,
			Rect:      endpoint.Rect{X: x, Y: 0, Width: 6, Height: 4},
			InnerRect: endpoint.Rect{X: x + 1, Y: 1, Width: 4, Height: 2},
		}
	}
	return &endpoint.Surface{
		BootID:             "boot",
		ProjectionRevision: 1,
		Revision:           1,
		Size:               endpoint.GridSize{Cols: 12, Rows: 4},
		Panes:              []endpoint.SurfacePane{pane("p1", 0), pane("p2", 6)},
	}
}

// idAt returns the pane id at (x, y), or "" when no pane holds that point.
func idAt(s *endpoint.Surface, x, y float32) string {
	if p := PaneAt(s, cellPx, x, y); p != nil {
		return p.PaneID
	}
	return ""
}

func TestAPointInsideTheInnerRectIsThatPane(t *testing.T) {
	s := twoPanes()
	if got := idAt(s, 15, 25); got != "p1" {
		t.Errorf("first inner cell: %q", got)
	}
	if got := idAt(s, 45, 45); got != "p1" {
		t.Errorf("last inner cell: %q", got)
	}
}

func TestAPointOnAPaneBorderIsNoPane(t *testing.T) {
	s := twoPanes()
	for _, p := range [][2]float32{{5, 5}, {55, 45}, {15, 65}, {65, 45}} {
		if got := idAt(s, p[0], p[1]); got != "" {
			t.Errorf("(%v, %v): %q", p[0], p[1], got)
		}
	}
}

func TestAPointOutsideEveryPaneIsNoPane(t *testing.T) {
	s := twoPanes()
	s.Panes = s.Panes[:1]
	for _, p := range [][2]float32{{85, 25}, {120, 25}, {15, 80}} {
		if got := idAt(s, p[0], p[1]); got != "" {
			t.Errorf("(%v, %v): %q", p[0], p[1], got)
		}
	}
}

func TestAPointInThePaddingIsNoPane(t *testing.T) {
	s := twoPanes()
	if got := idAt(s, -1, 25); got != "" {
		t.Errorf("left padding: %q", got)
	}
	if got := idAt(s, 15, -1); got != "" {
		t.Errorf("top padding: %q", got)
	}
}

func TestTwoPanesSideBySideHoldTheirOwnCells(t *testing.T) {
	s := twoPanes()
	for _, c := range []struct {
		x, y float32
		want string
	}{{45, 25, "p1"}, {75, 25, "p2"}, {105, 45, "p2"}} {
		if got := idAt(s, c.x, c.y); got != c.want {
			t.Errorf("(%v, %v): %q, want %q", c.x, c.y, got, c.want)
		}
	}
}

func TestAPointOnACellEdgeBelongsToTheCellAfterIt(t *testing.T) {
	s := twoPanes()
	if got := idAt(s, 70, 25); got != "p2" {
		t.Errorf("70: %q", got)
	}
	if got := idAt(s, 69.9, 25); got != "" {
		t.Errorf("69.9: %q", got)
	}
}

func TestANonFinitePointOrAZeroCellIsNoPane(t *testing.T) {
	s := twoPanes()
	if got := idAt(s, float32(math.NaN()), 25); got != "" {
		t.Errorf("NaN: %q", got)
	}
	if got := idAt(s, 15, float32(math.Inf(1))); got != "" {
		t.Errorf("Inf: %q", got)
	}
	noCells := Metrics{CellH: 20}
	if PaneAt(s, noCells, 15, 25) != nil {
		t.Error("zero cell width gave a pane")
	}
}
