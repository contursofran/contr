package terminal

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

const (
	red   uint32 = 0x02E06C6C
	green uint32 = 0x0200FF00
	plain uint32 = 0
)

func cellOf(text string, attrs uint16) endpoint.Cell {
	return endpoint.Cell{Text: text, Fg: plain, Bg: plain, Attrs: attrs}
}

func coloured(text string, fg uint32) endpoint.Cell {
	c := cellOf(text, 0)
	c.Fg = fg
	return c
}

func tail() endpoint.Cell {
	c := cellOf("", 0)
	c.WideTail = true
	return c
}

type colText struct {
	Col  int
	Text string
}

func texts(row []endpoint.Cell) []colText {
	var out []colText
	for _, s := range textSpans(row) {
		out = append(out, colText{s.Col, s.Text})
	}
	return out
}

func eq[T comparable](t *testing.T, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAsciiCellsWithOneStyleMakeOneSpan(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("a", 0), cellOf("b", 0), cellOf("c", 0)}), []colText{{0, "abc"}})
}

func TestAStyleChangeStartsANewSpan(t *testing.T) {
	eq(t, texts([]endpoint.Cell{coloured("a", red), coloured("b", green)}), []colText{{0, "a"}, {1, "b"}})
}

func TestSpacesInsideALineStayInTheSpanSoColumnsHold(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("a", 0), cellOf(" ", 0), cellOf("b", 0)}), []colText{{0, "a b"}})
}

func TestABlankRunMakesNoSpanEvenWhenUnderlined(t *testing.T) {
	// An underline is drawn per cell, so a blank run needs no span for it.
	if got := textSpans([]endpoint.Cell{cellOf(" ", 0), cellOf(" ", 0)}); len(got) != 0 {
		t.Errorf("plain blanks: %v", got)
	}
	if got := textSpans([]endpoint.Cell{cellOf(" ", Underline)}); len(got) != 0 {
		t.Errorf("underlined blank: %v", got)
	}
}

func TestAStruckBlankRunStillMakesASpan(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf(" ", Strikethrough)}), []colText{{0, " "}})
}

func TestAnItalicCellStartsANewSpanAndKeepsItsColumn(t *testing.T) {
	row := []endpoint.Cell{cellOf("a", 0), cellOf("b", Italic), cellOf("c", Italic)}
	eq(t, texts(row), []colText{{0, "a"}, {1, "bc"}})
}

func TestTheRunFontIsItalicOnlyWhenTheStyleAsks(t *testing.T) {
	plainStyle := Style{Fg: theme.Text}
	italic := plainStyle
	italic.Italic = true
	boldItalic := italic
	boldItalic.Bold = true
	for _, c := range []struct {
		style  Style
		italic bool
		weight int
	}{{plainStyle, false, 400}, {italic, true, 400}, {boldItalic, true, 700}} {
		f := terminalFont(c.style)
		if f.Italic != c.italic || f.Weight != c.weight {
			t.Errorf("%+v: italic %v weight %d", c.style, f.Italic, f.Weight)
		}
	}
}

func TestANonAsciiCellIsItsOwnSpanAtItsColumn(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("a", 0), cellOf("é", 0), cellOf("b", 0)}), []colText{{0, "a"}, {1, "é"}, {2, "b"}})
}

func TestAnAsciiCellNeverJoinsANonAsciiSpanAcrossAGap(t *testing.T) {
	// "é" is two bytes but one column, so byte length must not stand in for columns.
	row := []endpoint.Cell{cellOf("é", 0), cellOf("h", Hidden), cellOf("a", 0)}
	eq(t, texts(row), []colText{{0, "é"}, {2, "a"}})
}

func TestAWideGlyphSkipsItsTailAndTheNextTextKeepsItsColumn(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("界", 0), tail(), cellOf("x", 0)}), []colText{{0, "界"}, {2, "x"}})
}

func TestHiddenCellsMakeNoSpanSoNothingIsShapedForThem(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("a", Hidden), cellOf("b", 0)}), []colText{{1, "b"}})
	if got := textSpans([]endpoint.Cell{cellOf("a", Hidden), cellOf("é", Hidden)}); len(got) != 0 {
		t.Errorf("only hidden: %v", got)
	}
}

func TestBoldItalicAndStrikeAreReadFromAttrs(t *testing.T) {
	style := func(attrs uint16) Style { return styleOf(cellOf("a", attrs)) }
	if !style(Bold).Bold || !style(Italic).Italic || !style(Strikethrough).Strike {
		t.Error("a flag is not read")
	}
	if s := style(0); s.Bold || s.Italic || s.Strike {
		t.Errorf("no flags: %+v", s)
	}
}

