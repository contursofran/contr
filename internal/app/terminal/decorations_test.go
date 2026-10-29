package terminal

import (
	"reflect"
	"testing"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

const (
	redFg    = 0x02E06C6C
	greenFg  = 0x0200FF00
	redRGB   = 0xE06C6C
	greenRGB = 0x00FF00
	// Eight pixels a cell and twenty a row: a bar sits at y 18, its double
	// at 16, a wave at 16.
	decCellW float32 = 8
	decCellH float32 = 20
)

func decCell(text string, attrs uint16) endpoint.Cell {
	return endpoint.Cell{Text: text, Attrs: attrs}
}

func decColored(text string, attrs uint16, fg uint32) endpoint.Cell {
	c := decCell(text, attrs)
	c.Fg = fg
	return c
}

type bar [3]float32

// bars returns the marks as (x0, x1, y). A wave fails the test.
func bars(t *testing.T, ms []mark) []bar {
	t.Helper()
	var out []bar
	for _, m := range ms {
		if m.Wave {
			t.Fatalf("expected a bar, got %+v", m)
		}
		out = append(out, bar{m.X0, m.X1, m.Y})
	}
	return out
}

func wantBars(t *testing.T, got []mark, want []bar) {
	t.Helper()
	if g := bars(t, got); !reflect.DeepEqual(g, want) {
		t.Errorf("bars = %v, want %v", g, want)
	}
}

func wantRuns(t *testing.T, got, want []underlineRun) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runs = %+v, want %+v", got, want)
	}
}

func spans(runs []underlineRun) [][2]int {
	var out [][2]int
	for _, r := range runs {
		out = append(out, [2]int{r.Col, r.Cols})
	}
	return out
}

func TestHerdrBitsNameTheUnderlineStyle(t *testing.T) {
	style := func(attrs uint16) UnderlineKind { return underlineOf(decCell("a", attrs)) }
	cases := []struct {
		attrs uint16
		want  UnderlineKind
	}{
		{0, UnderlineNone},
		{Underline, UnderlineSingle},
		// Style 1 is single too, and a style without the underline bit
		// still underlines.
		{0x1000, UnderlineSingle},
		{Underline | 0x2000, UnderlineDouble},
		{Underline | 0x3000, UnderlineCurly},
		{Underline | 0x4000, UnderlineDotted},
		{Underline | 0x5000, UnderlineDashed},
		// herdr sends no other style. Any other value paints as single.
		{Underline | 0x6000, UnderlineSingle},
	}
	for _, c := range cases {
		if got := style(c.attrs); got != c.want {
			t.Errorf("style(%#x) = %v, want %v", c.attrs, got, c.want)
		}
	}
}

func TestAHiddenCellIsNotUnderlined(t *testing.T) {
	for _, attrs := range []uint16{Hidden | Underline, Hidden | 0x3000} {
		if got := underlineOf(decCell("a", attrs)); got != UnderlineNone {
			t.Errorf("underlineOf(%#x) = %v, want none", attrs, got)
		}
	}
}

func TestCellsWithOneStyleAndColourMakeOneRun(t *testing.T) {
	row := []endpoint.Cell{
		decColored("a", Underline, redFg),
		decColored("b", Underline, redFg),
		decColored(" ", Underline, redFg),
	}
	wantRuns(t, underlineRuns(row), []underlineRun{{0, 3, UnderlineSingle, redRGB}})
}

func TestAChangeOfStyleOrColourStartsARun(t *testing.T) {
	row := []endpoint.Cell{
		decColored("a", Underline, redFg),
		decColored("b", Underline|0x3000, redFg),
		decColored("c", Underline|0x3000, greenFg),
	}
	wantRuns(t, underlineRuns(row), []underlineRun{
		{0, 1, UnderlineSingle, redRGB},
		{1, 1, UnderlineCurly, redRGB},
		{2, 1, UnderlineCurly, greenRGB},
	})
}

func TestAPlainOrHiddenCellEndsARun(t *testing.T) {
	want := [][2]int{{0, 1}, {2, 1}}
	plain := []endpoint.Cell{decCell("a", Underline), decCell("b", 0), decCell("c", Underline)}
	if got := spans(underlineRuns(plain)); !reflect.DeepEqual(got, want) {
		t.Errorf("plain spans = %v, want %v", got, want)
	}
	hidden := []endpoint.Cell{decCell("a", Underline), decCell("b", Hidden|Underline), decCell("c", Underline)}
	if got := spans(underlineRuns(hidden)); !reflect.DeepEqual(got, want) {
		t.Errorf("hidden spans = %v, want %v", got, want)
	}
}

