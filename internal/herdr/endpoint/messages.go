// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
)

// Names and limits of endpoint generation 1.
const (
	EndpointGeneration = 1
	HelloKind          = "endpoint.hello.v1"
	WelcomeKind        = "endpoint.welcome.v1"
	SnapshotKind       = "shell.snapshot.v1"
	SnapshotCodec      = "shell.snapshot.v1"
	SurfaceCodec       = "shell.surface.v1"
	InputCodec         = "shell.input.semantic.v1"
	BlobCodec          = "shell.blob.v1"
)

// MaxFrame is the largest frame payload, in bytes: 32 MiB (herdr allows it
// with graphics).
const MaxFrame = 32 * 1024 * 1024

// Limits herdr puts on a resize: columns and rows each in 1..=MaxGridSide,
// their product at most MaxGridCells, and a cell side at most MaxCellPx
// device pixels.
const (
	MaxGridSide  = 4096
	MaxGridCells = 1_000_000
	MaxCellPx    = 4096
)

// TestedHerdrVersions lists the herdr versions whose wire layout was checked
// against source (0.9.3 live, 0.9.4 and 0.9.5 by source diff).
var TestedHerdrVersions = []string{"0.9.3", "0.9.4", "0.9.5"}

// Tags of the client messages contr sends.
const (
	CDetach    = 4
	CResize    = 12
	CPaneInput = 13
	CFocus     = 18
	CControl   = 20
)

// Tags of the server messages contr reads. Every other tag is OtherMsg.
const (
	SWelcomeLegacy = 0
	SShutdown      = 3
	SClipboard     = 5
	SShellError    = 15
	SControl       = 20
	SSurface       = 13
	SPatch         = 19
)

// GridSize is a size in terminal cells.
type GridSize struct {
	Cols uint16
	Rows uint16
}

// CellPx is the size of one cell in pixels.
type CellPx struct {
	Width  uint32
	Height uint32
}

// KeyKind is the variant of a key code. The values are herdr's tags.
type KeyKind uint8

// Key kinds, in herdr's tag order.
const (
	KeyBackspace KeyKind = iota
	KeyEnter
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyTab
	KeyBackTab
	KeyDelete
	KeyInsert
	KeyEsc
	KeyChar
	KeyF
)

// KeyCode is a key. Char is set for KeyChar and N (the F number) for KeyF.
type KeyCode struct {
	Kind KeyKind
	Char rune
	N    uint8
}

// CharKey returns the key code of a character.
func CharKey(c rune) KeyCode { return KeyCode{Kind: KeyChar, Char: c} }

// FKey returns the key code of function key n.
func FKey(n uint8) KeyCode { return KeyCode{Kind: KeyF, N: n} }

// Key modifier bits, as herdr reads them.
const (
	ModShift   uint8 = 1
	ModControl uint8 = 2
	ModAlt     uint8 = 4
)

// WheelDir is the direction of one wheel step. Left and Right are horizontal.
type WheelDir uint8

// Wheel directions.
const (
	WheelUp WheelDir = iota
	WheelDown
	WheelLeft
	WheelRight
)

// InputEvent is one event for a pane: KeyEvent, TextEvent, PasteEvent or
// WheelEvent.
type InputEvent interface{ put(e *Enc) }

// KeyEvent is a key press, or a repeat of one.
type KeyEvent struct {
	Code   KeyCode
	Mods   uint8
	Repeat bool
}

// TextEvent is committed text.
type TextEvent struct{ Text string }

// PasteEvent is pasted text.
type PasteEvent struct{ Text string }

// WheelEvent is a wheel step at a cell of the pane, relative to its inner
// rect. herdr scrolls its own history by Lines, or sends one wheel report or
// arrow key to the program.
type WheelEvent struct {
	Dir    WheelDir
	Column uint16
	Row    uint16
	Mods   uint8
	Lines  uint16
}

// ClientMsg is what contr sends to herdr: HelloMsg, ResizeMsg, InputMsg,
// FocusMsg or DetachMsg.
type ClientMsg interface {
	// Encode returns the payload only. The caller adds the length prefix
	// with WriteFrame.
	Encode() []byte
}