func TestBackgroundRunsMergeEqualColoursAndSkipTheDefault(t *testing.T) {
	bg := func(c uint32) endpoint.Cell {
		cell := cellOf(" ", 0)
		cell.Bg = c
		return cell
	}
	row := []endpoint.Cell{bg(0x0200FF00), bg(0x0200FF00), bg(plain), bg(0x0200FF00), bg(0x02000001)}
	want := []bgRun{{0, 2, 0x00FF00}, {3, 1, 0x00FF00}, {4, 1, 0x000001}}
	eq(t, backgroundRuns(row), want)
}

func TestTheCursorIsClampedToTheGrid(t *testing.T) {
	grid := endpoint.GridSize{Cols: 80, Rows: 24}
	at := func(x, y uint16) endpoint.Cursor { return endpoint.Cursor{X: x, Y: y, Visible: true} }
	for _, c := range []struct{ in, want [2]uint16 }{
		{[2]uint16{10, 5}, [2]uint16{10, 5}},
		// herdr may put it one past the last column or row.
		{[2]uint16{80, 24}, [2]uint16{79, 23}},
		{[2]uint16{900, 900}, [2]uint16{79, 23}},
	} {
		x, y := cursorCell(at(c.in[0], c.in[1]), grid)
		if x != c.want[0] || y != c.want[1] {
			t.Errorf("%v: got %d,%d", c.in, x, y)
		}
	}
}

func TestCursorShapesFillTheBlockTheBarOrTheUnderline(t *testing.T) {
	cell := ui.Rect{X: 10, Y: 20, W: 8, H: 20}
	if got := cursorRect(cell, 2); got != cell {
		t.Errorf("block: %v", got)
	}
	if got, want := cursorRect(cell, 5), (ui.Rect{X: 10, Y: 20, W: 2, H: 20}); got != want {
		t.Errorf("bar: %v", got)
	}
	if got, want := cursorRect(cell, 3), (ui.Rect{X: 10, Y: 38, W: 8, H: 2}); got != want {
		t.Errorf("underline: %v", got)
	}
}

// rows of one cell each, as herdr sends them: a new slice for a changed row.
func oneCell(text string) endpoint.Row { return endpoint.Row{cellOf(text, 0)} }

func TestARowHerdrSendsAgainIsShapedOnlyWhenItIsANewRow(t *testing.T) {
	first, second := oneCell("a"), oneCell("b")
	var cache []rowEntry[string]
	var shaped []string
	shapeRow := func(row []endpoint.Cell) []string {
		shaped = append(shaped, row[0].Text)
		return []string{row[0].Text}
	}

	cache = refreshRows(cache, []endpoint.Row{first, second}, shapeRow)
	eq(t, shaped, []string{"a", "b"})

	// The same rows again, as herdr sends them with a patch that leaves them
	// alone: no shaping.
	cache = refreshRows(cache, []endpoint.Row{first, second}, shapeRow)
	eq(t, shaped, []string{"a", "b"})

	// A patch that replaces the second row: only that row is shaped again.
	cache = refreshRows(cache, []endpoint.Row{first, oneCell("c")}, shapeRow)
	eq(t, shaped, []string{"a", "b", "c"})

	// A shorter surface drops the rows below it: nothing is shaped.
	cache = refreshRows(cache, []endpoint.Row{first}, shapeRow)
	eq(t, shaped, []string{"a", "b", "c"})
	if len(cache) != 1 {
		t.Errorf("cache has %d rows", len(cache))
	}
}

// A real patch puts a new row in the surface and leaves the others alone.
func TestAPatchThatChangesOneRowShapesOneRow(t *testing.T) {
	s := endpoint.Surface{BootID: "b", Revision: 1, Size: endpoint.GridSize{Cols: 1, Rows: 3},
		Rows: []endpoint.Row{oneCell("a"), oneCell("b"), oneCell("c")}}
	var cache []rowEntry[string]
	n := 0
	shapeRow := func(row []endpoint.Cell) []string { n++; return []string{row[0].Text} }
	cache = refreshRows(cache, s.Rows, shapeRow)
	patch := endpoint.SurfacePatch{BootID: "b", BaseRevision: 1, Revision: 2,
		Spans: []endpoint.Span{{X: 0, Y: 1, Cells: []endpoint.Cell{cellOf("x", 0)}}}}
	if err := s.ApplyPatch(patch); err != nil {
		t.Fatal(err)
	}
	cache = refreshRows(cache, s.Rows, shapeRow)
	if n != 4 {
		t.Errorf("shaped %d rows, want 3 then 1", n)
	}
	if cache[1].lines[0] != "x" {
		t.Errorf("row 1 is %v", cache[1].lines)
	}
}

func TestANonAsciiOrEmptyTextIsNotAPlainChar(t *testing.T) {
	for text, want := range map[string]bool{"a": true, " ": true, "": false, "ab": false, "é": false, "\t": false} {
		if got := isASCIIChar(text); got != want {
			t.Errorf("%q: got %v", text, got)
		}
	}
}

