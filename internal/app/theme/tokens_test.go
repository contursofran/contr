package theme

import (
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// specRows reads the table rows of docs/spec/tokens.md under a "## name"
// heading (or "### name"): each row as its cells, without the backticks.
func specRows(t *testing.T, section string) [][]string {
	t.Helper()
	b, err := os.ReadFile("../../../docs/spec/tokens.md")
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			in = strings.TrimPrefix(line, "## ") == section
			continue
		}
		if !in || !strings.HasPrefix(line, "| `") {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(line, "| "), " | ") {
			cells = append(cells, strings.Trim(strings.TrimSpace(c), "`"))
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatalf("no rows under %q in tokens.md", section)
	}
	return rows
}

var (
	rgbaRe = regexp.MustCompile(`^rgba\((\d+),(\d+),(\d+),([\d.]+)\)$`)
	hexRe  = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

func specColor(t *testing.T, s string) ui.Color {
	t.Helper()
	if hexRe.MatchString(s) {
		return ui.Hex(s)
	}
	m := rgbaRe.FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("unknown color %q", s)
	}
	n := func(v string) uint8 { i, _ := strconv.Atoi(v); return uint8(i) }
	a, _ := strconv.ParseFloat(m[4], 32)
	return ui.RGBA(n(m[1]), n(m[2]), n(m[3]), float32(a))
}

func specShadow(t *testing.T, s string) Shadow {
	t.Helper()
	var sh Shadow
	// Layers split at "), " so the commas inside rgba() stay.
	for _, part := range strings.SplitAfter(s, "), ") {
		part = strings.TrimSuffix(strings.TrimSpace(part), ",")
		inset := strings.HasPrefix(part, "inset ")
		part = strings.TrimPrefix(part, "inset ")
		i := strings.Index(part, "rgba(")
		var nums []float32
		for _, f := range strings.Fields(part[:i]) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(f, "px"), 32)
			if err != nil {
				t.Fatalf("bad shadow %q", s)
			}
			nums = append(nums, float32(v))
		}
		l := ShadowLayer{Inset: inset, X: nums[0], Y: nums[1], Color: specColor(t, part[i:])}
		if len(nums) > 2 {
			l.Blur = nums[2]
		}
		if len(nums) > 3 {
			l.Wide = nums[3]
		}
		sh = append(sh, l)
	}
	return sh
}

// tokenFields maps each token name of a palette to its field value.
func tokenFields(p *Palette) map[string]any {
	out := map[string]any{}
	v, ty := reflect.ValueOf(p).Elem(), reflect.TypeOf(p).Elem()
	for i := 0; i < ty.NumField(); i++ {
		if name := ty.Field(i).Tag.Get("token"); name != "" {
			out[name] = v.Field(i).Interface()
		}
	}
	return out
}

