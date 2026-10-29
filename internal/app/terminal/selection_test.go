package terminal

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// selM: cells of 10 by 20 pixels, with no padding. Cell (c, r) spans x from
// 10c to 10c + 10.
var selM = Metrics{CellW: 10, CellH: 20}

func selCell(text string) endpoint.Cell { return endpoint.Cell{Text: text} }

// selCells is one cell per character.
func selCells(text string) []endpoint.Cell {
	var out []endpoint.Cell
	for _, c := range text {
		out = append(out, selCell(string(c)))
	}
	return out
}

// selWide is a wide glyph: the cell that draws it, then the cell it covers.
func selWide(glyph string) []endpoint.Cell {
	return []endpoint.Cell{selCell(glyph), {WideTail: true}}
}

// selJoin joins runs of cells into one row.
func selJoin(parts ...[]endpoint.Cell) []endpoint.Cell {
	var out []endpoint.Cell
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// selRowOf is a row of cells of the given byte lengths, each one text.
func selRowOf(lengths ...int) []endpoint.Cell {
	var out []endpoint.Cell
	for _, n := range lengths {
		out = append(out, selCell(strings.Repeat("x", n)))
	}
	return out
}

func selPane(id string, x, y, width, height uint16) endpoint.SurfacePane {
	r := endpoint.Rect{X: x, Y: y, Width: width, Height: height}
	return endpoint.SurfacePane{PaneID: id, Rect: r, InnerRect: r, Focused: true}
}

// selScreen is a surface of rows, each padded with blanks to the widest row.
func selScreen(rows [][]endpoint.Cell, panes ...endpoint.SurfacePane) *endpoint.Surface {
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	out := make([]endpoint.Row, len(rows))
	for i, r := range rows {
		row := append([]endpoint.Cell(nil), r...)
		for len(row) < cols {
			row = append(row, selCell(" "))
		}
		out[i] = row
	}
	return &endpoint.Surface{
		BootID:             "boot",
		ProjectionRevision: 1,
		Revision:           1,
		Size:               endpoint.GridSize{Cols: uint16(cols), Rows: uint16(len(out))},
		Rows:               out,
		Panes:              panes,
	}
}

type pt2 struct{ x, y float32 }

// selAt is the pointer a fraction of the way across cell (col, row), at mid
// height.
func selAt(col, row uint16, fraction float32) pt2 {
	return pt2{
		float32(col)*selM.CellW + fraction*selM.CellW,
		float32(row)*selM.CellH + selM.CellH/2,
	}
}

func selLeft(col, row uint16) pt2  { return selAt(col, row, 0.2) }
func selRight(col, row uint16) pt2 { return selAt(col, row, 0.8) }

// selDrag presses at from, drags to to, and releases.
func selDrag(t *testing.T, s *endpoint.Surface, from, to pt2) *Selection {
	t.Helper()
	sel := BeginSelection(s, selM, from.x, from.y, 1)
	if sel == nil {
		t.Fatal("no press on a pane")
	}
	sel.Extend(s, selM, to.x, to.y)
	sel.Release(s)
	return sel
}

// selPress presses clicks times at p, with no drag.
func selPress(t *testing.T, s *endpoint.Surface, p pt2, clicks int) *Selection {
	t.Helper()
	sel := BeginSelection(s, selM, p.x, p.y, clicks)
	if sel == nil {
		t.Fatal("no press on a pane")
	}
	return sel
}

func copied(t *testing.T, sel *Selection, s *endpoint.Surface) string {
	t.Helper()
	text, err := sel.Text(s)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	return text
}

func wantText(t *testing.T, sel *Selection, s *endpoint.Surface, want string) {
	t.Helper()
	if got := copied(t, sel, s); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func wantSpans(t *testing.T, got []RowSpan, want ...RowSpan) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func wantErr(t *testing.T, sel *Selection, s *endpoint.Surface, want error) {
	t.Helper()
	if _, err := sel.Text(s); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func regionAt(x, y, width, height uint16) region {
	r, ok := regionOf(endpoint.Rect{X: x, Y: y, Width: width, Height: height})
	if !ok {
		panic("region without cells")
	}
	return r
}

func wantEdge(t *testing.T, r region, p pt2, m Metrics, want *cellEdge) {
	t.Helper()
	got, ok := r.edgeAt(p.x, p.y, m)
	switch {
	case want == nil && ok:
		t.Errorf("edgeAt(%v) = %+v, want none", p, got)
	case want != nil && (!ok || got != *want):
		t.Errorf("edgeAt(%v) = %+v ok=%v, want %+v", p, got, ok, *want)
	}
}

func TestAnEdgeIsTheHalfOfTheCellUnderThePoint(t *testing.T) {
	r := regionAt(2, 1, 5, 2)
	wantEdge(t, r, selLeft(2, 1), selM, &cellEdge{1, 2, sideLeft})
	wantEdge(t, r, selRight(2, 1), selM, &cellEdge{1, 2, sideRight})
	wantEdge(t, r, selRight(6, 2), selM, &cellEdge{2, 6, sideRight})
}

func TestAnEdgeOutsideTheRegionIsClampedToItsBoundary(t *testing.T) {
	r := regionAt(2, 1, 5, 2)
	// Above the region: its first row, at its first column.
	wantEdge(t, r, pt2{25, 0}, selM, &cellEdge{1, 2, sideLeft})
	// Below the region: its last row, at its last column.
	wantEdge(t, r, pt2{25, 100}, selM, &cellEdge{2, 6, sideRight})
	// Left of the region: its first column, on the row of the point.
	wantEdge(t, r, pt2{-5, 30}, selM, &cellEdge{1, 2, sideLeft})
	// Right of the region: its last column, on the row of the point.
	wantEdge(t, r, pt2{500, 30}, selM, &cellEdge{1, 6, sideRight})
}

func TestAnEdgeNeedsAFinitePointAndACellWithASize(t *testing.T) {
	r := regionAt(0, 0, 5, 1)
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	wantEdge(t, r, pt2{nan, 5}, selM, nil)
	wantEdge(t, r, pt2{5, inf}, selM, nil)
	wantEdge(t, r, pt2{5, 5}, Metrics{CellW: 0, CellH: 20}, nil)
	wantEdge(t, r, pt2{5, 5}, Metrics{CellW: 10, CellH: nan}, nil)
}

func TestAPressSelectsInThePaneUnderThePointerOnly(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdefghijkl")},
		selPane("left", 0, 0, 5, 1), selPane("right", 7, 0, 5, 1))
	if got := selPress(t, s, selLeft(1, 0), 1).PaneID(); got != "left" {
		t.Errorf("left press: %q", got)
	}
	if got := selPress(t, s, selRight(8, 0), 1).PaneID(); got != "right" {
		t.Errorf("right press: %q", got)
	}
	// Columns 5 and 6 are the gap between the panes.
	for _, col := range []uint16{5, 6} {
		p := selAt(col, 0, 0.5)
		if BeginSelection(s, selM, p.x, p.y, 1) != nil {
			t.Errorf("column %d: press in the gap selected", col)
		}
	}
	// Negative positions lie in the padding.
	if BeginSelection(s, selM, -1, 5, 1) != nil {
		t.Error("press in the padding selected")
	}
}

func TestAPaneWithNoCellsTakesNoPress(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abc")}, selPane("empty", 0, 0, 0, 1))
	if BeginSelection(s, selM, 5, 5, 1) != nil {
		t.Error("press on an empty pane selected")
	}
}

func TestAClickWithoutADragSelectsNoCell(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selPress(t, s, selRight(3, 0), 1)
	if !sel.Dragging() {
		t.Error("not dragging after press")
	}
	wantSpans(t, sel.Rows(s))
	wantText(t, sel, s, "")
	if !sel.Release(s) {
		t.Error("first Release did not end the drag")
	}
	if sel.Dragging() {
		t.Error("dragging after Release")
	}
	if sel.Release(s) {
		t.Error("second Release ended the drag")
	}
}

func TestADragSelectsFromTheFirstCellToTheOneUnderThePointer(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, s, selLeft(1, 0), selRight(3, 0))
	wantSpans(t, sel.Rows(s), RowSpan{0, 1, 4})
	wantText(t, sel, s, "bcd")
}

func TestADragBackToTheLeftSelectsTheSameCells(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	wantText(t, selDrag(t, s, selRight(3, 0), selLeft(1, 0)), s, "bcd")
}

func TestADragOverSeveralRowsCutsTheFirstAndLastRowsAndKeepsTheMiddleFull(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcd"), selCells("efgh"), selCells("ijkl")},
		selPane("p", 0, 0, 4, 3))
	sel := selDrag(t, s, selLeft(2, 0), selRight(1, 2))
	wantSpans(t, sel.Rows(s), RowSpan{0, 2, 4}, RowSpan{1, 0, 4}, RowSpan{2, 0, 2})
	wantText(t, sel, s, "cd\nefgh\nij")
}

