// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

const (
	sSurface = 13
	sPatch   = 19
)

// plain is a cell with no colour, no style and no wide tail.
func plain(text string) Cell { return Cell{Text: text} }

// tail is the second column of a wide glyph.
func tail() Cell { return Cell{WideTail: true} }

func testPane(id string, focused bool) SurfacePane {
	return SurfacePane{
		PaneID:    id,
		Rect:      Rect{0, 0, 3, 1},
		InnerRect: Rect{0, 0, 2, 1},
		Focused:   focused,
	}
}

// blank is a cols × rows surface of blank cells, boot "b1", projection 7,
// revision 9.
func blank(cols, rows uint16) Surface {
	rs := []Row{}
	for i := 0; i < int(rows); i++ {
		r := make(Row, cols)
		for j := range r {
			r[j] = plain(" ")
		}
		rs = append(rs, r)
	}
	return Surface{
		BootID: "b1", ProjectionRevision: 7, Revision: 9,
		Size:  GridSize{Cols: cols, Rows: rows},
		Rows:  rs,
		Panes: []SurfacePane{},
	}
}

func testPatch(base, revision uint64, spans []Span) SurfacePatch {
	if spans == nil {
		spans = []Span{}
	}
	return SurfacePatch{
		BootID: "b1", ProjectionRevision: 7,
		BaseRevision: base, Revision: revision,
		Spans: spans, Panes: []SurfacePane{},
	}
}

func texts(r Row) []string {
	out := make([]string, len(r))
	for i, c := range r {
		out[i] = c.Text
	}
	return out
}

func putCell(e *Enc, c Cell) {
	e.Str(c.Text)
	e.Vu(uint64(c.Fg))
	e.Vu(uint64(c.Bg))
	e.Vu(uint64(c.Attrs))
	e.Bool(c.WideTail)
	e.None() // hyperlink
}

func putRect(e *Enc, r Rect) {
	e.Vu(uint64(r.X))
	e.Vu(uint64(r.Y))
	e.Vu(uint64(r.Width))
	e.Vu(uint64(r.Height))
}

func putPane(e *Enc, p SurfacePane) {
	e.Str(p.PaneID)
	e.Vu(0) // content_revision
	putRect(e, p.Rect)
	putRect(e, p.InnerRect)
	e.None() // scrollbar_rect
	e.None() // scroll
	e.Bool(p.Focused)
	e.Bool(false) // mouse_reporting
	e.Bool(false) // sgr_pixel_mouse
	e.Bool(p.AlternateScreen)
	e.Vu(0) // pixel_width
	e.Vu(0) // pixel_height
}

// encSurface writes a PaneSurface payload the way herdr lays it out: the
// cells of all rows in order, one hyperlink, three graphics bytes, then the
// tail (splits, popup, graphics scene) that decoding skips.
func encSurface(s Surface) []byte {
	var e Enc
	e.Vu(sSurface)
	e.Str(s.BootID)
	e.Vu(s.ProjectionRevision)
	e.Vu(s.Revision)
	var cells []Cell
	for _, r := range s.Rows {
		cells = append(cells, r...)
	}
	e.Vu(uint64(len(cells)))
	for _, c := range cells {
		putCell(&e, c)
	}
	e.Vu(uint64(s.Size.Cols))
	e.Vu(uint64(s.Size.Rows))
	if c := s.Cursor; c != nil {
		e.Some()
		e.Vu(uint64(c.X))
		e.Vu(uint64(c.Y))
		e.Bool(c.Visible)
		e.U8(c.Shape)
	} else {
		e.None()
	}
	e.Vu(1) // hyperlinks
	e.Str("h")
	e.Vu(3) // graphics
	e.Buf = append(e.Buf, 1, 2, 3)
	e.Vu(uint64(len(s.Panes)))
	for _, p := range s.Panes {
		putPane(&e, p)
	}
	// Tail: no splits, no popup, empty graphics scene.
	e.Vu(0)
	e.None()
	e.Vu(0)
	e.Vu(0)
	e.Vu(0)
	return e.Buf
}

