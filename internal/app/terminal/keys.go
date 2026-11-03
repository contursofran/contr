// Package terminal holds the terminal pane: keys, grid and drawing.
package terminal

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/term"
)

var namedKeys = map[ui.Key]term.KeyKind{
	ui.KeyEnter:     term.KeyEnter,
	ui.KeyBackspace: term.KeyBackspace,
	ui.KeyDelete:    term.KeyDelete,
	ui.KeyEscape:    term.KeyEsc,
	ui.KeyTab:       term.KeyTab,
	ui.KeyLeft:      term.KeyLeft,
	ui.KeyRight:     term.KeyRight,
	ui.KeyUp:        term.KeyUp,
	ui.KeyDown:      term.KeyDown,
	ui.KeyHome:      term.KeyHome,
	ui.KeyEnd:       term.KeyEnd,
	ui.KeyPageUp:    term.KeyPageUp,
	ui.KeyPageDown:  term.KeyPageDown,
	ui.KeyInsert:    term.KeyInsert,
}

var functionKeys = map[ui.Key]uint8{
	ui.KeyF1: 1, ui.KeyF2: 2, ui.KeyF3: 3, ui.KeyF4: 4, ui.KeyF5: 5, ui.KeyF6: 6,
	ui.KeyF7: 7, ui.KeyF8: 8, ui.KeyF9: 9, ui.KeyF10: 10, ui.KeyF11: 11, ui.KeyF12: 12,
}

// controlChars are the keys that make a control code with Ctrl, other than
// the letters. The value is the character herdr names the key by.
var controlChars = map[ui.Key]rune{
	ui.KeySpace:        ' ',
	ui.KeyBracketLeft:  '[',
	ui.KeyBackslash:    '\\',
	ui.KeyBracketRight: ']',
}

// shiftedControlChars are the keys that Ctrl and Shift type as @, ^ and _
// on a US layout. The shift is in the character, so the event has Ctrl only.
var shiftedControlChars = map[ui.Key]rune{
	ui.Key2:     '@',
	ui.Key6:     '^',
	ui.KeyMinus: '_',
}

// KeyEvent returns the herdr event for a key press, or false when the press
// is text or belongs to the app. Printable text is not a key: the IME delivers
// it, so Option types characters (D11) and dead keys work. Cmd chords are the
// app's. Named keys, function keys and Ctrl chords become key events.
// Repeat is always false: the view does not send repeat events yet.
func KeyEvent(key ui.Key, m ui.Modifiers) (term.KeyEvent, bool) {
	if m&ui.Super != 0 {
		return term.KeyEvent{}, false
	}
	var flags uint8
	if m&ui.Shift != 0 {
		flags |= term.ModShift
	}
	if m&ui.Ctrl != 0 {
		flags |= term.ModControl
	}
	if m&ui.Alt != 0 {
		flags |= term.ModAlt
	}
	if kind, ok := namedKeys[key]; ok {
		if key == ui.KeyTab && m&ui.Shift != 0 {
			kind = term.KeyBackTab
		}
		return term.KeyEvent{Code: term.KeyCode{Kind: kind}, Mods: flags}, true
	}
	if n, ok := functionKeys[key]; ok {
		return term.KeyEvent{Code: term.FKey(n), Mods: flags}, true
	}
	// Option is not Alt for characters, so Alt with a character is text.
	// Only Ctrl makes a key out of a character.
	if m&ui.Alt != 0 || m&ui.Ctrl == 0 {
		return term.KeyEvent{}, false
	}
	if m&ui.Shift != 0 {
		if c, ok := shiftedControlChars[key]; ok {
			return term.KeyEvent{Code: term.CharKey(c), Mods: term.ModControl}, true
		}
	}
	if key >= ui.KeyA && key <= ui.KeyZ {
		return term.KeyEvent{Code: term.CharKey('a' + rune(key-ui.KeyA)), Mods: flags}, true
	}
	if c, ok := controlChars[key]; ok {
		return term.KeyEvent{Code: term.CharKey(c), Mods: flags}, true
	}
	return term.KeyEvent{}, false
}
