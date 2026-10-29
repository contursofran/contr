package terminal

import (
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// pointerView is a settled 800 x 600 view with a recording FocusPane.
func pointerView(t *testing.T) (v *View, tt *ui.Tester, invalidations *int, asked *[]string) {
	t.Helper()
	asked = new([]string)
	v, tt, invalidations = newTestViewWith(t, Options{
		FocusPane: func(id string) { *asked = append(*asked, id) },
	})
	settle(tt, 800, 600)
	return v, tt, invalidations, asked
}

// showPage gives the view a link and a full-grid page of lines.
func showPage(v *View, tt *ui.Tester, lines ...string) *fakeLink {
	f := attach(v, tt)
	v.acceptSurface(fullPage(v, 2, lines))
	tt.Frame()
	return f
}

func clickCell(v *View, tt *ui.Tester, col, row int) {
	x, y := cellPoint(v, tt, col, row, 0.5)
	tt.ClickAt(x, y)
}

func wantAsked(t *testing.T, asked *[]string, want ...string) {
	t.Helper()
	if len(*asked) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(*asked, want) {
		t.Errorf("herdr was asked for %v, want %v", *asked, want)
	}
}

func wantClipboard(t *testing.T, tt *ui.Tester, want string) {
	t.Helper()
	if got := tt.Clipboard(); got != want {
		t.Errorf("clipboard = %q, want %q", got, want)
	}
}

func TestAClickOnAPaneHerdrDoesNotFocusAsksHerdrForItOnce(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	v.acceptSurface(twoPanesFocused(1, "p1"))
	tt.Frame()

	clickCell(v, tt, 8, 2)
	wantAsked(t, asked, "p2")

	// herdr shows p2 focused: a click on it asks for nothing more.
	v.acceptSurface(twoPanesFocused(2, "p2"))
	clickCell(v, tt, 8, 2)
	wantAsked(t, asked, "p2")
}

func TestAClickOnTheFocusedPaneAsksHerdrForNothing(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	v.acceptSurface(twoPanesFocused(1, "p1"))
	tt.Frame()

	clickCell(v, tt, 2, 2)
	wantAsked(t, asked)
}

func TestAClickThatIsNotOnAPaneAsksHerdrForNothing(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	v.acceptSurface(twoPanesFocused(1, "p1"))
	tt.Frame()

	// A pane's corner, its right border, and the other pane's left border.
	for _, c := range [][2]int{{0, 0}, {5, 2}, {6, 2}} {
		clickCell(v, tt, c[0], c[1])
	}
	// Below the surface, and in the padding before the first cell.
	clickCell(v, tt, 2, 10)
	grid, _ := tt.Find(LabelGrid)
	tt.ClickAt(grid.X+2, grid.Y+2)
	wantAsked(t, asked)
}

func TestAClickWithNoSurfaceAsksHerdrForNothing(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	grid, _ := tt.Find(LabelGrid)
	tt.ClickAt(grid.X+grid.W/2, grid.Y+grid.H/2)
	wantAsked(t, asked)
	if v.selection != nil {
		t.Error("a click with no surface made a selection")
	}
}

func TestAClickAsksForThePaneTheShellReportsWhenNoPaneIsFocused(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	v.shellPane = "p1"
	v.acceptSurface(twoPanesFocused(1, ""))
	tt.Frame()

	// The shell's pane is p1: a click on p1 asks for nothing, and one on p2
	// asks for p2.
	clickCell(v, tt, 2, 2)
	clickCell(v, tt, 8, 2)
	wantAsked(t, asked, "p2")
}

func TestADragThenCmdCCopiesTheSelectedText(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	f := showPage(v, tt, "abcdef")
	tt.SetClipboard("kept")

	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	wantClipboard(t, tt, "kept") // a release copies nothing (Rust D83)

	tt.Key(ui.Super, ui.KeyC)
	wantClipboard(t, tt, "abc")
	if q := f.queued(); len(q) != 0 {
		t.Errorf("a selection sent %v", q)
	}
}

func TestTheCopyActionCopiesTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})

	// The Edit menu's Copy is the same entry point as Cmd-C (Rust #106).
	tt.Command("copy")
	wantClipboard(t, tt, "abc")
}