// decodeSurface decodes a payload with tag 13, as the message decoder will.
func decodeSurface(payload []byte) (SurfaceFrame, error) {
	d := NewDec(payload)
	if tag, err := d.Vu(); err != nil || tag != sSurface {
		return SurfaceFrame{}, errors.New("not a surface payload")
	}
	return DecodeSurfaceFrame(d)
}

// decodePatch decodes a payload with tag 19.
func decodePatch(payload []byte) (SurfacePatch, error) {
	d := NewDec(payload)
	if tag, err := d.Vu(); err != nil || tag != sPatch {
		return SurfacePatch{}, errors.New("not a patch payload")
	}
	return DecodeSurfacePatch(d)
}

// frameGolden is a PaneSurface payload, byte by byte, from the layout above.
// Surface 3×1, boot "b1", projection 7, revision 9. Cells: "a" (fg indexed 1,
// bold), "界" (fg RGB 0x112233, italic, hyperlink 0), wide tail. Cursor (2,0)
// bar. Pane "w1:p1", focused, 300 px wide, 40 px high. The last five bytes
// are the tail.
var frameGolden = []byte{
	13,          // tag: PaneSurface
	2, 'b', '1', // boot_id
	7,                                   // projection_revision
	9,                                   // surface_revision
	3,                                   // cells: 3
	1, 'a', 252, 1, 0, 0, 1, 0, 1, 0, 0, // "a", fg 0x01000001, bg 0, attrs 1, skip false, no hyperlink
	3, 0xE7, 0x95, 0x8C, 252, 0x33, 0x22, 0x11, 0x02, 0, 4, 0, 1, 0, // "界", fg 0x02112233, attrs 4, hyperlink 0
	0, 0, 0, 0, 1, 0, // "" wide tail, skip true
	3, 1, // width 3, height 1
	1, 2, 0, 1, 5, // cursor: some, x 2, y 0, visible, shape 5
	1, 1, 'h', // hyperlinks: one URI "h"
	0,                          // graphics: no bytes
	1,                          // panes: 1
	5, 'w', '1', ':', 'p', '1', // pane_id "w1:p1"
	4,          // content_revision
	0, 0, 3, 1, // rect
	0, 0, 2, 1, // inner_rect
	0, 0, // scrollbar_rect none, scroll none
	1, 0, 0, 0, // focused, mouse, sgr pixel, alternate screen
	251, 44, 1, 40, // pixel_width 300, pixel_height 40
	0, 0, 0, 0, 0, // tail: splits 0, popup none, graphics 0/0/0
}

// goldenSurface is the surface that frameGolden decodes to.
func goldenSurface() Surface {
	p := testPane("w1:p1", true)
	return Surface{
		BootID: "b1", ProjectionRevision: 7, Revision: 9,
		Size: GridSize{Cols: 3, Rows: 1},
		Rows: []Row{{
			{Text: "a", Fg: 0x0100_0001, Attrs: 1},
			{Text: "界", Fg: 0x0211_2233, Attrs: 4},
			tail(),
		}},
		Cursor: &Cursor{X: 2, Y: 0, Visible: true, Shape: 5},
		Panes:  []SurfacePane{p},
	}
}

// patchGolden is a PaneSurfacePatch payload, byte by byte: boot "b1",
// projection 7, base 9 → revision 10. One span at x 1, y 1: "z" (fg indexed 2,
// dim) and a wide tail. One pane "w1:p1" (alternate screen on). Cursor (1,1),
// hidden.
var patchGolden = []byte{
	19,          // tag: PaneSurfacePatch
	2, 'b', '1', // boot_id
	7,       // projection_revision
	9,       // base_surface_revision
	10,      // surface_revision
	1,       // spans: 1
	1, 1, 2, // x 1, y 1, cells: 2
	1, 'z', 252, 2, 0, 0, 1, 0, 2, 0, 0, // "z", fg 0x01000002, attrs 2, skip false, no hyperlink
	0, 0, 0, 0, 1, 0, // "" wide tail
	1,                          // panes: 1
	5, 'w', '1', ':', 'p', '1', // pane_id "w1:p1"
	4,          // content_revision
	0, 0, 3, 1, // rect
	0, 0, 2, 1, // inner_rect
	0, 0, // scrollbar_rect none, scroll none
	0, 0, 0, 1, // focused, mouse, sgr pixel, alternate screen
	251, 44, 1, 40, // pixel_width 300, pixel_height 40
	1, 1, 1, 0, 0, // cursor: some, x 1, y 1, hidden, shape 0
}