func TestPalettesHaveEveryColorToken(t *testing.T) {
	rows := specRows(t, "Color")
	for _, p := range []*Palette{Dark, Light} {
		fields := tokenFields(p)
		for _, r := range rows {
			name, want := r[0], specColor(t, r[1+btoi(!p.Dark)])
			got, ok := fields[name]
			if !ok {
				t.Errorf("dark=%v: token %s has no field", p.Dark, name)
				continue
			}
			if got != want {
				t.Errorf("dark=%v: %s = %v, want %v", p.Dark, name, got, want)
			}
		}
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestPalettesHaveEveryShadowToken(t *testing.T) {
	rows := specRows(t, "Shadow")
	for _, p := range []*Palette{Dark, Light} {
		fields := tokenFields(p)
		for _, r := range rows {
			want := specShadow(t, r[1+btoi(!p.Dark)])
			got, ok := fields[r[0]].(Shadow)
			if !ok {
				t.Errorf("dark=%v: token %s has no field", p.Dark, r[0])
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dark=%v: %s = %+v, want %+v", p.Dark, r[0], got, want)
			}
		}
	}
}

// Every tagged field is a token of tokens.md: a field without a row is a
// token the spec does not have.
func TestPaletteFieldsAreSpecTokens(t *testing.T) {
	spec := map[string]bool{}
	for _, sec := range []string{"Color", "Shadow"} {
		for _, r := range specRows(t, sec) {
			spec[r[0]] = true
		}
	}
	for name := range tokenFields(Dark) {
		if !spec[name] {
			t.Errorf("field token %q is not in tokens.md", name)
		}
	}
	if len(tokenFields(Dark)) != len(spec) || len(tokenFields(Light)) != len(spec) {
		t.Errorf("palettes have %d and %d tokens, tokens.md has %d",
			len(tokenFields(Dark)), len(tokenFields(Light)), len(spec))
	}
}

func TestScaleTokens(t *testing.T) {
	want := map[string]float32{}
	add := func(section string, names map[string]float32) {
		for _, r := range specRows(t, section) {
			raw, scale := strings.TrimSuffix(r[1], "px"), 1.0
			if strings.HasSuffix(raw, "%") { // glass-saturate: 140% is 1.4
				raw, scale = strings.TrimSuffix(raw, "%"), 0.01
			}
			v, err := strconv.ParseFloat(raw, 32)
			if err != nil {
				t.Fatalf("%s: bad value %q", r[0], r[1])
			}
			v *= scale
			want[r[0]] = float32(v)
			if got, ok := names[r[0]]; !ok || got != float32(v) {
				t.Errorf("%s = %v, want %v (listed: %v)", r[0], got, v, ok)
			}
		}
		if len(names) != len(specRows(t, section)) {
			t.Errorf("%s: %d values in code, %d in tokens.md", section, len(names), len(specRows(t, section)))
		}
	}
	add("Spacing", map[string]float32{"space-2": Space2, "space-4": Space4, "space-6": Space6,
		"space-7": Space7, "space-8": Space8, "space-9": Space9, "space-10": Space10,
		"space-12": Space12, "space-14": Space14, "space-16": Space16, "space-18": Space18,
		"space-26": Space26})
	add("Radius", map[string]float32{"radius-1": Radius1, "radius-2": Radius2, "radius-4": Radius4,
		"radius-6": Radius6, "radius-7": Radius7, "radius-8": Radius8, "radius-9": Radius9,
		"radius-12": Radius12, "radius-14": Radius14})
	add("Size", map[string]float32{"titlebar-height": SizeTitlebarHeight,
		"sidebar-width": SizeSidebarWidth, "files-width": SizeFilesWidth,
		"row-height": SizeRowHeight, "file-row-height": SizeFileRowHeight,
		"icon-button": SizeIconButton, "tab-max-width": SizeTabMaxWidth,
		"notch-height": SizeNotchHeight, "notch-open-width": SizeNotchOpenWidth,
		"outline-card-width": SizeOutlineCardWidth, "tick-height": SizeTickHeight,
		"meter-height": SizeMeterHeight})
	add("Effect", map[string]float32{"glass-blur": GlassBlur, "glass-saturate": GlassSaturate})
}

func TestTypeStyles(t *testing.T) {
	rows := specRows(t, "Type")
	if len(rows) != len(Styles) {
		t.Fatalf("%d styles in code, %d in tokens.md", len(Styles), len(rows))
	}
	px := func(s string) float32 {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
		return float32(v)
	}
	for _, r := range rows {
		weight, _ := strconv.Atoi(r[4])
		want := TextStyle{Mono: r[1] == "mono", Size: px(r[2]), Line: px(r[3]), Weight: weight}
		got, ok := Style(r[0])
		if !ok {
			t.Errorf("style %s does not resolve", r[0])
			continue
		}
		if got != want {
			t.Errorf("%s = %+v, want %+v", r[0], got, want)
		}
	}
}

func TestUnknownStyleDoesNotResolve(t *testing.T) {
	if _, ok := Style("huge"); ok {
		t.Error("style huge resolves")
	}
}

// Sans styles use Inter, mono styles use JetBrains Mono: the registered
// family and the stack both.
func TestStylesUseTheirFamily(t *testing.T) {
	for _, r := range specRows(t, "Type") {
		s, _ := Style(r[0])
		family, stack := "Inter", FontSans
		if r[1] == "mono" {
			family, stack = "JetBrains Mono", FontMono
		}
		if s.Family() != family || s.Stack() != stack {
			t.Errorf("%s: family %q, stack %q; want %q, %q", r[0], s.Family(), s.Stack(), family, stack)
		}
	}
}

func TestFontStacksMatchSpec(t *testing.T) {
	b, err := os.ReadFile("../../../docs/spec/tokens.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"- sans: `" + FontSans + "`", "- mono: `" + FontMono + "`"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("tokens.md has no line %q", s)
		}
	}
}

// fakePlatform stands for mygo.Theme.
type fakePlatform struct {
	dark    bool
	source  mygo.ThemeSource
	updated func()
	offs    int
}

func (f *fakePlatform) SetSource(s mygo.ThemeSource) { f.source = s }
func (f *fakePlatform) IsDark() bool                 { return f.dark }
func (f *fakePlatform) OnUpdated(fn func()) func()   { f.updated = fn; return func() { f.offs++ } }

// fakeWindow runs updates when the test says so, like the UI thread.
type fakeWindow struct{ queue []func() }

func (w *fakeWindow) Update(fn func()) { w.queue = append(w.queue, fn) }
func (w *fakeWindow) run() {
	q := w.queue
	w.queue = nil
	for _, fn := range q {
		fn()
	}
}

func TestSystemFollowsAppearance(t *testing.T) {
	p, w := &fakePlatform{dark: false}, &fakeWindow{}
	f := Follow(p, w, mygo.ThemeSystem)
	defer f.Stop()
	if p.source != mygo.ThemeSystem {
		t.Errorf("source = %q", p.source)
	}
	if Current() != Light {
		t.Fatal("system light gives the light palette")
	}
	p.dark = true // macOS switches
	p.updated()
	if Current() != Light {
		t.Error("the palette changes in Window.Update, not before")
	}
	w.run()
	if Current() != Dark {
		t.Error("macOS dark gives the dark palette")
	}
}

func TestForcedSourceIgnoresAppearance(t *testing.T) {
	p, w := &fakePlatform{dark: false}, &fakeWindow{}
	f := Follow(p, w, mygo.ThemeSystem)
	defer f.Stop()
	f.SetSource(mygo.ThemeDark)
	if p.source != mygo.ThemeDark || Current() != Dark {
		t.Errorf("dark source: platform %q, current dark = %v", p.source, Current().Dark)
	}
	p.updated()
	w.run()
	if Current() != Dark {
		t.Error("a forced source stays when the appearance updates")
	}
	f.SetSource(mygo.ThemeLight)
	p.dark = true
	if Current() != Light {
		t.Error("light source gives the light palette")
	}
	f.SetSource("")
	if p.source != mygo.ThemeSystem || Current() != Dark {
		t.Errorf("empty source is system: platform %q, current dark = %v", p.source, Current().Dark)
	}
}

func TestStopEndsWatching(t *testing.T) {
	p := &fakePlatform{}
	f := Follow(p, &fakeWindow{}, mygo.ThemeSystem)
	f.Stop()
	f.Stop()
	if p.offs != 1 {
		t.Errorf("off called %d times, want 1", p.offs)
	}
}

func TestUIThemeGetsPaletteValues(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		u := UITheme(p)
		checks := []struct {
			name      string
			got, want ui.Color
		}{
			{"Background", u.Background, p.GlassWindow}, {"Surface", u.Surface, p.Hover},
			{"SurfaceHover", u.SurfaceHover, p.HoverStrong}, {"SurfacePressed", u.SurfacePressed, p.Press},
			{"Border", u.Border, p.InputLine}, {"Text", u.Text, p.Ink95},
			{"TextMuted", u.TextMuted, p.Ink50}, {"Danger", u.Danger, p.Blocked},
			{"Focus", u.Focus, p.FocusRing}, {"Selection", u.Selection, p.Selected},
			{"Warning", u.Warning, p.Ink85},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("dark=%v: %s = %v, want %v", p.Dark, c.name, c.got, c.want)
			}
		}
		// D76 keeps the claude hue for Claude identity, not for warnings.
		if u.Warning == p.Claude {
			t.Errorf("dark=%v: Warning is the claude color", p.Dark)
		}
		if u.Dark != p.Dark {
			t.Errorf("Dark = %v for the palette dark=%v", u.Dark, p.Dark)
		}
	}
}

func TestApplySetsUITheme(t *testing.T) {
	var seen *ui.Theme
	view := func(c *ui.Context) {
		Apply(c)
		seen = c.Theme()
		ui.Text(c, "x")
	}
	for _, p := range []*Palette{Light, Dark} {
		current.Store(p)
		ui.NewTester(view, 100, 40)
		if seen.Text != p.Ink95 || seen.Dark != p.Dark {
			t.Errorf("dark=%v: ui theme Text = %v, Dark = %v", p.Dark, seen.Text, seen.Dark)
		}
	}
}