func TestReleasingADragCopiesNothingAndKeepsTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	tt.SetClipboard("kept")

	drag(v, tt, [2]int{1, 0}, [2]int{3, 0})
	wantClipboard(t, tt, "kept")
	if v.selection == nil {
		t.Fatal("the selection is gone")
	}
	if v.selection.Dragging() {
		t.Error("the drag did not end")
	}
	wantSpans(t, v.selection.Rows(v.surface), RowSpan{Row: 0, Start: 1, End: 4})
}

func TestAClickWithoutADragClearsTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	tt.SetClipboard("kept")

	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	if v.selection == nil {
		t.Fatal("no selection after a drag")
	}
	clickCell(v, tt, 4, 0)
	if v.selection != nil {
		t.Error("a click kept the selection")
	}
	tt.Key(ui.Super, ui.KeyC)
	wantClipboard(t, tt, "kept") // a click copies nothing
}

func TestADoubleClickSelectsAWordThatCmdCCopies(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "ls -la src/main.rs:12 ok")

	x, y := cellPoint(v, tt, 12, 0, 0.8)
	tt.ClickAt(x, y)
	tt.ClickAt(x, y) // the tester clock has not moved: Clicks is 2
	tt.Key(ui.Super, ui.KeyC)
	wantClipboard(t, tt, "src/main.rs:12")
}

func TestATripleClickSelectsTheWholeRowThatCmdCCopies(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "one two", "three")

	// The row copies without its trailing blanks.
	x, y := cellPoint(v, tt, 1, 0, 0.8)
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	tt.Key(ui.Super, ui.KeyC)
	wantClipboard(t, tt, "one two")
}

func TestACmdClickStartsNoSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")

	// Rust #104 holds a Cmd-click for the link under the pointer.
	x0, y0 := cellPoint(v, tt, 1, 0, 0.2)
	x1, y1 := cellPoint(v, tt, 3, 0, 0.8)
	tt.ClickAtWith(ui.Super, x0, y0)
	tt.Move(x1, y1)
	if v.selection != nil {
		t.Error("a Cmd-click made a selection")
	}

	// It leaves a selection that is already there alone.
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	selected := v.selection
	if selected == nil {
		t.Fatal("no selection after a drag")
	}
	x, y := cellPoint(v, tt, 4, 0, 0.5)
	tt.ClickAtWith(ui.Super, x, y)
	if v.selection != selected {
		t.Error("a Cmd-click changed the selection")
	}
}

func TestACmdClickStillAsksHerdrForThePane(t *testing.T) {
	v, tt, _, asked := pointerView(t)
	v.acceptSurface(twoPanesFocused(1, "p1"))
	tt.Frame()

	x, y := cellPoint(v, tt, 8, 2, 0.5)
	tt.ClickAtWith(ui.Super, x, y)
	wantAsked(t, asked, "p2")
}

func TestCmdCWithNoSelectionLeavesTheClipboardAndHerdrAlone(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	f := attach(v, tt)
	tt.SetClipboard("kept")

	tt.Key(ui.Super, ui.KeyC)
	wantClipboard(t, tt, "kept")
	if q := f.queued(); len(q) != 0 {
		t.Errorf("Cmd-C sent %v", q)
	}
	// Cmd-C falls through to the app: there was nothing to copy.
	if v.copySelection() {
		t.Error("copySelection reported a copy with no selection")
	}
}

func TestAProgramWriteOverTheSelectedRowsDropsTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	if v.selection == nil {
		t.Fatal("no selection after a drag")
	}

	// The second cell of the selection reads x now: the tint would mark cells
	// the user did not choose.
	v.acceptSurface(fullPage(v, 3, []string{"axcdef"}))
	if v.selection != nil {
		t.Error("the selection survived a write over its rows")
	}
}

func TestARecolourKeepsTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})

	v.acceptSurface(fullPage(v, 3, []string{"abcdef"}))
	if v.selection == nil {
		t.Error("a recolour dropped the selection")
	}
}