func TestFrameGoldenBytesDecodeToTheSurface(t *testing.T) {
	got, err := decodeSurface(frameGolden)
	if err != nil {
		t.Fatal(err)
	}
	if want := (SurfaceFrame{goldenSurface()}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestFrameSkipsTheTailAfterThePanes(t *testing.T) {
	payload := bytes.Clone(frameGolden)
	n := len(payload)
	copy(payload[n-5:], []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) // a bad varint if read
	got, err := decodeSurface(payload)
	if err != nil {
		t.Fatal(err)
	}
	if want := (SurfaceFrame{goldenSurface()}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestFrameCutBeforeItsTailIsAnError(t *testing.T) {
	tailStart := len(frameGolden) - 5
	for n := 0; n < tailStart; n++ {
		if _, err := decodeSurface(frameGolden[:n]); err == nil {
			t.Fatalf("a frame cut at %d bytes decoded", n)
		}
	}
}

func TestFrameRoundTripsThroughTheLayout(t *testing.T) {
	wide := blank(3, 2)
	wide.Rows = []Row{
		{plain("a"), plain("界"), tail()},
		{plain("x"), plain(" "), plain("é")},
	}
	wide.Cursor = &Cursor{X: 1, Y: 1}
	wide.Panes = []SurfacePane{testPane("w1:p1", true), testPane("w1:p2", false)}
	empty := blank(1, 1)
	empty.Size = GridSize{}
	empty.Rows = []Row{}
	zeroWide := blank(1, 1)
	zeroWide.Size = GridSize{Cols: 0, Rows: 2}
	zeroWide.Rows = []Row{{}, {}}
	for _, s := range []Surface{goldenSurface(), wide, empty, zeroWide} {
		got, err := decodeSurface(encSurface(s))
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		if !reflect.DeepEqual(got, SurfaceFrame{s}) {
			t.Fatalf("got %+v\nwant %+v", got, s)
		}
	}
}

func TestFrameWithAWrongCellCountIsInvalid(t *testing.T) {
	s := blank(2, 2)
	s.Rows = s.Rows[:1] // two cells left for a 2×2 surface
	_, err := decodeSurface(encSurface(s))
	var ce *CodecError
	if !errors.As(err, &ce) || ce.Kind != Invalid || ce.What != "frame size" {
		t.Fatalf("got %v, want Invalid frame size", err)
	}
}

func TestCellCountOverHerdrLimitIsRefusedBeforeAnyCellIsRead(t *testing.T) {
	// One past the limit, with that many bytes after the count so only the
	// cap can refuse it.
	var e Enc
	e.Vu(maxCells + 1)
	e.Buf = append(e.Buf, make([]byte, maxCells+1)...)
	_, err := readCells(NewDec(e.Buf))
	var ce *CodecError
	if !errors.As(err, &ce) || ce.Kind != Invalid {
		t.Fatalf("got %v, want Invalid", err)
	}
}

func TestCellCountAtHerdrLimitIsRead(t *testing.T) {
	var e Enc
	e.Vu(maxCells)
	for i := 0; i < maxCells; i++ {
		putCell(&e, plain(""))
	}
	cells, err := readCells(NewDec(e.Buf))
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != maxCells {
		t.Fatalf("got %d cells", len(cells))
	}
}

func TestPatchGoldenBytesDecode(t *testing.T) {
	got, err := decodePatch(patchGolden)
	if err != nil {
		t.Fatal(err)
	}
	pane := testPane("w1:p1", false)
	pane.AlternateScreen = true
	want := SurfacePatch{
		BootID: "b1", ProjectionRevision: 7, BaseRevision: 9, Revision: 10,
		Spans: []Span{{X: 1, Y: 1, Cells: []Cell{
			{Text: "z", Fg: 0x0100_0002, Attrs: 2},
			tail(),
		}}},
		Panes:  []SurfacePane{pane},
		Cursor: &Cursor{X: 1, Y: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestPaneScrollMetricsAndMouseReportingAreRead(t *testing.T) {
	// The optional scrollbar rect and scroll metrics are present here, so the
	// fields after them only line up if the rect is skipped and the metrics
	// are read.
	var e Enc
	e.Vu(sPatch)
	e.Str("b1")
	e.Vu(7)
	e.Vu(9)
	e.Vu(10)
	e.Vu(0) // spans
	e.Vu(1) // panes
	e.Str("w1:p1")
	e.Vu(4) // content_revision
	putRect(&e, Rect{0, 0, 3, 1})
	putRect(&e, Rect{0, 0, 2, 1})
	e.Some()
	putRect(&e, Rect{2, 0, 1, 1}) // scrollbar_rect
	e.Some()
	e.Vu(5)       // offset_from_bottom
	e.Vu(300)     // max_offset_from_bottom
	e.Vu(2)       // viewport_rows
	e.Bool(true)  // focused
	e.Bool(true)  // mouse_reporting
	e.Bool(true)  // sgr_pixel_mouse
	e.Bool(false) // alternate_screen_active
	e.Vu(0)       // pixel_width
	e.Vu(0)       // pixel_height
	e.None()      // cursor
	got, err := decodePatch(e.Buf)
	if err != nil {
		t.Fatal(err)
	}
	pane := testPane("w1:p1", true)
	pane.MouseReporting = true
	pane.Scroll = &ScrollMetrics{OffsetFromBottom: 5, MaxOffsetFromBottom: 300, ViewportRows: 2}
	want := testPatch(9, 10, nil)
	want.Panes = []SurfacePane{pane}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestPaneWithoutScrollMetricsHasNone(t *testing.T) {
	// frameGolden sends `scroll none` and mouse reporting off.
	f, err := decodeSurface(frameGolden)
	if err != nil {
		t.Fatal(err)
	}
	pane := f.Surface.Panes[0]
	if pane.Scroll != nil || pane.MouseReporting {
		t.Fatalf("got %+v", pane)
	}
}

func TestPatchCutAnywhereIsAnError(t *testing.T) {
	for n := 0; n < len(patchGolden); n++ {
		if _, err := decodePatch(patchGolden[:n]); err == nil {
			t.Fatalf("a patch cut at %d bytes decoded", n)
		}
	}
}

func TestPatchChangesOnlyTheRowItNames(t *testing.T) {
	s := blank(3, 2)
	row0 := s.Rows[0]
	old1 := s.Rows[1]
	if err := s.ApplyPatch(testPatch(9, 10, []Span{{0, 1, []Cell{plain("x"), plain("y")}}})); err != nil {
		t.Fatal(err)
	}
	if &row0[0] != &s.Rows[0][0] {
		t.Fatal("row 0 was copied")
	}
	if got := texts(s.Rows[1]); !reflect.DeepEqual(got, []string{"x", "y", " "}) {
		t.Fatalf("row 1 = %q", got)
	}
	if old1[0].Text != " " {
		t.Fatal("the old row 1 was changed in place")
	}
	if s.Revision != 10 {
		t.Fatalf("revision = %d", s.Revision)
	}
}

func TestPatchSpanStartsAtItsColumn(t *testing.T) {
	s := blank(3, 1)
	if err := s.ApplyPatch(testPatch(9, 10, []Span{{1, 0, []Cell{plain("x")}}})); err != nil {
		t.Fatal(err)
	}
	if got := texts(s.Rows[0]); !reflect.DeepEqual(got, []string{" ", "x", " "}) {
		t.Fatalf("row 0 = %q", got)
	}
}

func TestPatchWithNoSpansMovesOnlyTheRevisionAndCursor(t *testing.T) {
	s := blank(3, 1)
	before := append([]Row(nil), s.Rows...)
	p := testPatch(9, 10, nil)
	p.Cursor = &Cursor{Visible: true, Shape: 6}
	if err := s.ApplyPatch(p); err != nil {
		t.Fatal(err)
	}
	if s.Revision != 10 || s.Cursor == nil || s.Cursor.Shape != 6 {
		t.Fatalf("got %+v", s)
	}
	for i := range before {
		if &before[i][0] != &s.Rows[i][0] {
			t.Fatalf("row %d was copied", i)
		}
	}
}

func cloneSurface(s Surface) Surface {
	c := s
	c.Rows = make([]Row, len(s.Rows))
	for i, r := range s.Rows {
		c.Rows[i] = append(Row(nil), r...)
	}
	c.Panes = append([]SurfacePane{}, s.Panes...)
	return c
}

func TestStaleBaseRevisionIsRefusedAndNothingChanges(t *testing.T) {
	s := blank(3, 2)
	before := cloneSurface(s)
	err := s.ApplyPatch(testPatch(8, 10, []Span{{0, 0, []Cell{plain("x")}}}))
	if err != ErrStale {
		t.Fatalf("got %v, want ErrStale", err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("the surface changed")
	}
}

func TestOtherBootIsStale(t *testing.T) {
	s := blank(3, 2)
	p := testPatch(9, 10, nil)
	p.BootID = "b2"
	if err := s.ApplyPatch(p); err != ErrStale {
		t.Fatalf("got %v, want ErrStale", err)
	}
}

func TestOtherProjectionIsStale(t *testing.T) {
	s := blank(3, 2)
	p := testPatch(9, 10, nil)
	p.ProjectionRevision = 8
	if err := s.ApplyPatch(p); err != ErrStale {
		t.Fatalf("got %v, want ErrStale", err)
	}
}

func TestSpanPastTheWidthIsOutOfRangeAndNothingApplies(t *testing.T) {
	s := blank(3, 2)
	before := cloneSurface(s)
	err := s.ApplyPatch(testPatch(9, 10, []Span{
		{0, 0, []Cell{plain("x")}},
		{2, 1, []Cell{plain("y"), plain("z")}},
	}))
	if err != ErrOutOfRange {
		t.Fatalf("got %v, want ErrOutOfRange", err)
	}
	if !reflect.DeepEqual(s, before) {
		t.Fatal("the first span was applied")
	}
}

func TestSpanBelowTheLastRowIsOutOfRange(t *testing.T) {
	s := blank(3, 2)
	err := s.ApplyPatch(testPatch(9, 10, []Span{{0, 2, []Cell{plain("x")}}}))
	if err != ErrOutOfRange {
		t.Fatalf("got %v, want ErrOutOfRange", err)
	}
}

func TestPatchCursorNoneClearsTheCursor(t *testing.T) {
	s := blank(3, 1)
	s.Cursor = &Cursor{Visible: true, Shape: 1}
	if err := s.ApplyPatch(testPatch(9, 10, nil)); err != nil {
		t.Fatal(err)
	}
	if s.Cursor != nil {
		t.Fatalf("cursor = %+v", s.Cursor)
	}
}

func TestPatchReplacesAPaneByIDAndAddsAnUnknownOne(t *testing.T) {
	s := blank(3, 1)
	s.Panes = []SurfacePane{testPane("w1:p1", true)}
	p := testPatch(9, 10, nil)
	p.Panes = []SurfacePane{testPane("w1:p1", false), testPane("w1:p2", true)}
	if err := s.ApplyPatch(p); err != nil {
		t.Fatal(err)
	}
	if len(s.Panes) != 2 || s.Panes[0].Focused {
		t.Fatalf("panes = %+v", s.Panes)
	}
	if f := s.FocusedPane(); f == nil || f.PaneID != "w1:p2" {
		t.Fatalf("focused = %+v", f)
	}
}

func TestFocusedPaneIsNoneWhenNoPaneHasFocus(t *testing.T) {
	s := blank(3, 1)
	s.Panes = []SurfacePane{testPane("w1:p1", false)}
	if s.FocusedPane() != nil {
		t.Fatal("got a focused pane")
	}
}