func TestEachLineLosesItsTrailingSpacesAndABlankRowStaysALine(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("ab  "), selCells("    "), selCells("cd  ")},
		selPane("p", 0, 0, 4, 3))
	wantText(t, selDrag(t, s, selLeft(0, 0), selRight(3, 2)), s, "ab\n\ncd")
}

func TestAWideGlyphIsWrittenOnceFromItsFirstCell(t *testing.T) {
	row := selJoin([]endpoint.Cell{selCell("a")}, selWide("漢"), selCells("b"))
	s := selScreen([][]endpoint.Cell{row}, selPane("p", 0, 0, 4, 1))
	wantText(t, selDrag(t, s, selLeft(0, 0), selRight(3, 0)), s, "a漢b")
	wantText(t, selDrag(t, s, selLeft(0, 0), selRight(1, 0)), s, "a漢")
}

func TestAWideGlyphWhoseFirstCellIsBeforeTheSelectionIsLeftOut(t *testing.T) {
	row := selJoin([]endpoint.Cell{selCell("a")}, selWide("漢"), selCells("b"))
	s := selScreen([][]endpoint.Cell{row}, selPane("p", 0, 0, 4, 1))
	// The drag starts on the glyph's second cell, so the glyph is not in the
	// selection.
	wantText(t, selDrag(t, s, selLeft(2, 0), selRight(3, 0)), s, "b")
}