func TestABlankCellAndTheTailOfAWideGlyphAreUnderlined(t *testing.T) {
	tail := decCell("", Underline)
	tail.WideTail = true
	row := []endpoint.Cell{decCell(" ", Underline), decCell("界", Underline), tail}
	fg, _, _ := CellColors(row[0])
	wantRuns(t, underlineRuns(row), []underlineRun{{0, 3, UnderlineSingle, fg}})
}

func TestSingleIsOneBarTwoPixelsAboveTheBottom(t *testing.T) {
	wantBars(t, marks(UnderlineSingle, 0, 16, decCellH), []bar{{0, 16, 18}})
}

func TestDoubleIsTwoBarsTwoAndFourPixelsAboveTheBottom(t *testing.T) {
	wantBars(t, marks(UnderlineDouble, 0, 16, decCellH), []bar{{0, 16, 16}, {0, 16, 18}})
}

func TestCurlyIsOneWaveThatStartsFourPixelsAboveTheBottom(t *testing.T) {
	got := marks(UnderlineCurly, 0, 16, decCellH)
	want := []mark{{X0: 0, X1: 16, Y: 16, Wave: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("marks = %+v, want %+v", got, want)
	}
}

func TestDottedIsOnePixelDotsEveryTwoPixelsFromTheGridOrigin(t *testing.T) {
	// The second cell: dots start at 8, 10, 12 and 14.
	wantBars(t, marks(UnderlineDotted, decCellW, 2*decCellW, decCellH),
		[]bar{{8, 9, 18}, {10, 11, 18}, {12, 13, 18}, {14, 15, 18}})
}

func TestDashedIsFourPixelDashesEverySevenPixelsFromTheGridOrigin(t *testing.T) {
	// Dashes start at 0, 7, 14, 21 and so on. The second and third cells
	// hold 8 to 24.
	wantBars(t, marks(UnderlineDashed, decCellW, 3*decCellW, decCellH),
		[]bar{{8, 11, 18}, {14, 18, 18}, {21, 24, 18}})
}

func TestADashAcrossACellEdgeIsTwoPiecesThatMeetThere(t *testing.T) {
	// The dash at 7 to 11 is 7 to 8 in the first cell and 8 to 11 in the second.
	wantBars(t, marks(UnderlineDashed, 0, decCellW, decCellH), []bar{{0, 4, 18}, {7, 8, 18}})
	wantBars(t, marks(UnderlineDashed, decCellW, 2*decCellW, decCellH), []bar{{8, 11, 18}, {14, 16, 18}})
}

func TestEachStylePaintsInsideItsCells(t *testing.T) {
	// Every style paints marks, and each mark stays in its span and row.
	styles := []UnderlineKind{UnderlineSingle, UnderlineDouble, UnderlineCurly, UnderlineDotted, UnderlineDashed}
	for _, kind := range styles {
		found := marks(kind, decCellW, 5*decCellW, decCellH)
		if len(found) == 0 {
			t.Errorf("%v paints nothing", kind)
		}
		for _, m := range found {
			height := float32(1)
			if m.Wave {
				height = 3
			}
			if !(m.X0 >= decCellW && m.X1 <= 5*decCellW && m.X0 < m.X1) {
				t.Errorf("%v: %+v outside span", kind, m)
			}
			if !(m.Y >= 0 && m.Y+height <= decCellH) {
				t.Errorf("%v: %+v outside row", kind, m)
			}
		}
	}
}

func TestNoUnderlineOrEmptySpanPaintsNothing(t *testing.T) {
	for name, got := range map[string][]mark{
		"none":     marks(UnderlineNone, 0, 16, decCellH),
		"empty":    marks(UnderlineSingle, decCellW, decCellW, decCellH),
		"reversed": marks(UnderlineDashed, 2*decCellW, decCellW, decCellH),
	} {
		if len(got) != 0 {
			t.Errorf("%s: marks = %+v, want none", name, got)
		}
	}
}