// HelloMsg opens the handshake.
type HelloMsg struct {
	Size GridSize
	Cell CellPx
}

// ResizeMsg tells herdr the new grid and cell size.
type ResizeMsg struct {
	Size GridSize
	Cell CellPx
}

// InputMsg sends events to a pane.
type InputMsg struct {
	PaneID string
	Events []InputEvent
}

// FocusMsg tells herdr whether the window has focus.
type FocusMsg struct{ Focused bool }

// DetachMsg ends the session.
type DetachMsg struct{}

// Encode implements ClientMsg.
func (m HelloMsg) Encode() []byte {
	var e Enc
	e.Vu(CControl)
	e.Str(HelloKind)
	e.Str(helloJSON(m.Size, m.Cell))
	return e.Buf
}

// Encode implements ClientMsg.
func (m ResizeMsg) Encode() []byte {
	var e Enc
	e.Vu(CResize)
	e.Vu(uint64(m.Cell.Width))
	e.Vu(uint64(m.Cell.Height))
	e.Vu(uint64(m.Size.Cols))
	e.Vu(uint64(m.Size.Rows))
	e.Bool(false) // pixel_mouse
	return e.Buf
}

// Encode implements ClientMsg.
func (m InputMsg) Encode() []byte {
	var e Enc
	e.Vu(CPaneInput)
	e.Str(m.PaneID)
	e.Vu(uint64(len(m.Events)))
	for _, ev := range m.Events {
		ev.put(&e)
	}
	return e.Buf
}

// Encode implements ClientMsg.
func (m FocusMsg) Encode() []byte {
	var e Enc
	e.Vu(CFocus)
	e.Bool(m.Focused)
	return e.Buf
}

// Encode implements ClientMsg.
func (DetachMsg) Encode() []byte {
	var e Enc
	e.Vu(CDetach)
	return e.Buf
}

type helloSize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

type helloData struct {
	Generation          int       `json:"generation"`
	CellWidthPx         uint32    `json:"cell_width_px"`
	CellHeightPx        uint32    `json:"cell_height_px"`
	SurfaceSize         helloSize `json:"surface_size"`
	PixelMouse          bool      `json:"pixel_mouse"`
	DirectGraphics      bool      `json:"direct_graphics"`
	EndpointKeybindings bool      `json:"endpoint_keybindings"`
	MouseCapture        bool      `json:"mouse_capture"`
	SurfaceActive       bool      `json:"surface_active"`
	SurfaceReuse        bool      `json:"surface_reuse"`
	SurfaceDelta        bool      `json:"surface_delta"`
	SurfaceScroll       bool      `json:"surface_scroll"`
	SnapshotCodecs      []string  `json:"snapshot_codecs"`
	SurfaceCodecs       []string  `json:"surface_codecs"`
	InputCodecs         []string  `json:"input_codecs"`
	BlobCodecs          []string  `json:"blob_codecs"`
}

func helloJSON(size GridSize, cell CellPx) string {
	b, err := json.Marshal(helloData{
		Generation:     EndpointGeneration,
		CellWidthPx:    cell.Width,
		CellHeightPx:   cell.Height,
		SurfaceSize:    helloSize{Cols: size.Cols, Rows: size.Rows},
		SurfaceActive:  true,
		SnapshotCodecs: []string{SnapshotCodec},
		SurfaceCodecs:  []string{SurfaceCodec},
		InputCodecs:    []string{InputCodec},
		BlobCodecs:     []string{BlobCodec},
	})
	if err != nil {
		// Numbers, bools and strings only: this cannot happen.
		slog.Error("encode endpoint hello", "err", err)
		return ""
	}
	return string(b)
}

