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

// From 80% the Context value and the fill are ink-100 (D264); no hue.
func TestASoonMeterIsInk100(t *testing.T) {
	tt := meterTester([]Meter{
		{Label: "Context", Detail: "200k · compacts soon", Value: "100%", Percent: 100, Has: true, Soon: true},
		{Label: "Claude", Value: "100%", Percent: 100, Has: true},
	})
	tt.Frame()
	p := theme.Current()
	soon, plain := find(t, tt, "Context meter"), find(t, tt, "Claude meter")
	r, g, b, _ := pixelAt(tt, soon, 0.5).RGBA()
	w := p.Ink100
	if uint8(r>>8) != w.R || uint8(g>>8) != w.G || uint8(b>>8) != w.B {
		t.Errorf("the soon fill is %v, want ink-100 %v", pixelAt(tt, soon, 0.5), p.Ink100)
	}
	if pixelAt(tt, soon, 0.5) == pixelAt(tt, plain, 0.5) {
		t.Error("a soon fill looks like the meter-fill")
	}
	for _, c := range []struct {
		m    Meter
		text string
		ink  ui.Color
	}{
		{Meter{Value: "86%", Has: true, Soon: true}, "86%", p.Ink100},
		{Meter{Value: "45%", Has: true}, "45%", p.Ink64},
		{Meter{Value: "86%", Soon: true}, NoValue, p.Ink48},
	} {
		if text, ink := meterValueText(p, c.m); text != c.text || ink != c.ink {
			t.Errorf("%+v: %q %v, want %q %v", c.m, text, ink, c.text, c.ink)
		}
	}
}

// A long detail clips; the label and the value keep their whole width (D264).
func TestALongDetailClipsAndTheLabelAndValueStay(t *testing.T) {
	long := Meter{Label: "Context", Detail: "172k of 200k · compacts soon, and more text", Value: "86%", Percent: 86, Has: true, Soon: true}
	short := Meter{Label: "Context", Value: "86%", Percent: 86, Has: true}
	measure := func(m Meter) (label, value ui.Rect, tt *ui.Tester) {
		tt = meterTester([]Meter{m})
		tt.Frame()
		return find(t, tt, "Context"), find(t, tt, "86%"), tt
	}
	l1, v1, tt := measure(long)
	_, v2, _ := measure(short)
	if l1.W < 30 {
		t.Errorf("the label clips to %v", l1.W)
	}
	if v1.W != v2.W || v1.X != v2.X {
		t.Errorf("the value moved or clipped: %+v, want %+v", v1, v2)
	}
	d := find(t, tt, long.Detail)
	if d.X < l1.X+l1.W || d.X+d.W > v1.X {
		t.Errorf("the detail %+v does not sit between the label %+v and the value %+v", d, l1, v1)
	}
	// Without data the detail does not show.
	tt = meterTester([]Meter{{Label: "Context", Detail: "90k of 200k"}})
	tt.Frame()
	if tt.HasText("90k of 200k") {
		t.Error("a meter without data shows its detail")
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
