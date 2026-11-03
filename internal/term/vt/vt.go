// Package vt is contr's terminal emulator. It reads the raw bytes of a
// terminal and gives the screen as term types. It wraps a pure-Go VT library
// (D39); no other package imports that library.
package vt

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"io"
	"sync"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	cvt "github.com/charmbracelet/x/vt"

	"github.com/contursofran/contr/internal/term"
)

// MaxScrollback is the most lines of scrollback that the emulator keeps.
const MaxScrollback = 10000

// Modes are the terminal modes that decide how input is encoded.
type Modes struct {
	// AppCursor is DECCKM: arrow keys send SS3 instead of CSI.
	AppCursor bool
	// AppKeypad is DECKPAM.
	AppKeypad bool
	// BracketedPaste is mode 2004.
	BracketedPaste bool
	// Focus is mode 1004: send focus in and out reports.
	Focus bool
	// Mouse is the mouse mode: 0 none, 9 X10, 1000, 1002 or 1003.
	Mouse int
	// MouseSGR is mode 1006.
	MouseSGR bool
	// AltScreen is true while the alternate screen shows.
	AltScreen bool
}

// Terminal is an emulator. It is safe for use from several goroutines.
type Terminal struct {
	mu    sync.Mutex
	e     *cvt.Emulator
	modes Modes
	rev   uint64
}

