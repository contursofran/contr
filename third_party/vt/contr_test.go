// Tests of the changes for contr (see the header of screen.go, scrollback.go
// and utf8.go).

package vt

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// fillScreen gives each cell of s a distinct content and some styles, with a
// wide glyph in each line.
func fillScreen(s *Screen) {
	w, h := s.Width(), s.Height()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := uv.Cell{Content: string(rune('a' + (x+y)%26)), Width: 1}
			if y%2 == 0 {
				c.Style.Fg = ansi.BasicColor(y % 8)
				c.Style.Attrs = uv.AttrBold
			}
			s.buf.SetCell(x, y, &c)
		}
		s.buf.SetCell(2, y, &uv.Cell{Content: "日", Width: 2})
	}
}

// TestDeleteFullLinesMatchesCellCopy checks that moving whole lines gives the
// same cells and touched lines as the cell copy it replaces.
func TestDeleteFullLinesMatchesCellCopy(t *testing.T) {
	const w, h = 7, 6
	for _, tc := range []struct {
		top, bottom, y, n int
		bg                bool
	}{
		{0, h, 0, 1, false}, // a scroll of the whole screen
		{0, h, 0, 1, true},  // the pen background fills the new line
		{0, h, 2, 3, false},
		{0, h, 0, h, true},  // all lines
		{0, h, 1, 99, true}, // more lines than the region has
		{1, 4, 1, 1, true},  // top and bottom margins
		{1, 4, 3, 2, false},
		{2, 3, 2, 1, false},
		{0, h, 0, 5, true}, // more lines than the stash of deleteFullLines
	} {
		t.Run(fmt.Sprintf("%+v", tc), func(t *testing.T) {
			want, got := NewScreen(w, h), NewScreen(w, h)
			region := uv.Rect(0, tc.top, w, tc.bottom-tc.top)
			for _, s := range []*Screen{want, got} {
				fillScreen(s)
				s.scroll = region
				s.buf.Touched = nil
				if tc.bg {
					s.cur.Pen.Bg = ansi.IndexedColor(42)
				}
			}
			want.buf.DeleteLineArea(tc.y, tc.n, want.blankCell(), region)
			got.deleteFullLines(tc.y, tc.n, region)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if wc, gc := want.CellAt(x, y), got.CellAt(x, y); !wc.Equal(gc) {
						t.Errorf("cell %d,%d: got %+v, want %+v", x, y, *gc, *wc)
					}
				}
			}
			if len(got.buf.Touched) != len(want.buf.Touched) {
				t.Fatalf("touched lines: got %d, want %d", len(got.buf.Touched), len(want.buf.Touched))
			}
			for y := range want.buf.Touched {
				if wt, gt := want.buf.Touched[y], got.buf.Touched[y]; (wt == nil) != (gt == nil) || wt != nil && *wt != *gt {
					t.Errorf("touched line %d: got %+v, want %+v", y, gt, wt)
				}
			}
		})
	}
}

// TestDeleteLineKeepsLinesApart checks that the lines stay separate after
// they move: a write to one line does not change another.
func TestDeleteLineKeepsLinesApart(t *testing.T) {
	e := NewEmulator(4, 3)
	_, _ = e.WriteString("aaaa\r\nbbbb\r\ncccc\r\ndddd\r\neeee")
	for y, want := range []string{"cccc", "dddd", "eeee"} {
		if got := lineText(e, y); got != want {
			t.Errorf("line %d: got %q, want %q", y, got, want)
		}
	}
	for i, want := range []string{"aaaa", "bbbb"} {
		if got := e.ScrollbackCellAt(0, i).Content; got != want[:1] {
			t.Errorf("history %d: got %q, want %q", i, got, want[:1])
		}
	}
}

func lineText(e *Emulator, y int) string {
	var s string
	for x := 0; x < e.Width(); x++ {
		s += e.CellAt(x, y).Content
	}
	return s
}

func line(s string) uv.Line {
	l := make(uv.Line, len(s))
	for i, r := range s {
		l[i] = uv.Cell{Content: string(r), Width: 1}
	}
	return l
}

func contents(sb *Scrollback) []string {
	var out []string
	for _, l := range sb.Lines() {
		var s string
		for _, c := range l {
			s += c.Content
		}
		out = append(out, s)
	}
	return out
}

func TestScrollbackRing(t *testing.T) {
	sb := NewScrollback(3)
	for _, s := range []string{"a", "b", "c", "d", "ee"} {
		sb.Push(line(s))
	}
	check := func(step string, want ...string) {
		t.Helper()
		got := contents(sb)
		if fmt.Sprint(got) != fmt.Sprint(want) || sb.Len() != len(want) {
			t.Fatalf("%s: lines %q (len %d), want %q", step, got, sb.Len(), want)
		}
		for i, w := range want {
			if c := sb.CellAt(0, i); c == nil || c.Content != w[:1] {
				t.Fatalf("%s: CellAt(0, %d) = %v, want %q", step, i, c, w[:1])
			}
		}
		if sb.Line(len(want)) != nil || sb.Line(-1) != nil {
			t.Fatalf("%s: a line out of range is not nil", step)
		}
	}
	check("after the ring is full", "c", "d", "ee")
	sb.SetMaxLines(2)
	check("smaller", "d", "ee")
	sb.Push(line("f"))
	check("push after smaller", "ee", "f")
	sb.SetMaxLines(4)
	sb.Push(line("g"))
	check("larger", "ee", "f", "g")
	sb.Clear()
	check("clear")
	sb.Push(line("h"))
	check("push after clear", "h")
}

// TestScrollbackRingReusesMemory checks that a full ring allocates nothing for
// a new line that fits in the oldest line's memory.
func TestScrollbackRingReusesMemory(t *testing.T) {
	sb := NewScrollback(4)
	l := line("hello")
	for range 4 {
		sb.Push(l)
	}
	if n := testing.AllocsPerRun(100, func() { sb.Push(l) }); n != 0 {
		t.Errorf("Push on a full ring: %v allocations, want 0", n)
	}
}

func TestASCIICellNoAlloc(t *testing.T) {
	e := NewEmulator(80, 4)
	b := []byte("abcdefghij")
	_, _ = e.Write(b)
	if n := testing.AllocsPerRun(100, func() { _, _ = e.Write([]byte("\rabcdefghij")) }); n != 0 {
		t.Errorf("a line of ASCII: %v allocations, want 0", n)
	}
	if got := lineText(e, 0)[:10]; got != "abcdefghij" {
		t.Errorf("line: got %q", got)
	}
}
