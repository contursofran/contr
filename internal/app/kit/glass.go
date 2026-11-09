package kit

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// floatGlass is the glass-float material: the blur of what is behind the
// surface, then the glass-float fill over it. A MyGo material replaces the
// element's background, so one material has to paint both.
type floatGlass struct{ fill ui.Color }

// floatBlur is the blur under the glass-float fill.
var floatBlur = glass.Blur{Radius: theme.GlassBlur}

// PaintMaterial blurs what is behind box, then fills box with the tint.
func (f floatGlass) PaintMaterial(pt *ui.Painter, box ui.Rect, radii [4]float32) {
	floatBlur.PaintMaterial(pt, box, radii)
	pt.FillRadii(box, radii, f.fill)
}

// String describes the material, as the MyGo inspector shows it: the token and the blur.
func (f floatGlass) String() string {
	return "glass-float + " + floatBlur.String()
}

// GlassFloat gives a floating surface the glass-float fill and the blur of
// what is behind it, together. The fill is 72-80% opaque, so without the
// blur the terminal text under it shows through (#536). Every glass-float
// surface uses this helper; a test fails on any other use of the token as a
// background.
func GlassFloat(e ui.Element, p *theme.Palette) ui.Element {
	return e.Material(floatGlass{fill: p.GlassFloat})
}
