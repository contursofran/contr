package vt

import (
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/term"
)

func key(k term.KeyKind, mods uint8) term.KeyEvent {
	return term.KeyEvent{Code: term.KeyCode{Kind: k}, Mods: mods}
}

func TestEncodeArrowsCursorMode(t *testing.T) {
	cases := []struct {
		k      term.KeyKind
		mods   uint8
		app    bool
		expect string
	}{
		{term.KeyUp, 0, false, "\x1b[A"},
		{term.KeyUp, 0, true, "\x1bOA"},
		{term.KeyLeft, 0, false, "\x1b[D"},
		{term.KeyLeft, 0, true, "\x1bOD"},
		{term.KeyRight, term.ModControl, true, "\x1b[1;5C"},
		{term.KeyDown, term.ModShift | term.ModAlt, false, "\x1b[1;4B"},
		{term.KeyHome, 0, false, "\x1b[H"},
		{term.KeyEnd, 0, true, "\x1bOF"},
	}
	for _, c := range cases {
		got := string(Encode(key(c.k, c.mods), Modes{AppCursor: c.app}))
		if got != c.expect {
			t.Errorf("kind %d mods %d app %v: %q, want %q", c.k, c.mods, c.app, got, c.expect)
		}
	}
}

func TestEncodeCursorModeFromProgram(t *testing.T) {
	v := feed(t, 10, 3, "\x1b[?1h")
	if got := string(Encode(key(term.KeyUp, 0), v.Modes())); got != "\x1bOA" {
		t.Fatalf("got %q", got)
	}
}

func TestEncodeKeys(t *testing.T) {
	cases := []struct {
		ev     term.InputEvent
		expect string
	}{
		{key(term.KeyEnter, 0), "\r"},
		{key(term.KeyTab, 0), "\t"},
		{key(term.KeyBackTab, 0), "\x1b[Z"},
		{key(term.KeyEsc, 0), "\x1b"},
		{key(term.KeyBackspace, 0), "\x7f"},
		{key(term.KeyBackspace, term.ModAlt), "\x1b\x7f"},
		{key(term.KeyDelete, 0), "\x1b[3~"},
		{key(term.KeyPageUp, term.ModControl), "\x1b[5;5~"},
		{key(term.KeyInsert, 0), "\x1b[2~"},
		{term.KeyEvent{Code: term.CharKey('c'), Mods: term.ModControl}, "\x03"},
		{term.KeyEvent{Code: term.CharKey('Z'), Mods: term.ModControl}, "\x1a"},
		{term.KeyEvent{Code: term.CharKey(' '), Mods: term.ModControl}, "\x00"},
		{term.KeyEvent{Code: term.CharKey('['), Mods: term.ModControl}, "\x1b"},
		{term.KeyEvent{Code: term.CharKey('_'), Mods: term.ModControl}, "\x1f"},
		{term.KeyEvent{Code: term.CharKey('b'), Mods: term.ModAlt}, "\x1bb"},
		{term.KeyEvent{Code: term.CharKey('é'), Mods: term.ModAlt}, "\x1bé"},
		{term.KeyEvent{Code: term.CharKey('x')}, "x"},
		{term.KeyEvent{Code: term.FKey(1)}, "\x1bOP"},
		{term.KeyEvent{Code: term.FKey(4)}, "\x1bOS"},
		{term.KeyEvent{Code: term.FKey(5)}, "\x1b[15~"},
		{term.KeyEvent{Code: term.FKey(12)}, "\x1b[24~"},
		{term.KeyEvent{Code: term.FKey(2), Mods: term.ModShift}, "\x1b[1;2Q"},
		{term.KeyEvent{Code: term.FKey(6), Mods: term.ModControl}, "\x1b[17;5~"},
		{term.TextEvent{Text: "héllo"}, "héllo"},
	}
	for _, c := range cases {
		if got := string(Encode(c.ev, Modes{})); got != c.expect {
			t.Errorf("%+v: %q, want %q", c.ev, got, c.expect)
		}
	}
	if Encode(term.KeyEvent{Code: term.FKey(13)}, Modes{}) != nil {
		t.Error("F13 must encode to nothing (D10)")
	}
}

func TestPasteBracketed(t *testing.T) {
	v := feed(t, 10, 3, "\x1b[?2004h")
	got := string(Encode(term.PasteEvent{Text: "a\nb\x1b[201~c"}, v.Modes()))
	if got != "\x1b[200~a\nbc\x1b[201~" {
		t.Fatalf("got %q", got)
	}
}

