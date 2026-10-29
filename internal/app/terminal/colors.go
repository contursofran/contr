package terminal

import (
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Program colors for the terminal. The terminal shows the colors a program
// asks for, as herdr sends them (D10); greyscale applies to contr's own UI
// only. A packed color is herdr's uint32: the top byte is the kind (0 named,
// 1 indexed, 2 RGB) and the low bits are the value.

// Palette16 holds the 16 ANSI colors in herdr's order: black, red, green,
// yellow, blue, magenta, cyan, white, then the bright eight. Own values,
// readable on theme.BgTerminal.
var Palette16 = [16]uint32{
	0x1E1E1E, 0xE06C6C, 0x8CC07A, 0xE0C070, 0x6CA8E8, 0xC080D8, 0x5CC0C8, 0xD0D0D0, 0x5C5C5C,
	0xFF8080, 0xA8E090, 0xFFD890, 0x90C4FF, 0xDCA0F0, 0x80DCE4, 0xFFFFFF,
}

const (
	kindNamed   = 0
	kindIndexed = 1
	kindRGB     = 2
)

// Resolve returns the color (0xRRGGBB) of a packed herdr color, or def when it
// names the default color.
func Resolve(v, def uint32) uint32 {
	if c, ok := resolveExplicit(v); ok {
		return c
	}
	return def
}

// resolveExplicit reports ok for a color the program set; not ok for the
// default color and for kinds this client does not know.
func resolveExplicit(v uint32) (uint32, bool) {
	switch v >> 24 {
	case kindNamed:
		// Named 1..=16 are ANSI 0..=15; named 0 is the default color.
		if v >= 1 && v <= 16 {
			return Palette16[v-1], true
		}
	case kindIndexed:
		return indexed(uint8(v & 0xFF)), true
	case kindRGB:
		return v & 0xFFFFFF, true
	}
	return 0, false
}

// indexed returns xterm's 256 colors: 0-15 are the ANSI palette, 16-231 a
// 6x6x6 cube, 232-255 a grey ramp.
func indexed(i uint8) uint32 {
	switch {
	case i <= 15:
		return Palette16[i]
	case i <= 231:
		n := uint32(i - 16)
		return cubeLevel(n/36)<<16 | cubeLevel((n/6)%6)<<8 | cubeLevel(n%6)
	default:
		grey := 8 + 10*uint32(i-232)
		return grey<<16 | grey<<8 | grey
	}
}

// cubeLevel is one level of the cube: 0, then 95, 135, 175, 215 and 255.
func cubeLevel(n uint32) uint32 {
	if n == 0 {
		return 0
	}
	return 55 + 40*n
}

// dim halves fg toward bg, channel by channel.
func dim(fg, bg uint32) uint32 {
	channel := func(shift uint) uint32 { return ((fg>>shift)&0xFF + (bg>>shift)&0xFF) / 2 }
	return channel(16)<<16 | channel(8)<<8 | channel(0)
}

func has(cell endpoint.Cell, flag uint16) bool { return cell.Attrs&flag != 0 }

// CellColors returns the foreground (0xRRGGBB) and background to paint a cell
// with. hasBg is false for the default background, which the view paints as
// theme.BgTerminal. Reversed swaps the two colors, Dim halves the foreground
// toward the background, and Hidden paints the foreground in the background
// color, so the glyph does not show.
func CellColors(cell endpoint.Cell) (fg uint32, bg uint32, hasBg bool) {
	f, fOK := resolveExplicit(cell.Fg)
	b, bOK := resolveExplicit(cell.Bg)
	if has(cell, Reversed) {
		if !bOK {
			b = theme.BgTerminal
		}
		if !fOK {
			f = theme.Text
		}
		f, b, bOK = b, f, true
	} else if !fOK {
		f = theme.Text
	}
	backdrop := uint32(theme.BgTerminal)
	if bOK {
		backdrop = b
	}
	if has(cell, Dim) {
		f = dim(f, backdrop)
	}
	if has(cell, Hidden) {
		f = backdrop
	}
	return f, b, bOK
}