func TestAConcealedCellIsCopiedAsABlank(t *testing.T) {
	row := selJoin(selCells("ab"),
		[]endpoint.Cell{{Text: "c", Attrs: Hidden}, {Text: "d", Attrs: Hidden}},
		selCells("ef"))
	s := selScreen([][]endpoint.Cell{row}, selPane("p", 0, 0, 6, 1))
	wantText(t, selDrag(t, s, selLeft(0, 0), selRight(5, 0)), s, "ab  ef")
}

func TestAnEmptyCellIsCopiedAsABlank(t *testing.T) {
	row := []endpoint.Cell{selCell("a"), selCell(""), selCell("b")}
	s := selScreen([][]endpoint.Cell{row}, selPane("p", 0, 0, 3, 1))
	wantText(t, selDrag(t, s, selLeft(0, 0), selRight(2, 0)), s, "a b")
}

func TestADoubleClickSelectsTheWordUnderThePointer(t *testing.T) {
	// The word src/main.rs:12 has no separator in it.
	s := selScreen([][]endpoint.Cell{selCells("ls -la src/main.rs:12 ok")}, selPane("p", 0, 0, 24, 1))
	sel := selPress(t, s, selRight(12, 0), 2)
	wantSpans(t, sel.Rows(s), RowSpan{0, 7, 21})
	wantText(t, sel, s, "src/main.rs:12")
}

func TestAWordEndsBeforeAFullStopThatClosesASentence(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("see foo.bar. ok")}, selPane("p", 0, 0, 15, 1))
	wantText(t, selPress(t, s, selRight(5, 0), 2), s, "foo.bar")
	// A click on the full stop keeps it.
	wantText(t, selPress(t, s, selRight(11, 0), 2), s, "foo.bar.")
}

func TestAWordStopsAtQuotesAndBrackets(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells(`call("x")`)}, selPane("p", 0, 0, 9, 1))
	wantText(t, selPress(t, s, selRight(6, 0), 2), s, "x")
}

func TestADoubleClickOnASeparatorSelectsThatOneCell(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("ab cd")}, selPane("p", 0, 0, 5, 1))
	wantSpans(t, selPress(t, s, selRight(2, 0), 2).Rows(s), RowSpan{0, 2, 3})
}

func TestAWideGlyphBelongsToTheWordItIsIn(t *testing.T) {
	row := selJoin(selWide("漢"), selWide("字"), selCells(" ok"))
	s := selScreen([][]endpoint.Cell{row}, selPane("p", 0, 0, 7, 1))
	// Column 3 is the second cell of 字.
	wantText(t, selPress(t, s, selRight(3, 0), 2), s, "漢字")
}

