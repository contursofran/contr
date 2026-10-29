package terminal

import (
	"math"
	"testing"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

var cellMetrics = Metrics{CellW: 7.8, CellH: 20, Pad: 8}

func nan() float32 { return float32(math.NaN()) }
func inf() float32 { return float32(math.Inf(1)) }

func wantGrid(t *testing.T, w, h float32, m Metrics, want *endpoint.GridSize) {
	t.Helper()
	got, ok := GridFor(w, h, m)
	if want == nil {
		if ok {
			t.Errorf("GridFor(%v, %v) = %v, want none", w, h, got)
		}
		return
	}
	if !ok || got != *want {
		t.Errorf("GridFor(%v, %v) = %v, %v; want %v", w, h, got, ok, *want)
	}
}

func TestGridFitsTheViewMinusThePadding(t *testing.T) {
	// (720 - 16) / 7.8 = 90.25 columns, (834 - 16) / 20 = 40.9 rows.
	wantGrid(t, 720, 834, cellMetrics, &endpoint.GridSize{Cols: 90, Rows: 40})
}

func TestTwoByTwoIsTheSmallestGrid(t *testing.T) {
	m := Metrics{CellW: 8, CellH: 20, Pad: 8}
	wantGrid(t, 32, 56, m, &endpoint.GridSize{Cols: 2, Rows: 2})
}

func TestAViewWithOneColumnOrOneRowHasNoGrid(t *testing.T) {
	m := Metrics{CellW: 8, CellH: 20, Pad: 8}
	wantGrid(t, 31, 56, m, nil) // one column
	wantGrid(t, 32, 55, m, nil) // one row
	wantGrid(t, 10, 10, cellMetrics, nil)
}

func TestANanOrNegativeViewHasNoGrid(t *testing.T) {
	wantGrid(t, nan(), 834, cellMetrics, nil)
	wantGrid(t, 720, -50, cellMetrics, nil)
}

func TestACellWithoutASizeHasNoGrid(t *testing.T) {
	zeroWidth := cellMetrics
	zeroWidth.CellW = 0
	nanHeight := cellMetrics
	nanHeight.CellH = nan()
	wantGrid(t, 720, 834, zeroWidth, nil)
	wantGrid(t, 720, 834, nanHeight, nil)
}

func TestTheGridIsClampedToHerdrsLimits(t *testing.T) {
	unit := Metrics{CellW: 1, CellH: 1}
	// Each side is capped at 4096; then 1,000,000 cells leave 244 rows.
	wantGrid(t, 5000, 5000, unit, &endpoint.GridSize{Cols: 4096, Rows: 244})
	// 3000 x 3000 is 9,000,000 cells: the rows give way to 1,000,000 / 3000 = 333.
	wantGrid(t, 3000, 3000, unit, &endpoint.GridSize{Cols: 3000, Rows: 333})
	// 1000 x 1500 is 1,500,000 cells: 1000 rows fit.
	wantGrid(t, 1000, 1500, unit, &endpoint.GridSize{Cols: 1000, Rows: 1000})
	// An unbounded view is clamped, not refused.
	wantGrid(t, inf(), inf(), unit, &endpoint.GridSize{Cols: 4096, Rows: 244})
}

func TestCellPxIsAtLeastOneDevicePixel(t *testing.T) {
	tiny := Metrics{CellW: 0.2, CellH: 0.4}
	if got := CellPx(tiny, 1); got != (endpoint.CellPx{Width: 1, Height: 1}) {
		t.Errorf("tiny = %v", got)
	}
	noSize := cellMetrics
	noSize.CellW = nan()
	if got := CellPx(noSize, 1).Width; got != 1 {
		t.Errorf("NaN width = %d, want 1", got)
	}
}

func TestCellPxIsAtMostHerdrsLimit(t *testing.T) {
	huge := Metrics{CellW: 1e9, CellH: inf()}
	want := endpoint.CellPx{Width: endpoint.MaxCellPx, Height: endpoint.MaxCellPx}
	if got := CellPx(huge, 1); got != want {
		t.Errorf("huge = %v, want %v", got, want)
	}
}

func TestCellPxRoundsTheCellSizeAtTheScale(t *testing.T) {
	cases := []struct {
		scale float32
		want  endpoint.CellPx
	}{
		{2, endpoint.CellPx{Width: 16, Height: 40}},
		// 7.8 x 1.5 = 11.7 rounds to 12; 20 x 1.5 = 30.
		{1.5, endpoint.CellPx{Width: 12, Height: 30}},
		{1, endpoint.CellPx{Width: 8, Height: 20}},
	}
	for _, c := range cases {
		if got := CellPx(cellMetrics, c.scale); got != c.want {
			t.Errorf("scale %v: %v, want %v", c.scale, got, c.want)
		}
	}
}
