package apptest

import (
	"image"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// ThemeCases are the two themes, for tests that check each.
var ThemeCases = []struct {
	Name string
	Src  mygo.ThemeSource
	Pal  *theme.Palette
}{
	{"dark", mygo.ThemeDark, theme.Dark},
	{"light", mygo.ThemeLight, theme.Light},
}

// Near reports that a and b differ by at most tol in each channel.
func Near(a, b ui.Color, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

// RGB is the color of the pixel at x, y.
func RGB(img *image.RGBA, x, y float32) ui.Color {
	p := img.RGBAAt(int(x), int(y))
	return ui.RGB(p.R, p.G, p.B)
}
