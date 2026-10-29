package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Port of the Rust test legend_shows_all_ten.
func TestLegendShowsAllTen(t *testing.T) {
	tt := ui.NewTester((&Legend{}).View, 480, 420)
	var previous float32
	for i, s := range icons.All {
		r, ok := tt.Find(s.Label())
		if !ok {
			t.Fatalf("no icon labelled %q", s.Label())
		}
		// Find returns the first match: the icon comes before the text.
		if r.W != theme.IconSize || r.H != theme.IconSize {
			t.Errorf("%s icon is %vx%v, want %v square", s.Label(), r.W, r.H, float32(theme.IconSize))
		}
		if i > 0 && r.Y <= previous {
			t.Errorf("%s is not below the row before", s.Label())
		}
		previous = r.Y
	}
	if !tt.HasText("worktree waits on the owner") || !tt.HasText("contr status icons") {
		t.Errorf("legend texts %q", tt.Texts())
	}
}

// Port of the Rust test working_redraws_only_its_row. MyGo paints the
// spinner from the last build, so no row is built again, and only the
// working icon's pixels change.
func TestWorkingRedrawsOnlyItsRow(t *testing.T) {
	l := &Legend{}
	tt := ui.NewTester(l.View, 480, 420)
	if !tt.FramePending() {
		t.Fatal("the working icon asks for no frames")
	}
	builds := l.rowBuilds
	before := copyImage(tt.Image())
	tt.Advance(350 * time.Millisecond) // a quarter of a turn
	after := tt.Image()

	if l.rowBuilds != builds {
		t.Errorf("rows built %v times before and %v after the turn", builds, l.rowBuilds)
	}
	for _, s := range icons.All {
		r, _ := tt.Find(s.Label())
		row := image.Rect(0, int(r.Y)-4, 480, int(r.Y)+int(r.H)+4)
		changed := differs(before, after, row)
		if s == icons.Working && !changed {
			t.Error("the working row did not change")
		}
		if s != icons.Working && changed {
			t.Errorf("the %s row was repainted", s.Label())
		}
	}
}

func copyImage(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func differs(a, b *image.RGBA, area image.Rectangle) bool {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return true
			}
		}
	}
	return false
}
