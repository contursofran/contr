package term

// KeyKind is the variant of a key code.
type KeyKind uint8

// Key kinds.
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

// Key modifier bits.
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
type InputEvent interface{ inputEvent() }

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
// rect. The source scrolls its history by Lines, or sends one wheel report
// or arrow key to the program.
type WheelEvent struct {
	Dir    WheelDir
	Column uint16
	Row    uint16
	Mods   uint8
	Lines  uint16
}

func (KeyEvent) inputEvent()   {}
func (TextEvent) inputEvent()  {}
func (PasteEvent) inputEvent() {}
func (WheelEvent) inputEvent() {}
