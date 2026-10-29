// Package terminal holds the parts of the terminal view that need no window:
// grid metrics, program colors, box drawing, underlines, keys and the
// painter. The painter draws on a ui.Painter; its rules are pure functions.
package terminal

// herdr's modifier bits in endpoint.Cell.Attrs (ratatui's Modifier bits).
// Colors, decorations and the painter share them.
const (
	Bold          uint16 = 1
	Dim           uint16 = 2
	Italic        uint16 = 1 << 2
	Underline     uint16 = 8
	Reversed      uint16 = 64
	Hidden        uint16 = 128
	Strikethrough uint16 = 256
)

// herdr keeps the underline style in bits 12-15. Any of them set means
// underlined.
const (
	UnderlineStyle      uint16 = 0xF000
	UnderlineStyleShift uint16 = 12
)
