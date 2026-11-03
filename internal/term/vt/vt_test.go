package vt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/term"
)

func feed(t *testing.T, cols, rows int, s string) *Terminal {
	t.Helper()
	v := New(cols, rows, nil)
	t.Cleanup(func() { v.Close() })
	if _, err := v.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	return v
}

func text(r term.Row) string {
	var b strings.Builder
	for _, c := range r {
		if c.WideTail {
			continue
		}
		if c.Text == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.Text)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func TestPlainTextAndColors(t *testing.T) {
	v := feed(t, 20, 3, "\x1b[31mred\x1b[0m \x1b[1;38;5;200mx\x1b[0m \x1b[38;2;1;2;3my\x1b[48;5;7m")
	r := v.Screen(0).Rows[0]
	if text(r) != "red x y" {
		t.Fatalf("text %q", text(r))
	}
	if r[0].Fg != 2 { // named: ANSI 1 is 2
		t.Errorf("red fg %#x", r[0].Fg)
	}
	if r[4].Fg != 1<<24|200 || r[4].Attrs&attrBold == 0 {
		t.Errorf("indexed bold: fg %#x attrs %d", r[4].Fg, r[4].Attrs)
	}
	if r[6].Fg != 2<<24|0x010203 {
		t.Errorf("rgb fg %#x", r[6].Fg)
	}
}

func TestWideChars(t *testing.T) {
	r := feed(t, 10, 2, "a日b").Screen(0).Rows[0]
	if r[1].Text != "日" || r[1].WideTail || !r[2].WideTail || r[3].Text != "b" {
		t.Fatalf("cells %+v", r[:4])
	}
}

func TestAttrsAndUnderlineStyle(t *testing.T) {
	r := feed(t, 20, 2, "\x1b[4:3mc\x1b[0m\x1b[7;9;3;2mr").Screen(0).Rows[0]
	if r[0].Attrs&attrUnderline == 0 || r[0].Attrs>>underlineShift != 3 {
		t.Errorf("curly: %#x", r[0].Attrs)
	}
	want := uint16(attrReversed | attrStrikethrough | attrItalic | attrDim)
	if r[1].Attrs != want {
		t.Errorf("attrs %#x want %#x", r[1].Attrs, want)
	}
}

func TestCursor(t *testing.T) {
	s := feed(t, 20, 5, "\x1b[3;4H\x1b[6 q").Screen(0)
	c := s.Cursor
	if c == nil || c.X != 3 || c.Y != 2 || c.Shape != 6 {
		t.Fatalf("cursor %+v", c)
	}
	s = feed(t, 20, 5, "\x1b[?25l").Screen(0)
	if s.Cursor != nil {
		t.Error("hidden cursor shown")
	}
}

func TestModes(t *testing.T) {
	v := feed(t, 10, 3, "\x1b[?1h\x1b[?2004h\x1b[?1004h\x1b[?1002h\x1b[?1006h\x1b[?1049h")
	m := v.Modes()
	want := Modes{AppCursor: true, BracketedPaste: true, Focus: true, Mouse: 1002, MouseSGR: true, AltScreen: true}
	if m != want {
		t.Fatalf("modes %+v", m)
	}
	p := v.Screen(0).Panes[0]
	if !p.AlternateScreen || !p.MouseReporting {
		t.Errorf("pane %+v", p)
	}
	v.Write([]byte("\x1b[?1l\x1b[?2004l\x1b[?1002l\x1b[?1049l"))
	m = v.Modes()
	if m.AppCursor || m.BracketedPaste || m.Mouse != 0 || m.AltScreen {
		t.Errorf("modes after reset %+v", m)
	}
}

func TestScrollbackOffset(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("line")
		b.WriteByte(byte('0' + i))
		b.WriteString("\r\n")
	}
	v := feed(t, 10, 4, b.String())
	s := v.Screen(0)
	if text(s.Rows[0]) != "line7" {
		t.Fatalf("live top %q", text(s.Rows[0]))
	}
	if s.Panes[0].Scroll.MaxOffsetFromBottom != 7 {
		t.Fatalf("history %d", s.Panes[0].Scroll.MaxOffsetFromBottom)
	}
	s = v.Screen(3)
	if text(s.Rows[0]) != "line4" || s.Cursor != nil {
		t.Errorf("offset 3: top %q cursor %v", text(s.Rows[0]), s.Cursor)
	}
	if got := text(v.Screen(99).Rows[0]); got != "line0" {
		t.Errorf("clamped top %q", got)
	}
	if got := text(v.Screen(-5).Rows[0]); got != "line7" {
		t.Errorf("negative top %q", got)
	}
}

func TestResize(t *testing.T) {
	v := feed(t, 10, 3, "abc")
	v.Resize(20, 5)
	s := v.Screen(0)
	if s.Size != (term.GridSize{Cols: 20, Rows: 5}) || len(s.Rows) != 5 || len(s.Rows[0]) != 20 {
		t.Fatalf("size %+v", s.Size)
	}
	if text(s.Rows[0]) != "abc" {
		t.Errorf("row %q", text(s.Rows[0]))
	}
}