func TestPasteRaw(t *testing.T) {
	got := string(Encode(term.PasteEvent{Text: "a\nb\r\nc"}, Modes{}))
	if got != "a\rb\rc" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "\x1b[200~") {
		t.Fatal("raw paste has marks")
	}
}

func TestWheelMouseSGR(t *testing.T) {
	ev := term.WheelEvent{Dir: term.WheelUp, Column: 4, Row: 9, Lines: 3}
	sgr := Modes{Mouse: 1000, MouseSGR: true}
	if got := string(Encode(ev, sgr)); got != "\x1b[<64;5;10M" {
		t.Fatalf("up: %q", got)
	}
	ev.Dir = term.WheelDown
	ev.Mods = term.ModShift | term.ModControl
	if got := string(Encode(ev, sgr)); got != "\x1b[<85;5;10M" {
		t.Fatalf("down with mods: %q", got)
	}
	ev.Dir, ev.Mods = term.WheelRight, 0
	if got := string(Encode(ev, sgr)); got != "\x1b[<67;5;10M" {
		t.Fatalf("right: %q", got)
	}
	// Legacy form, and out of range.
	ev.Dir = term.WheelUp
	if got := string(Encode(ev, Modes{Mouse: 1002})); got != "\x1b[M`%*" {
		t.Fatalf("legacy: %q", got)
	}
	ev.Column = 300
	if Encode(ev, Modes{Mouse: 1002}) != nil {
		t.Fatal("legacy report past column 222")
	}
	if Encode(ev, Modes{Mouse: 9, MouseSGR: true}) != nil {
		t.Fatal("X10 has no wheel")
	}
}

func TestWheelAltScreenArrows(t *testing.T) {
	up := term.WheelEvent{Dir: term.WheelUp, Lines: 3}
	b, scroll := Wheel(up, Modes{AltScreen: true})
	if string(b) != "\x1b[A" || scroll != 0 {
		t.Fatalf("%q %d", b, scroll)
	}
	b, _ = Wheel(term.WheelEvent{Dir: term.WheelDown}, Modes{AltScreen: true, AppCursor: true})
	if string(b) != "\x1bOB" {
		t.Fatalf("%q", b)
	}
	if b, s := Wheel(term.WheelEvent{Dir: term.WheelLeft}, Modes{AltScreen: true}); b != nil || s != 0 {
		t.Fatal("horizontal on the alt screen must do nothing")
	}
}

func TestWheelLocalScroll(t *testing.T) {
	b, s := Wheel(term.WheelEvent{Dir: term.WheelUp, Lines: 3}, Modes{})
	if b != nil || s != 3 {
		t.Fatalf("%q %d", b, s)
	}
	if _, s = Wheel(term.WheelEvent{Dir: term.WheelDown, Lines: 3}, Modes{}); s != -3 {
		t.Fatalf("down %d", s)
	}
	if b, s = Wheel(term.WheelEvent{Dir: term.WheelRight, Lines: 3}, Modes{}); b != nil || s != 0 {
		t.Fatal("horizontal must do nothing")
	}
	if Encode(term.WheelEvent{Dir: term.WheelUp, Lines: 3}, Modes{}) != nil {
		t.Fatal("local scroll has no bytes")
	}
}

func TestWheelScrollbackClamp(t *testing.T) {
	v := feed(t, 10, 3, strings.Repeat("line\r\n", 10))
	hist := int(v.Screen(0).Panes[0].Scroll.MaxOffsetFromBottom)
	if hist == 0 {
		t.Fatal("no history")
	}
	off := 0
	for i := 0; i < 20; i++ {
		_, s := Wheel(term.WheelEvent{Dir: term.WheelUp, Lines: 3}, v.Modes())
		off = ClampOffset(off, s, hist)
	}
	if off != hist {
		t.Fatalf("offset %d, want %d", off, hist)
	}
	if got := int(v.Screen(off + 50).Panes[0].Scroll.OffsetFromBottom); got != hist {
		t.Fatalf("screen offset %d", got)
	}
	for i := 0; i < 20; i++ {
		_, s := Wheel(term.WheelEvent{Dir: term.WheelDown, Lines: 3}, v.Modes())
		off = ClampOffset(off, s, hist)
	}
	if off != 0 {
		t.Fatalf("offset %d below 0", off)
	}
}