func (v KeyEvent) put(e *Enc) {
	e.Vu(0) // Key
	putCode(e, v.Code)
	e.U8(v.Mods)
	if v.Repeat { // ClientKeyKind: Press 0, Repeat 1
		e.Vu(1)
	} else {
		e.Vu(0)
	}
	e.Vu(1)       // repeat_count
	e.None()      // shifted_codepoint
	e.None()      // generated_text
	e.Bool(false) // tracks_release
	e.None()      // physical_key_id
	e.None()      // windows_record
}

func (v TextEvent) put(e *Enc) {
	e.Vu(1) // TextCommit
	e.Str(v.Text)
}

func (v PasteEvent) put(e *Enc) {
	e.Vu(3) // Paste
	e.Str(v.Text)
}

func (v WheelEvent) put(e *Enc) {
	e.Vu(2)                 // Mouse
	e.Vu(4 + uint64(v.Dir)) // ScrollUp, ScrollDown, ScrollLeft, ScrollRight
	e.Vu(0)                 // position: Cell
	e.Vu(uint64(v.Column))
	e.Vu(uint64(v.Row))
	e.None() // geometry: none, as cells are sent, not pixels
	e.U8(v.Mods)
	e.Vu(uint64(v.Lines))
}

// putCode writes one ClientKeyCode: the variant tag, then a char or an
// F number when it has one.
func putCode(e *Enc, c KeyCode) {
	e.Vu(uint64(c.Kind))
	switch c.Kind {
	case KeyChar:
		e.Char(c.Char)
	case KeyF:
		e.U8(c.N)
	}
}

// Welcome is the reply to our hello: the JSON in an endpoint.welcome.v1
// control frame. Unknown fields are ignored.
type Welcome struct {
	Generation    uint32        `json:"generation"`
	ServerVersion string        `json:"server_version"`
	SnapshotCodec string        `json:"snapshot_codec"`
	SurfaceCodec  string        `json:"surface_codec"`
	InputCodec    string        `json:"input_codec"`
	BlobCodec     string        `json:"blob_codec"`
	Error         *WelcomeError `json:"error"`
}

// WelcomeError is a refusal in a welcome.
type WelcomeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ShellSnapshot is the shell.snapshot.v1 control frame: the shell's view,
// without surface data.
type ShellSnapshot struct {
	BootID        string  `json:"boot_id"`
	Revision      uint64  `json:"revision"`
	FocusedPaneID *string `json:"focused_pane_id"`
}

// VersionCheck is the result of an accepted welcome.
type VersionCheck struct {
	// Untested is the server version when contr has not been tested with
	// it. It is empty for a tested version.
	Untested string
}

// Tested reports whether the server version is in TestedHerdrVersions.
func (v VersionCheck) Tested() bool { return v.Untested == "" }

// CheckWelcome refuses only a welcome with an error, a generation other than
// EndpointGeneration, or a codec name other than the v1 one. Any other server
// version connects as untested.
func CheckWelcome(w *Welcome) (VersionCheck, error) {
	if w.Error != nil {
		return VersionCheck{}, fmt.Errorf("herdr refused the endpoint: %s: %s", w.Error.Code, w.Error.Message)
	}
	if w.Generation != EndpointGeneration {
		return VersionCheck{}, fmt.Errorf("herdr speaks endpoint generation %d, contr speaks %d", w.Generation, EndpointGeneration)
	}
	codecs := []struct{ what, offered, wanted string }{
		{"snapshot", w.SnapshotCodec, SnapshotCodec},
		{"surface", w.SurfaceCodec, SurfaceCodec},
		{"input", w.InputCodec, InputCodec},
		{"blob", w.BlobCodec, BlobCodec},
	}
	for _, c := range codecs {
		if c.offered != c.wanted {
			return VersionCheck{}, fmt.Errorf("herdr offers %s codec %s, contr needs %s", c.what, c.offered, c.wanted)
		}
	}
	if slices.Contains(TestedHerdrVersions, w.ServerVersion) {
		return VersionCheck{}, nil
	}
	return VersionCheck{Untested: w.ServerVersion}, nil
}

// ServerMsg is a server message contr decodes: ControlMsg, ShutdownMsg,
// ShellErrorMsg, LegacyWelcomeMsg, SurfaceMsg, PatchMsg, ClipboardMsg or
// OtherMsg.
type ServerMsg interface{ serverMsg() }