func TestABoxGlyphMakesNoTextSpanSoItIsNotShaped(t *testing.T) {
	eq(t, texts([]endpoint.Cell{cellOf("a", 0), cellOf("│", 0), cellOf("b", 0)}), []colText{{0, "a"}, {2, "b"}})
}

func TestABoxGlyphCellIsALineGlyphInItsColour(t *testing.T) {
	line := coloured("─", red)
	if g, ok := quadGlyph(line); !ok || g.IsBlock {
		t.Errorf("not a line glyph: %+v %v", g, ok)
	}
	if fg, _, _ := CellColors(line); fg != 0xE06C6C {
		t.Errorf("colour %06X", fg)
	}
	if _, ok := quadGlyph(cellOf("a", 0)); ok {
		t.Error("a is a box glyph")
	}
}

func TestAHiddenBoxGlyphPaintsNothingAndAWideTailIsNotAGlyph(t *testing.T) {
	hidden := cellOf("│", Hidden)
	if _, ok := quadGlyph(hidden); ok {
		t.Error("hidden glyph paints")
	}
	if got := textSpans([]endpoint.Cell{hidden}); len(got) != 0 {
		t.Errorf("hidden spans: %v", got)
	}
	wide := tail()
	wide.Text = "│"
	if _, ok := quadGlyph(wide); ok {
		t.Error("wide tail paints")
	}
}

func TestATextOfMoreThanOneCharIsNotABoxGlyph(t *testing.T) {
	cell := cellOf("│a", 0)
	if _, ok := quadGlyph(cell); ok {
		t.Error("two chars are a box glyph")
	}
	eq(t, texts([]endpoint.Cell{cell}), []colText{{0, "│a"}})
}

func TestTheTerminalFontKeepsItsFamilyAndTurnsTheLigatureFeaturesOff(t *testing.T) {
	f := terminalFont(Style{})
	if f.Family != theme.FontFamily {
		t.Errorf("family %q", f.Family)
	}
	for _, tag := range []string{"calt=0", "clig=0", "liga=0"} {
		if !strings.Contains(f.Features, tag) {
			t.Errorf("%s should be off: %q", tag, f.Features)
		}
	}
}

func TestTheSelectionTintIsWhiteAtAQuarter(t *testing.T) {
	if selectionRGBA != 0xFFFFFF40 {
		t.Errorf("tint %08X", selectionRGBA)
	}
	// 25 % of 255 is 63.75, which rounds to 64.
	if selectionRGBA&0xFF != 64 {
		t.Errorf("alpha %d", selectionRGBA&0xFF)
	}
	if c := rgbaColor(selectionRGBA); c != (ui.Color{R: 255, G: 255, B: 255, A: 64}) {
		t.Errorf("color %+v", c)
	}
}

func TestASelectionTintCoversTheCellsOfItsRow(t *testing.T) {
	m := Metrics{CellW: 8, CellH: 20}
	origin := ui.Point{X: 10, Y: 20}
	got, ok := highlightRect(origin, m, Highlight{Row: 2, Start: 3, End: 5}, 4)
	if !ok {
		t.Fatal("no tint over cells the surface holds")
	}
	if want := (ui.Rect{X: 34, Y: 60, W: 16, H: 20}); got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	// A row the surface does not hold, and an empty span, paint nothing.
	if _, ok := highlightRect(origin, m, Highlight{Row: 4, Start: 0, End: 1}, 4); ok {
		t.Error("a row past the surface has a tint")
	}
	if _, ok := highlightRect(origin, m, Highlight{Row: 0, Start: 2, End: 2}, 4); ok {
		t.Error("an empty span has a tint")
	}
}

func TestTextThatMovesToAnotherRowIsNotShapedAgain(t *testing.T) {
	var m memo
	n := 0
	make := func() []ui.Glyph { n++; return nil }
	plainKey := shapeKey{text: "build ok"}
	m.get(plainKey, make)
	m.get(plainKey, make)
	if n != 1 {
		t.Fatalf("shaped %d times, want 1", n)
	}
	// The same text in another face is another entry.
	m.get(shapeKey{text: "build ok", bold: true}, make)
	if n != 2 {
		t.Fatalf("shaped %d times, want 2", n)
	}
}

func TestTheShapeMemoIsBounded(t *testing.T) {
	var m memo
	for i := 0; i < 3*memoLimit; i++ {
		m.get(shapeKey{text: string(rune('a'+i%26)) + strings.Repeat("x", i)}, func() []ui.Glyph { return nil })
		if len(m.glyphs) > memoLimit {
			t.Fatalf("memo holds %d entries", len(m.glyphs))
		}
	}
}