func TestATripleClickSelectsTheWholeRowAndADragTakesWholeRows(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("ab  cd"), selCells("xyz")}, selPane("p", 0, 0, 6, 2))
	sel := selPress(t, s, selRight(1, 0), 3)
	wantSpans(t, sel.Rows(s), RowSpan{0, 0, 6})
	wantText(t, sel, s, "ab  cd")
	p := selRight(2, 1)
	sel.Extend(s, selM, p.x, p.y)
	sel.Release(s)
	wantText(t, sel, s, "ab  cd\nxyz")
}

func TestAWordDragTakesWholeWordsForwards(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("one two three")}, selPane("p", 0, 0, 13, 1))
	sel := selPress(t, s, selRight(0, 0), 2)
	p := selRight(9, 0)
	sel.Extend(s, selM, p.x, p.y)
	wantText(t, sel, s, "one two three")
}

func TestAWordDragBackKeepsTheFarEndOfTheWordThePressTook(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("one two three")}, selPane("p", 0, 0, 13, 1))
	sel := selPress(t, s, selRight(5, 0), 2)
	p := selLeft(0, 0)
	sel.Extend(s, selM, p.x, p.y)
	sel.Release(s)
	wantText(t, sel, s, "one two")
}

func TestExtendReportsOnlyAChangeInTheSelectedCells(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selPress(t, s, selLeft(1, 0), 1)
	p3, p4 := selRight(3, 0), selRight(4, 0)
	if !sel.Extend(s, selM, p3.x, p3.y) {
		t.Error("first move reported no change")
	}
	if sel.Extend(s, selM, p3.x, p3.y) {
		t.Error("same move reported a change")
	}
	if !sel.Extend(s, selM, p4.x, p4.y) {
		t.Error("further move reported no change")
	}
}

func TestADragAfterReleaseDoesNotMoveTheSelection(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selPress(t, s, selLeft(1, 0), 1)
	p3, p5 := selRight(3, 0), selRight(5, 0)
	sel.Extend(s, selM, p3.x, p3.y)
	sel.Release(s)
	if sel.Extend(s, selM, p5.x, p5.y) {
		t.Error("Extend after Release reported a change")
	}
	wantSpans(t, sel.Rows(s), RowSpan{0, 1, 4})
}

func TestADragStaysInsideThePaneWhereItStarted(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdXefgh")},
		selPane("a", 0, 0, 4, 1), selPane("b", 5, 0, 4, 1))
	sel := selPress(t, s, selLeft(1, 0), 1)
	p := selRight(7, 0)
	sel.Extend(s, selM, p.x, p.y)
	sel.Release(s)
	if sel.PaneID() != "a" {
		t.Errorf("pane = %q", sel.PaneID())
	}
	wantText(t, sel, s, "bcd")
}

func TestASelectionGivesNoTextOnceItsPaneIsGone(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, s, selLeft(1, 0), selRight(3, 0))
	s.Panes = nil
	wantSpans(t, sel.Rows(s))
	wantErr(t, sel, s, ErrStale)
}

func TestASelectionGivesNoTextOnceItsPaneMoves(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, before, selLeft(1, 0), selRight(3, 0))
	wantText(t, sel, before, "bcd")

	// The pane shows the same text, one row down: the cells the pointer chose
	// are not under the selection any more, so the text alone would not tell.
	moved := selScreen([][]endpoint.Cell{selCells("abcdef"), selCells("abcdef")}, selPane("p", 0, 1, 6, 1))
	wantSpans(t, sel.Rows(moved))
	wantErr(t, sel, moved, ErrStale)
}

func TestASelectionGivesNoTextOnceItsPaneShrinks(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("ab"), selCells("cd")}, selPane("p", 0, 0, 2, 2))
	sel := selDrag(t, before, selLeft(0, 0), selRight(1, 1))
	wantText(t, sel, before, "ab\ncd")

	// One row is gone: a copy would be part of the selection, so there is none.
	shrunk := selScreen([][]endpoint.Cell{selCells("ab")}, selPane("p", 0, 0, 2, 1))
	wantSpans(t, sel.Rows(shrunk))
	wantErr(t, sel, shrunk, ErrStale)
}

