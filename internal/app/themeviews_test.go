package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/theme/themetest"
	"github.com/contursofran/contr/internal/app/viewers"
)

// Views read the palette on each build, so Light shows Light colors (#403,
// D157). Each test builds the view in Dark and in Light and checks the
// pixels against the palette of that theme.

// textInk returns the pixel of r that differs most from bg: the glyph core.
func textInk(img *image.RGBA, r ui.Rect, bg ui.Color) ui.Color {
	var best ui.Color
	most := -1
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			c := apptest.RGB(img, float32(x), float32(y))
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
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).Fill().Background(theme.Current().Ground).Font(theme.FontFamily).
				FontSize(theme.FontSize).Children(func() { placeholder("Terminal")(c) })
		}, 200, 60)
		tt.Frame()
		r := apptest.Box(t, tt, "[Terminal]")
		wantInk(t, tc.Name, tt.Image(), r, tc.Pal.Ground, tc.Pal.Ink42)
	}
}

func TestBoardEmptyLinesFollowTheTheme(t *testing.T) {
	problem := func() agentstate.Snapshot { return agentstate.Snapshot{Problem: "no hc"} }
	for _, tc := range apptest.ThemeCases {
		themetest.Use(t, tc.Src)
		for _, label := range []string{viewers.LabelBoardEmpty, viewers.LabelBoardProblem} {
			p := &viewers.BoardPanel{}
			if label == viewers.LabelBoardProblem {
				p = &viewers.BoardPanel{Snapshot: problem}
			}
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Fill().Background(theme.Current().Ground).Font(theme.FontFamily).
					FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).Children(func() { p.View(c) })
			}, theme.PanelWidth, 400)
			tt.Frame()
			r := apptest.Box(t, tt, label)
			wantInk(t, tc.Name+" "+label, tt.Image(), r, tc.Pal.Ground, tc.Pal.Ink50)
		}
	}
}
