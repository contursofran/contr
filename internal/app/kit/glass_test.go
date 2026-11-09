package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

func TestGlassFloatSetsTheFill(t *testing.T) {
	p := theme.Dark
	plain := ui.Render(func(c *ui.Context) {
		ui.Box(c).Width(20).Height(20)
	}, 20, 20, 1)
	img := ui.Render(func(c *ui.Context) {
		GlassFloat(ui.Box(c).Width(20).Height(20), p)
	}, 20, 20, 1)
	if img.RGBAAt(10, 10) == plain.RGBAAt(10, 10) {
		t.Errorf("pixel %v: no glass-float fill %v", img.RGBAAt(10, 10), p.GlassFloat)
	}
}
