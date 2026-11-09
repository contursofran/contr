package kit

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// TestGlassFloatSetsTheFill checks that the surface is glass-float composited over its backdrop:
// a blur of a plain backdrop is the backdrop, so the pixel is the fill over it.
func TestGlassFloatSetsTheFill(t *testing.T) {
	for _, tc := range apptest.ThemeCases {
		p := tc.Pal
		backdrop := ui.Hex("#3366cc") // a hue far from both glass-float fills
		img := ui.Render(func(c *ui.Context) {
			ui.Box(c).Width(40).Height(40).Background(backdrop).Children(func() {
				GlassFloat(ui.Box(c).Absolute().Top(10).Left(10).Width(20).Height(20), p)
			})
		}, 40, 40, 1)
		want := theme.Over(p.GlassFloat, backdrop)
		if got := apptest.RGB(img, 20, 20); !apptest.Near(got, want, 3) {
			t.Errorf("%s: pixel %v, want glass-float %v over the backdrop %v: %v", tc.Name, got, p.GlassFloat, backdrop, want)
		}
	}
}

// TestGlassFloatNamesItselfInTheInspector checks the text of the material in the MyGo inspector.
func TestGlassFloatNamesItselfInTheInspector(t *testing.T) {
	want := fmt.Sprintf("glass-float + blur(%g)", float64(theme.GlassBlur))
	if got := (floatGlass{}).String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
