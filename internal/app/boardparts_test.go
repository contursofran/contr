package app

import (
	stdcolor "image/color"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// pixelAt reads the colour of the last frame at (x, y) in DIPs.
func pixelAt(tt *ui.Tester, x, y float32) stdcolor.RGBA {
	return tt.Image().RGBAAt(int(x), int(y))
}

// rowFill reads the fill of a row at its left padding, away from icons and
// text.
func rowFill(t *testing.T, tt *ui.Tester, label string) uint32 {
	t.Helper()
	r := findRect(t, tt, label)
	p := pixelAt(tt, r.X+4, r.Y+r.H/2)
	return uint32(p.R)<<16 | uint32(p.G)<<8 | uint32(p.B)
}

// strikeRun reports the longest run of pixels that differ from the window
// background on any line near the middle of r. A struck text has a line
// across all its letters; a plain text has gaps.
func strikeRun(tt *ui.Tester, r ui.Rect) int {
	bg := theme.Color(theme.BgWindow)
	best := 0
	for y := r.Y + r.H/2 - 4; y <= r.Y+r.H/2+4; y++ {
		run := 0
		for x := r.X; x < r.X+r.W; x++ {
			p := pixelAt(tt, x, y)
			if p.R != bg.R || p.G != bg.G || p.B != bg.B {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
	}
	return best
}
