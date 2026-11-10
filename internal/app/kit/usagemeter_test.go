package kit

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

func meterTester(meters []Meter) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Fill().Width(240).Background(theme.Current().GlassWindow).Children(func() {
			UsageMeter(c, meters)
		})
	}, 240, 140)
}

// pixelAt reads the pixel at a fraction of a box's width, in its middle.
func pixelAt(tt *ui.Tester, r ui.Rect, frac float32) color.Color {
	return tt.Image().At(int(r.X+r.W*frac), int(r.Y+r.H/2))
}

func find(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no element %q; texts %q", label, tt.Texts())
	}
	return r
}

func TestMetersShowTheirLabelsValuesAndFill(t *testing.T) {
	tt := meterTester([]Meter{
		{Label: "Context"},
		{Label: "Claude", Value: "50%", Percent: 50, Has: true},
		{Label: "Codex", Value: "100%", Percent: 100, Has: true},
	})
	tt.Frame()
	for _, want := range []string{"Context", "Claude", "Codex", "50%", "100%"} {
		if !tt.HasText(want) {
			t.Errorf("no text %q; texts %q", want, tt.Texts())
		}
	}
	claude, codex := find(t, tt, "Claude meter"), find(t, tt, "Codex meter")
	if claude.H != theme.SizeMeterHeight {
		t.Errorf("meter height %v, want %d", claude.H, theme.SizeMeterHeight)
	}
	// 50 %: the left quarter is filled, the right quarter is the track.
	if pixelAt(tt, claude, 0.25) == pixelAt(tt, claude, 0.75) {
		t.Error("a half-full meter looks the same at both ends")
	}
	// 100 %: the whole track is filled.
	if pixelAt(tt, codex, 0.05) != pixelAt(tt, codex, 0.95) {
		t.Error("a full meter has an unfilled end")
	}
	if pixelAt(tt, codex, 0.5) != pixelAt(tt, claude, 0.25) {
		t.Error("a filled part is not the same color on both meters")
	}
}

func TestMetersWithoutDataShowADashAndAnEmptyTrack(t *testing.T) {
	tt := meterTester([]Meter{{Label: "Context"}, {Label: "Claude", Value: "70%", Percent: 70}})
	tt.Frame()
	if tt.HasText("70%") {
		t.Error("a meter without data shows its value")
	}
	if got := dashes(tt); got != 2 {
		t.Errorf("%d dashes, want 2; texts %q", got, tt.Texts())
	}
	for _, label := range []string{"Context meter", "Claude meter"} {
		r := find(t, tt, label)
		if pixelAt(tt, r, 0.05) != pixelAt(tt, r, 0.95) {
			t.Errorf("%s: the track is not empty", label)
		}
	}
}

func dashes(tt *ui.Tester) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == NoValue {
			n++
		}
	}
	return n
}

func TestAMeterOutOfRangeIsFullOrEmpty(t *testing.T) {
	tt := meterTester([]Meter{
		{Label: "Claude", Value: "140%", Percent: 140, Has: true},
		{Label: "Codex", Value: "0%", Percent: -5, Has: true},
	})
	tt.Frame()
	over, under := find(t, tt, "Claude meter"), find(t, tt, "Codex meter")
	if pixelAt(tt, over, 0.05) != pixelAt(tt, over, 0.95) {
		t.Error("an over-range meter is not full")
	}
	if pixelAt(tt, under, 0.05) != pixelAt(tt, under, 0.95) || pixelAt(tt, under, 0.5) == pixelAt(tt, over, 0.5) {
		t.Error("a negative meter is not empty")
	}
}

// The block has space-10 at the sides, so in the panel its text sits 20px in
// (D228), and each meter after the first has space-6 more above it than the
// space-7 gap.
func TestUsageMeterInsetsAndSpaceBetweenMeters(t *testing.T) {
	tt := meterTester([]Meter{{Label: "Context"}, {Label: "Claude"}, {Label: "Codex"}})
	tt.Frame()
	block, ctx, claude, codex := find(t, tt, "usage"), find(t, tt, "Context line"), find(t, tt, "Claude line"), find(t, tt, "Codex line")
	if got := ctx.X - block.X; got != theme.Space10 {
		t.Errorf("line at %v from the block side, want %d", got, theme.Space10)
	}
	if got := block.X + block.W - (ctx.X + ctx.W); got != theme.Space10 {
		t.Errorf("line ends %v from the block side, want %d", got, theme.Space10)
	}
	for _, p := range [][2]ui.Rect{{ctx, claude}, {claude, codex}} {
		if got := p[1].Y - (p[0].Y + p[0].H); got != theme.Space7+theme.Space6 {
			t.Errorf("space between meters %v, want %d", got, theme.Space7+theme.Space6)
		}
	}
}