func TestResponsesDoNotBlock(t *testing.T) {
	got := make(chan string, 1)
	v := New(10, 10, func(b []byte) {
		select {
		case got <- string(b):
		default:
		}
	})
	defer v.Close()
	v.Write([]byte("\x1b[5;7H\x1b[6n"))
	if r := <-got; r != "\x1b[5;7R" {
		t.Errorf("cursor report %q", r)
	}
}

func TestScreenIsACopy(t *testing.T) {
	v := feed(t, 10, 2, "a")
	s := v.Screen(0)
	v.Write([]byte("\rz"))
	if s.Rows[0][0].Text != "a" {
		t.Error("screen changed after Write")
	}
}

// golden checks a recorded stream: the final screen text.
func TestRecordedStreams(t *testing.T) {
	cases := []struct {
		file       string
		cols, rows int
		trim       string // cut the stream before this marker (to stay in the alt screen)
		want       []string
		alt        bool
	}{
		{"ls.bin", 60, 10, "", []string{
			"total 0",
			"-rwxr-xr-x@ 1 aerolab  staff   0 Nov  3 17:54 a.go",
			"-rw-r--r--@ 1 aerolab  staff   0 Nov  3 17:54 b.txt",
			"lrwxr-xr-x@ 1 aerolab  staff   4 Nov  3 17:54 link -> a.go",
			"drwxr-xr-x@ 2 aerolab  staff  64 Nov  3 17:54 sub"}, false},
		{"vim.bin", 60, 12, "\x1b[?1049l", []string{}, true},
		{"tui.bin", 60, 12, "", []string{
			"╭──────────────────────────────────────╮",
			"│ ✻ Welcome to the TUI 日本語       │",
			"╰──────────────────────────────────────╯",
			"+ added line", "- removed line", "curly reverse strike", "", "", "", "> type here"}, false},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			d, err := os.ReadFile(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatal(err)
			}
			if c.trim != "" {
				d = d[:strings.Index(string(d), c.trim)]
			}
			v := New(c.cols, c.rows, nil)
			defer v.Close()
			v.Write(d)
			s := v.Screen(0)
			for i, w := range c.want {
				if got := text(s.Rows[i]); got != w {
					t.Errorf("row %d: %q want %q", i, got, w)
				}
			}
			if s.Panes[0].AlternateScreen != c.alt {
				t.Errorf("alt screen %v", s.Panes[0].AlternateScreen)
			}
		})
	}
}

func TestRecordedColors(t *testing.T) {
	d, _ := os.ReadFile("testdata/ls.bin")
	v := New(60, 10, nil)
	defer v.Close()
	v.Write(d)
	r := v.Screen(0).Rows[1]
	i := strings.Index(text(r), "a.go")
	if r[i].Fg != 2 { // ls colors an executable red
		t.Errorf("a.go fg %#x", r[i].Fg)
	}
	if r[0].Fg != 0 {
		t.Errorf("plain text fg %#x", r[0].Fg)
	}
}

func TestTUIDiffColors(t *testing.T) {
	d, _ := os.ReadFile("testdata/tui.bin")
	v := New(60, 12, nil)
	defer v.Close()
	v.Write(d)
	s := v.Screen(0)
	if c := s.Rows[3][0]; c.Fg != 1<<24|114 || c.Bg != 1<<24|236 {
		t.Errorf("added %#x %#x", c.Fg, c.Bg)
	}
	if c := s.Rows[3][30]; c.Bg != 1<<24|236 {
		t.Errorf("EL fill bg %#x", c.Bg)
	}
	if s.Cursor == nil || s.Cursor.X != 12 || s.Cursor.Y != 9 || s.Cursor.Shape != 6 {
		t.Errorf("cursor %+v", s.Cursor)
	}
}

func TestWriteAfterCloseDoesNotBlock(t *testing.T) {
	v := New(10, 3, nil)
	v.Close()
	done := make(chan struct{})
	go func() { v.Write([]byte("\x1b[6nabc")); close(done) }()
	<-done
}

// BenchmarkFlood200x60 writes 3000 colored lines to a 200×60 screen
// (D39 compared two libraries with this stream).
func BenchmarkFlood200x60(b *testing.B) {
	var buf strings.Builder
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&buf, "\x1b[3%dm line %d \x1b[1;48;5;%dmtext 日本 more text to fill the row of the screen..........\x1b[0m\r\n", i%8, i, i%256)
	}
	d := []byte(buf.String())
	v := New(200, 60, nil)
	defer v.Close()
	b.SetBytes(int64(len(d)))
	for i := 0; i < b.N; i++ {
		v.Write(d)
	}
}

func TestOnClipboard(t *testing.T) {
	tm := New(20, 5, nil)
	defer tm.Close()
	var got []string
	tm.OnClipboard(func(s string) { got = append(got, s) })
	for _, in := range []string{
		"\x1b]52;c;aGVsbG8=\x07", // hello
		"\x1b]52;c;aGk\x1b\\",    // hi, no padding, ST end
		"\x1b]52;c;?\x07",        // query: dropped
		"\x1b]52;c;!!!\x07",      // not base64: dropped
		"\x1b]52;c;/w==\x07",     // invalid UTF-8: dropped
		"\x1b]52;c\x07",          // no payload field: dropped
	} {
		if _, err := tm.Write([]byte(in)); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != "hello" || got[1] != "hi" {
		t.Errorf("clipboard = %q, want [hello hi]", got)
	}
}
