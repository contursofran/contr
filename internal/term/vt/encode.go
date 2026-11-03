package vt

import (
	"bytes"
	"strconv"
	"unicode/utf8"

	"github.com/contursofran/contr/internal/term"
)

// Encode returns the bytes that a program reads for an input event, given the
// terminal modes. A wheel event gives its mouse report or arrow key; a wheel
// event that scrolls the history gives nil (use Wheel). It returns nil for an
// event it cannot encode.
func Encode(ev term.InputEvent, m Modes) []byte {
	switch e := ev.(type) {
	case term.KeyEvent:
		return encodeKey(e, m)
	case term.TextEvent:
		return []byte(e.Text)
	case term.PasteEvent:
		return encodePaste(e.Text, m)
	case term.WheelEvent:
		b, _ := Wheel(e, m)
		return b
	}
	return nil
}

// xtermMod returns the xterm modifier parameter of the mods, or 0 for none.
func xtermMod(mods uint8) int {
	n := 0
	if mods&term.ModShift != 0 {
		n |= 1
	}
	if mods&term.ModAlt != 0 {
		n |= 2
	}
	if mods&term.ModControl != 0 {
		n |= 4
	}
	if n == 0 {
		return 0
	}
	return n + 1
}

// csi builds a final-byte sequence: ESC [ final, or ESC [ 1 ; mod final with
// modifiers; an application cursor mode uses SS3 when there are none.
func csi(final byte, mods uint8, appCursor bool) []byte {
	if mod := xtermMod(mods); mod != 0 {
		return []byte("\x1b[1;" + strconv.Itoa(mod) + string(final))
	}
	if appCursor {
		return []byte{0x1b, 'O', final}
	}
	return []byte{0x1b, '[', final}
}

// tilde builds ESC [ n ~ or ESC [ n ; mod ~.
func tilde(n int, mods uint8) []byte {
	s := "\x1b[" + strconv.Itoa(n)
	if mod := xtermMod(mods); mod != 0 {
		s += ";" + strconv.Itoa(mod)
	}
	return []byte(s + "~")
}

var fTilde = [...]int{5: 15, 6: 17, 7: 18, 8: 19, 9: 20, 10: 21, 11: 23, 12: 24}

func encodeKey(e term.KeyEvent, m Modes) []byte {
	alt := e.Mods&term.ModAlt != 0
	prefix := func(b []byte) []byte {
		if alt {
			return append([]byte{0x1b}, b...)
		}
		return b
	}
	switch e.Code.Kind {
	case term.KeyChar:
		return encodeChar(e.Code.Char, e.Mods)
	case term.KeyEnter:
		return prefix([]byte{'\r'})
	case term.KeyTab:
		return prefix([]byte{'\t'})
	case term.KeyBackTab:
		return []byte("\x1b[Z")
	case term.KeyEsc:
		return prefix([]byte{0x1b})
	case term.KeyBackspace:
		if e.Mods&term.ModControl != 0 {
			return prefix([]byte{0x08})
		}
		return prefix([]byte{0x7f})
	case term.KeyUp:
		return csi('A', e.Mods, m.AppCursor)
	case term.KeyDown:
		return csi('B', e.Mods, m.AppCursor)
	case term.KeyRight:
		return csi('C', e.Mods, m.AppCursor)
	case term.KeyLeft:
		return csi('D', e.Mods, m.AppCursor)
	case term.KeyHome:
		return csi('H', e.Mods, m.AppCursor)
	case term.KeyEnd:
		return csi('F', e.Mods, m.AppCursor)
	case term.KeyInsert:
		return tilde(2, e.Mods)
	case term.KeyDelete:
		return tilde(3, e.Mods)
	case term.KeyPageUp:
		return tilde(5, e.Mods)
	case term.KeyPageDown:
		return tilde(6, e.Mods)
	case term.KeyF:
		n := int(e.Code.N)
		switch {
		case n >= 1 && n <= 4:
			// F1 to F4 are SS3 P to S, or CSI 1 ; mod P to S with modifiers.
			final := byte('P' + n - 1)
			if mod := xtermMod(e.Mods); mod != 0 {
				return []byte("\x1b[1;" + strconv.Itoa(mod) + string(final))
			}
			return []byte{0x1b, 'O', final}
		case n >= 5 && n <= 12:
			return tilde(fTilde[n], e.Mods)
		}
	}
	return nil
}