func TestADragNotifiesOnlyWhenTheSelectedCellsChange(t *testing.T) {
	v, tt, n, _ := pointerView(t)
	showPage(v, tt, "abcdef")

	x0, y0 := cellPoint(v, tt, 0, 0, 0.2)
	x1, y1 := cellPoint(v, tt, 2, 0, 0.8)
	x2, y2 := cellPoint(v, tt, 3, 0, 0.8)
	tt.Press(x0, y0)
	before := *n

	tt.Move(x1, y1)
	if got := *n - before; got != 1 {
		t.Fatalf("first move: %d invalidations, want 1", got)
	}
	tt.Move(x1, y1)
	if got := *n - before; got != 1 {
		t.Errorf("the same cells repainted: %d invalidations, want 1", got)
	}
	tt.Move(x2, y2)
	if got := *n - before; got != 2 {
		t.Errorf("a new cell: %d invalidations, want 2", got)
	}
}

func TestASurfaceChangeDoesNotRetireADragInProgress(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")

	x0, y0 := cellPoint(v, tt, 0, 0, 0.2)
	x1, y1 := cellPoint(v, tt, 2, 0, 0.8)
	tt.Press(x0, y0)
	tt.Move(x1, y1)

	// The program writes while the pointer is down: the highlight follows the
	// pointer, so the drag stays. It is retired only once it has ended.
	v.acceptSurface(fullPage(v, 3, []string{"xyzxyz"}))
	if v.selection == nil || !v.selection.Dragging() {
		t.Fatal("the drag was retired")
	}
}

func TestAPaneThatMovesOrShrinksDropsTheSelection(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	f := showPage(v, tt, "abcdef")
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	size := v.want.size

	// The same text, one row down: the cells the pointer chose are not in the
	// pane any more.
	v.acceptSurface(page(v, 3, []string{"abcdef"}, endpoint.Rect{Y: 1, Width: size.Cols, Height: size.Rows - 1}))
	if v.selection != nil {
		t.Error("a pane that moved kept the selection")
	}

	// Select again, then take a row away: a copy would be part of the selection.
	v.acceptSurface(fullPage(v, 4, []string{"abcdef"}))
	tt.Frame()
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	if v.selection == nil {
		t.Fatal("no selection after the second drag")
	}
	v.acceptSurface(page(v, 5, []string{"abcdef"}, endpoint.Rect{Width: size.Cols, Height: size.Rows - 1}))
	if v.selection != nil {
		t.Error("a pane that shrank kept the selection")
	}

	if q := f.queued(); len(q) != 0 {
		t.Errorf("nothing should reach herdr, got %v", q)
	}
}

func TestAnEmptySelectionIsDroppedOnRelease(t *testing.T) {
	v, tt, n, _ := pointerView(t)
	showPage(v, tt, "abcdef")

	// A press that never leaves its half-cell selects no cell.
	x, y := cellPoint(v, tt, 1, 0, 0.2)
	tt.Press(x, y)
	if v.selection == nil {
		t.Fatal("a press on a pane made no selection")
	}
	before := *n
	tt.Release(x, y)
	if v.selection != nil {
		t.Error("an empty selection stayed after the release")
	}
	if *n == before {
		t.Error("dropping the selection asked for no frame")
	}
}

func TestTheSelectionIsPaintedFromItsRows(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	if got := v.highlights(); got != nil {
		t.Errorf("highlights with no selection = %v", got)
	}
	drag(v, tt, [2]int{1, 0}, [2]int{3, 0})
	want := []Highlight{{Row: 0, Start: 1, End: 4}}
	if got := v.highlights(); !reflect.DeepEqual(got, want) {
		t.Errorf("highlights = %v, want %v", got, want)
	}
}

func TestAStaleSelectionCopiesNothing(t *testing.T) {
	v, tt, _, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	drag(v, tt, [2]int{0, 0}, [2]int{2, 0})
	tt.SetClipboard("kept")

	// The surface no longer holds the pane the selection lies in.
	v.surface = twoPanesFocused(9, "p1")
	if v.copySelection() {
		t.Error("a stale selection copied")
	}
	wantClipboard(t, tt, "kept")
}

func TestAHoverMoveExtendsNothing(t *testing.T) {
	v, tt, n, _ := pointerView(t)
	showPage(v, tt, "abcdef")
	x0, y0 := cellPoint(v, tt, 0, 0, 0.2)
	x1, y1 := cellPoint(v, tt, 2, 0, 0.8)
	tt.Press(x0, y0)
	tt.Release(x0, y0)
	before := *n
	tt.Move(x1, y1) // no button held
	if *n != before {
		t.Error("a hover move asked for a frame")
	}
}