func TestASelectionGivesNoTextWhenTheSurfaceLacksOneOfItsRows(t *testing.T) {
	s := selScreen([][]endpoint.Cell{selCells("abc")}, selPane("p", 0, 0, 3, 3))
	sel := selDrag(t, s, selLeft(0, 0), selRight(0, 2))
	wantErr(t, sel, s, ErrStale)
}

func TestACopyOfFourMebibytesIsTakenAndOneByteMoreIsRefused(t *testing.T) {
	mib := 1 << 20
	atLimit := selScreen([][]endpoint.Cell{selRowOf(mib, mib, mib, mib)}, selPane("p", 0, 0, 4, 1))
	taken := selDrag(t, atLimit, selLeft(0, 0), selRight(3, 0))
	if got := len(copied(t, taken, atLimit)); got != MaxSelectionBytes {
		t.Errorf("len = %d, want %d", got, MaxSelectionBytes)
	}

	over := selScreen([][]endpoint.Cell{selRowOf(mib, mib, mib, mib+1)}, selPane("p", 0, 0, 4, 1))
	refused := selDrag(t, over, selLeft(0, 0), selRight(3, 0))
	wantErr(t, refused, over, ErrTooLarge)
}

func TestTheLineBreakBetweenRowsCountsTowardTheLimit(t *testing.T) {
	mib := 1 << 20
	fits := selScreen([][]endpoint.Cell{selRowOf(mib, mib), selRowOf(mib, mib-1)}, selPane("p", 0, 0, 2, 2))
	taken := selDrag(t, fits, selLeft(0, 0), selRight(1, 1))
	if got := len(copied(t, taken, fits)); got != MaxSelectionBytes {
		t.Errorf("len = %d, want %d", got, MaxSelectionBytes)
	}

	over := selScreen([][]endpoint.Cell{selRowOf(mib, mib), selRowOf(mib, mib)}, selPane("p", 0, 0, 2, 2))
	refused := selDrag(t, over, selLeft(0, 0), selRight(1, 1))
	wantErr(t, refused, over, ErrTooLarge)
}

func TestTextUnchangedHoldsWhileTheSelectedTextReadsTheSame(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, before, selLeft(1, 0), selRight(3, 0))

	recoloured := selCells("abcdef")
	for i := range recoloured {
		recoloured[i].Fg = 1
	}
	sameText := selScreen([][]endpoint.Cell{recoloured}, selPane("p", 0, 0, 6, 1))
	if !sel.TextUnchanged(sameText) {
		t.Error("a recolour counted as a change")
	}
}

func TestTextUnchangedFailsOnceASelectedRowIsWrittenOver(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, before, selLeft(1, 0), selRight(3, 0))

	written := selScreen([][]endpoint.Cell{selCells("axcdef")}, selPane("p", 0, 0, 6, 1))
	if sel.TextUnchanged(written) {
		t.Error("written-over text counted as unchanged")
	}
}

func TestTextUnchangedIgnoresAChangeOutsideTheSelectedCells(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selDrag(t, before, selLeft(1, 0), selRight(3, 0))

	written := selScreen([][]endpoint.Cell{selCells("abcdez")}, selPane("p", 0, 0, 6, 1))
	if !sel.TextUnchanged(written) {
		t.Error("a change outside the selection counted as a change")
	}
}

func TestADragStillInProgressRemembersNoText(t *testing.T) {
	before := selScreen([][]endpoint.Cell{selCells("abcdef")}, selPane("p", 0, 0, 6, 1))
	sel := selPress(t, before, selLeft(1, 0), 1)
	p := selRight(3, 0)
	sel.Extend(before, selM, p.x, p.y)

	written := selScreen([][]endpoint.Cell{selCells("xxxxxx")}, selPane("p", 0, 0, 6, 1))
	if !sel.TextUnchanged(written) {
		t.Error("a drag in progress remembered text")
	}
}

func TestAUnitFollowsTheClickCount(t *testing.T) {
	for clicks, want := range map[int]unit{0: unitCell, 1: unitCell, 2: unitWord, 3: unitLine, 4: unitLine} {
		if got := unitFor(clicks); got != want {
			t.Errorf("unitFor(%d) = %v, want %v", clicks, got, want)
		}
	}
}
