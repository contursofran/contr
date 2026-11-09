package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
)

// Views read the palette on each build, so Light shows Light colors (#403,
// D157). Each test builds the view in Dark and in Light and checks the
// pixels against the palette of that theme.

var themeCases = []struct {
	name string
	src  mygo.ThemeSource
	pal  *theme.Palette
}{
	{"dark", mygo.ThemeDark, theme.Dark},
	{"light", mygo.ThemeLight, theme.Light},
}

func near(a, b ui.Color, tol int) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol && d(a.B, b.B) <= tol
}

func rgb(img *image.RGBA, x, y float32) ui.Color {
	p := img.RGBAAt(int(x), int(y))
	return ui.RGB(p.R, p.G, p.B)
}

// textInk returns the pixel of r that differs most from bg: the glyph core.
func textInk(img *image.RGBA, r ui.Rect, bg ui.Color) ui.Color {
	var best ui.Color
	most := -1
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			c := rgb(img, float32(x), float32(y))
			d := 0
			for _, v := range [3]int{int(c.R) - int(bg.R), int(c.G) - int(bg.G), int(c.B) - int(bg.B)} {
				if v < 0 {
					v = -v
				}
				d += v
			}
			if d > most {
				best, most = c, d
			}
		}
	}
	return best
}

// wantInk fails when the text of r is not drawn in ink over bg. The glyph
// core is within 10 per channel of the full ink color.
func wantInk(t *testing.T, name string, img *image.RGBA, r ui.Rect, bg, ink ui.Color) {
	t.Helper()
	full := theme.Over(ink, bg)
	got := textInk(img, r, bg)
	reach := func(a, b ui.Color) int { // largest channel gap
		m := 0
		for _, v := range [3]int{int(a.R) - int(b.R), int(a.G) - int(b.G), int(a.B) - int(b.B)} {
			if v < 0 {
				v = -v
			}
			if v > m {
				m = v
			}
		}
		return m
	}
	if reach(got, full) > 10 {
		t.Errorf("%s: text core %v, want near %v over %v", name, got, full, bg)
	}
}

func TestPlaceholderFollowsTheTheme(t *testing.T) {
	for _, tc := range themeCases {
		themetest.Use(t, tc.src)
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Background(theme.Current().Ground).Font(theme.FontFamily).
				FontSize(theme.FontSize).Children(func() { placeholder("Terminal")(c) })
		}, 200, 60)
		tt.Frame()
		r := apptest.Box(t, tt, "[Terminal]")
		wantInk(t, tc.name, tt.Image(), r, tc.pal.Ground, tc.pal.Ink42)
	}
}

func TestBoardEmptyLinesFollowTheTheme(t *testing.T) {
	problem := func() agentstate.Snapshot { return agentstate.Snapshot{Problem: "no hc"} }
	for _, tc := range themeCases {
		themetest.Use(t, tc.src)
		for _, label := range []string{LabelBoardEmpty, LabelBoardProblem} {
			p := &BoardPanel{}
			if label == LabelBoardProblem {
				p = &BoardPanel{Snapshot: problem}
			}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Background(theme.Current().Ground).Font(theme.FontFamily).
					FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).Children(func() { p.View(c) })
			}, theme.PanelWidth, 400)
			tt.Frame()
			r := apptest.Box(t, tt, label)
			wantInk(t, tc.name+" "+label, tt.Image(), r, tc.pal.Ground, tc.pal.Ink50)
		}
	}
}