// encodeChar encodes a character key. Ctrl makes a control code; Alt adds an
// ESC prefix. Shift is already in the character.
func encodeChar(c rune, mods uint8) []byte {
	var b []byte
	if mods&term.ModControl != 0 {
		switch {
		case c >= 'a' && c <= 'z':
			b = []byte{byte(c-'a') + 1}
		case c >= 'A' && c <= 'Z':
			b = []byte{byte(c-'A') + 1}
		case c == ' ' || c == '@':
			b = []byte{0}
		case c >= '[' && c <= '_':
			b = []byte{byte(c-'[') + 27}
		case c == '?':
			b = []byte{0x7f}
		}
	}
	if b == nil {
		if c < 0 || !utf8.ValidRune(c) {
			return nil
		}
		b = utf8.AppendRune(nil, c)
	}
	if mods&term.ModAlt != 0 {
		b = append([]byte{0x1b}, b...)
	}
	return b
}

var (
	pasteStart = []byte("\x1b[200~")
	pasteEnd   = []byte("\x1b[201~")
)

// encodePaste wraps the text in the bracketed paste marks when the mode is
// set, and removes any end mark inside it so the text cannot end the paste
// early. It repeats the removal until none is left, because removing one
// mark can join the bytes around it into a new one. Without the mode, line breaks become CR, as a terminal sends them.
func encodePaste(s string, m Modes) []byte {
	b := []byte(s)
	if m.BracketedPaste {
		for bytes.Contains(b, pasteEnd) {
			b = bytes.ReplaceAll(b, pasteEnd, nil)
		}
		out := make([]byte, 0, len(b)+len(pasteStart)+len(pasteEnd))
		out = append(out, pasteStart...)
		out = append(out, b...)
		return append(out, pasteEnd...)
	}
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\r"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r"))
}

// Wheel handles one wheel event. With a mouse mode on, it returns the mouse
// report. On the alternate screen without a mouse mode, it returns one arrow
// key for a vertical step. Otherwise it returns the lines to scroll the local
// history: positive is up, into the history. Horizontal steps without a mouse
// mode do nothing.
func Wheel(e term.WheelEvent, m Modes) (b []byte, scroll int) {
	if m.Mouse != 0 {
		return mouseWheel(e, m), 0
	}
	vertical := e.Dir == term.WheelUp || e.Dir == term.WheelDown
	if !vertical {
		return nil, 0
	}
	if m.AltScreen {
		if e.Dir == term.WheelUp {
			return csi('A', 0, m.AppCursor), 0
		}
		return csi('B', 0, m.AppCursor), 0
	}
	if e.Dir == term.WheelUp {
		return nil, int(e.Lines)
	}
	return nil, -int(e.Lines)
}

// mouseWheel returns the report for a wheel step: SGR when mode 1006 is on,
// else the legacy form, which cannot hold cells past 222. X10 mode has no wheel.
func mouseWheel(e term.WheelEvent, m Modes) []byte {
	if m.Mouse == 9 {
		return nil
	}
	btn := 64 + int(e.Dir)
	if e.Mods&term.ModShift != 0 {
		btn |= 4
	}
	if e.Mods&term.ModAlt != 0 {
		btn |= 8
	}
	if e.Mods&term.ModControl != 0 {
		btn |= 16
	}
	x, y := int(e.Column)+1, int(e.Row)+1
	if m.MouseSGR {
		return []byte("\x1b[<" + strconv.Itoa(btn) + ";" + strconv.Itoa(x) + ";" + strconv.Itoa(y) + "M")
	}
	if x > 223 || y > 223 {
		return nil
	}
	return []byte{0x1b, '[', 'M', byte(32 + btn), byte(32 + x), byte(32 + y)}
}

// ClampOffset returns the history offset after moving by delta lines (up is
// positive), kept between 0 and maxOffset.
func ClampOffset(offset, delta, maxOffset int) int {
	return max(0, min(offset+delta, maxOffset))
}
