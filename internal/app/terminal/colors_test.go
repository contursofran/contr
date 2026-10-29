package terminal

import (
	"testing"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

const (
	defaultColor = 0
	text         = uint32(theme.Text)
	bgTerminal   = uint32(theme.BgTerminal)
)

func colorCell(fg, bg uint32, attrs uint16) endpoint.Cell {
	return endpoint.Cell{Text: "a", Fg: fg, Bg: bg, Attrs: attrs}
}

func wantResolve(t *testing.T, v, def, want uint32) {
	t.Helper()
	if got := Resolve(v, def); got != want {
		t.Errorf("Resolve(%#x, %#x) = %#x, want %#x", v, def, got, want)
	}
}

func wantColors(t *testing.T, c endpoint.Cell, fg, bg uint32, hasBg bool) {
	t.Helper()
	gf, gb, gh := CellColors(c)
	if gf != fg || gh != hasBg || (hasBg && gb != bg) {
		t.Errorf("CellColors = (%#x, %#x, %v), want (%#x, %#x, %v)", gf, gb, gh, fg, bg, hasBg)
	}
}

func TestNamedZeroIsTheDefaultColour(t *testing.T) {
	wantResolve(t, defaultColor, text, text)
	wantResolve(t, defaultColor, bgTerminal, bgTerminal)
	wantColors(t, colorCell(defaultColor, defaultColor, 0), text, 0, false)
}

func TestNamedColoursMapToTheAnsiPalette(t *testing.T) {
	wantResolve(t, 1, text, Palette16[0])   // black
	wantResolve(t, 2, text, Palette16[1])   // red
	wantResolve(t, 16, text, Palette16[15]) // bright white
}

func TestNamedValuesPastThePaletteAreTheDefault(t *testing.T) {
	wantResolve(t, 17, text, text)
	wantResolve(t, 0xFF, bgTerminal, bgTerminal)
}

func TestIndexed0To15ArePalette(t *testing.T) {
	wantResolve(t, 0x0100_0001, text, Palette16[1])
	wantResolve(t, 0x0100_000F, text, Palette16[15])
}

func TestIndexedCubeAndGreyRampFollowXterm(t *testing.T) {
	for _, c := range []struct{ i, want uint32 }{
		{16, 0x000000}, {21, 0x0000FF}, {196, 0xFF0000}, {231, 0xFFFFFF},
		{232, 0x080808}, {255, 0xEEEEEE},
	} {
		wantResolve(t, 0x0100_0000|c.i, text, c.want)
	}
}

func TestRgbColoursPassThrough(t *testing.T) {
	wantResolve(t, 0x0212_3456, text, 0x12_3456)
}

func TestUnknownKindsAreTheDefaultColour(t *testing.T) {
	wantResolve(t, 0x0312_3456, text, text)
	wantColors(t, colorCell(0x0300_0000, 0x0300_0000, 0), text, 0, false)
}

func TestExplicitColoursAreKeptAndTheDefaultBackgroundStaysNone(t *testing.T) {
	wantColors(t, colorCell(defaultColor, 0x02FF_0000, 0), text, 0xFF0000, true)
}

func TestReversedWithDefaultColoursPaintsTheBackgroundAsText(t *testing.T) {
	wantColors(t, colorCell(defaultColor, defaultColor, Reversed), bgTerminal, text, true)
}

func TestReversedSwapsExplicitColours(t *testing.T) {
	wantColors(t, colorCell(0x02FF_0000, 0x0200_FF00, Reversed), 0x00FF00, 0xFF0000, true)
}

func TestDimHalvesTheForegroundTowardTheBackground(t *testing.T) {
	// (0xED + 0x12) / 2 = 0x7F on each channel; the background is the default.
	wantColors(t, colorCell(defaultColor, defaultColor, Dim), 0x7F7F7F, 0, false)
}

func TestHiddenPaintsTheTextInTheBackgroundColour(t *testing.T) {
	wantColors(t, colorCell(0x02FF_0000, defaultColor, Hidden), bgTerminal, 0, false)
	wantColors(t, colorCell(0x02FF_0000, 0x0200_FF00, Hidden), 0x00FF00, 0x00FF00, true)
}