// ControlMsg is an EndpointControl frame: Kind names it, Data is JSON.
type ControlMsg struct{ Kind, Data string }

// ShutdownMsg is the server shutdown, with a reason when it gives one.
type ShutdownMsg struct{ Reason *string }

// ShellErrorMsg is an error text from the shell.
type ShellErrorMsg struct{ Text string }

// LegacyWelcomeMsg is a welcome for a first message herdr does not know
// (servers before generation 1). Error is set when it refuses.
type LegacyWelcomeMsg struct{ Error *string }

// SurfaceMsg is the full surface of the focused tab (PaneSurface).
type SurfaceMsg struct{ Frame SurfaceFrame }

// PatchMsg holds row spans that change the last surface (PaneSurfacePatch).
type PatchMsg struct{ Patch SurfacePatch }

// ClipboardMsg is text a program wrote with OSC 52. A payload that is not
// base64 UTF-8 text within the size limit never becomes this: it comes back
// as OtherMsg.
type ClipboardMsg struct{ Text string }

// OtherMsg is a tag contr does not decode. Its body is not read.
type OtherMsg struct{ Tag uint32 }

func (ControlMsg) serverMsg()       {}
func (ShutdownMsg) serverMsg()      {}
func (ShellErrorMsg) serverMsg()    {}
func (LegacyWelcomeMsg) serverMsg() {}
func (SurfaceMsg) serverMsg()       {}
func (PatchMsg) serverMsg()         {}
func (ClipboardMsg) serverMsg()     {}
func (OtherMsg) serverMsg()         {}

// DecodeServer decodes one server payload (the bytes after the length
// prefix).
func DecodeServer(payload []byte) (ServerMsg, error) {
	d := NewDec(payload)
	tag, err := d.Vu32()
	if err != nil {
		return nil, err
	}
	switch tag {
	case SWelcomeLegacy:
		if _, err := d.Vu32(); err != nil { // version
			return nil, err
		}
		// RenderEncoding has two variants; any other index is not a layout we know.
		enc, err := d.Vu32()
		if err != nil {
			return nil, err
		}
		if enc > 1 {
			return nil, NewBadTag("render encoding", enc)
		}
		s, err := optStr(d)
		if err != nil {
			return nil, err
		}
		return LegacyWelcomeMsg{Error: s}, nil
	case SShutdown:
		s, err := optStr(d)
		if err != nil {
			return nil, err
		}
		return ShutdownMsg{Reason: s}, nil
	case SClipboard:
		b, err := d.Bytes()
		if err != nil {
			return nil, err
		}
		text, ok := DecodeOSC52(b)
		if !ok {
			// A dropped write is not an error: the link stays up and ignores it.
			slog.Debug("dropped a clipboard payload: not base64 UTF-8, or over the size limit")
			return OtherMsg{Tag: SClipboard}, nil
		}
		return ClipboardMsg{Text: text}, nil
	case SShellError:
		s, err := d.Str()
		if err != nil {
			return nil, err
		}
		return ShellErrorMsg{Text: s}, nil
	case SControl:
		kind, err := d.Str()
		if err != nil {
			return nil, err
		}
		data, err := d.Str()
		if err != nil {
			return nil, err
		}
		return ControlMsg{Kind: kind, Data: data}, nil
	case SSurface:
		f, err := DecodeSurfaceFrame(d)
		if err != nil {
			return nil, err
		}
		return SurfaceMsg{Frame: f}, nil
	case SPatch:
		p, err := DecodeSurfacePatch(d)
		if err != nil {
			return nil, err
		}
		return PatchMsg{Patch: p}, nil
	}
	return OtherMsg{Tag: tag}, nil
}

func optStr(d *Dec) (*string, error) {
	some, err := d.Option()
	if err != nil || !some {
		return nil, err
	}
	s, err := d.Str()
	if err != nil {
		return nil, err
	}
	return &s, nil
}