// New returns a terminal of cols × rows cells. respond gets the answers that
// the program asked for (such as a cursor position report); the caller sends
// them back to the program. It may be nil. Call Close when done.
func New(cols, rows int, respond func([]byte)) *Terminal {
	t := &Terminal{e: cvt.NewEmulator(cols, rows)}
	// The emulator's answer pipe blocks the parser when nobody reads it.
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := t.e.Read(buf)
			if n > 0 && respond != nil {
				respond(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	t.e.SetScrollbackSize(MaxScrollback)
	t.e.SetCallbacks(cvt.Callbacks{
		EnableMode:  func(m ansi.Mode) { t.setMode(m, true) },
		DisableMode: func(m ansi.Mode) { t.setMode(m, false) },
	})
	return t
}

func (t *Terminal) setMode(m ansi.Mode, on bool) {
	switch m {
	case ansi.ModeCursorKeys:
		t.modes.AppCursor = on
	case ansi.ModeNumericKeypad:
		// ?66 set means application keypad.
		t.modes.AppKeypad = on
	case ansi.ModeBracketedPaste:
		t.modes.BracketedPaste = on
	case ansi.ModeFocusEvent:
		t.modes.Focus = on
	case ansi.ModeMouseExtSgr:
		t.modes.MouseSGR = on
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		n := int(m.Mode())
		switch {
		case on:
			t.modes.Mouse = n
		case t.modes.Mouse == n:
			t.modes.Mouse = 0
		}
	}
}

// Write feeds terminal output to the emulator.
func (t *Terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, err := t.e.Write(p)
	t.rev++
	t.modes.AltScreen = t.e.IsAltScreen()
	return n, err
}

// Close ends the goroutine that reads the emulator's answers.
//
// It closes the answer pipe itself. The library's Close writes a flag that its
// Read reads without a lock, which the race detector reports.
func (t *Terminal) Close() error {
	if pw, ok := t.e.InputPipe().(*io.PipeWriter); ok {
		return pw.CloseWithError(io.EOF)
	}
	return t.e.Close()
}

// Modes returns the current modes.
func (t *Terminal) Modes() Modes {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.modes
}

// Resize changes the size of the grid.
func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.e.Resize(cols, rows)
	t.rev++
}

// Screen returns the screen with the view scrolled historyOffset rows up into
// the scrollback. The offset is clamped to the history; it is 0 on the
// alternate screen, which has none. The result shares nothing with the
// emulator.
func (t *Terminal) Screen(historyOffset int) term.Screen {
	t.mu.Lock()
	defer t.mu.Unlock()
	w, h := t.e.Width(), t.e.Height()
	hist := t.e.ScrollbackLen()
	if t.modes.AltScreen || historyOffset < 0 {
		historyOffset = 0
	}
	if historyOffset > hist {
		historyOffset = hist
	}
	rows := make([]term.Row, h)
	for y := 0; y < h; y++ {
		rows[y] = make(term.Row, w)
		src := y - historyOffset // row in the live screen; negative is history
		for x := 0; x < w; x++ {
			var c *uv.Cell
			if src >= 0 {
				c = t.e.CellAt(x, src)
			} else {
				c = t.e.ScrollbackCellAt(x, hist+src)
			}
			rows[y][x] = convert(c)
		}
	}
	scr := term.Screen{
		Revision: t.rev,
		Size:     term.GridSize{Cols: uint16(w), Rows: uint16(h)},
		Rows:     rows,
	}
	pane := term.Pane{
		PaneID:          "0",
		Rect:            term.Rect{Width: uint16(w), Height: uint16(h)},
		InnerRect:       term.Rect{Width: uint16(w), Height: uint16(h)},
		Focused:         true,
		AlternateScreen: t.modes.AltScreen,
		MouseReporting:  t.modes.Mouse != 0,
		Scroll: &term.ScrollMetrics{
			OffsetFromBottom:    uint64(historyOffset),
			MaxOffsetFromBottom: uint64(hist),
			ViewportRows:        uint64(h),
		},
	}
	scr.Panes = []term.Pane{pane}
	if pos := t.e.CursorPosition(); historyOffset == 0 && !t.e.CursorHidden() {
		style, blink := t.e.CursorStyle()
		scr.Cursor = &term.Cursor{X: uint16(pos.X), Y: uint16(pos.Y), Visible: true, Shape: shape(style, blink)}
	}
	return scr
}

// shape maps a cursor style to the DECSCUSR parameter.
func shape(s cvt.CursorStyle, blink bool) uint8 {
	var n uint8
	switch s {
	case cvt.CursorUnderline:
		n = 3
	case cvt.CursorBar:
		n = 5
	default:
		n = 1
	}
	if !blink {
		n++
	}
	return n
}

// herdr's modifier bits (see internal/app/terminal/flags.go).
const (
	attrBold          = 1
	attrDim           = 2
	attrItalic        = 4
	attrUnderline     = 8
	attrReversed      = 64
	attrHidden        = 128
	attrStrikethrough = 256
	underlineShift    = 12
)

func convert(c *uv.Cell) term.Cell {
	if c == nil || c.Width == 0 && c.Content == "" {
		// A nil cell or the tail of a wide glyph.
		return term.Cell{WideTail: c != nil}
	}
	out := term.Cell{Text: c.Content, Fg: packColor(c.Style.Fg), Bg: packColor(c.Style.Bg)}
	a := c.Style.Attrs
	for _, m := range [...]struct {
		from uint8
		to   uint16
	}{
		{uv.AttrBold, attrBold}, {uv.AttrFaint, attrDim}, {uv.AttrItalic, attrItalic},
		{uv.AttrReverse, attrReversed}, {uv.AttrConceal, attrHidden}, {uv.AttrStrikethrough, attrStrikethrough},
	} {
		if a&m.from != 0 {
			out.Attrs |= m.to
		}
	}
	if u := c.Style.Underline; u != uv.UnderlineNone {
		out.Attrs |= attrUnderline | uint16(u)<<underlineShift
	}
	return out
}

// packColor returns herdr's packed color: kind in the top byte (0 named, 1
// indexed, 2 RGB). nil is the default color (named 0).
func packColor(c color.Color) uint32 {
	switch v := c.(type) {
	case nil:
		return 0
	case ansi.BasicColor:
		return uint32(v) + 1
	case ansi.IndexedColor:
		return 1<<24 | uint32(v)
	}
	r, g, b, _ := c.RGBA()
	return 2<<24 | (r>>8)<<16 | (g>>8)<<8 | b>>8
}

// MaxClipboardBytes is the largest text from OSC 52 that OnClipboard passes on.
const MaxClipboardBytes = 2 * 1024 * 1024

// OnClipboard sets fn to receive the text that a program writes with OSC 52
// (`ESC ] 52 ; <target> ; <base64> ST`). A payload that is not base64, not
// UTF-8 or past MaxClipboardBytes is dropped. A query (`?`) is dropped too:
// contr never reads the clipboard for a program. fn runs during Write, so it
// must not call the terminal. Call OnClipboard before the first Write.
func (t *Terminal) OnClipboard(fn func(text string)) {
	t.e.RegisterOscHandler(52, func(data []byte) bool {
		parts := bytes.SplitN(data, []byte{';'}, 3)
		if len(parts) != 3 || fn == nil {
			return true
		}
		payload := parts[2]
		if len(payload) > (MaxClipboardBytes+2)/3*4+4 {
			return true
		}
		b, err := base64.StdEncoding.DecodeString(string(payload))
		if err != nil {
			b, err = base64.RawStdEncoding.DecodeString(string(payload))
		}
		if err != nil || len(b) > MaxClipboardBytes || !utf8.Valid(b) {
			return true
		}
		fn(string(b))
		return true
	})
}
