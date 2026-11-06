package theme

import (
	"testing"

	"github.com/egoist/mygo"
)

type tintPlatform struct{}

func (tintPlatform) SetSource(mygo.ThemeSource)    {}
func (tintPlatform) IsDark() bool                  { return true }
func (tintPlatform) OnUpdated(func()) (off func()) { return func() {} }

type tintWindow struct{}

func (tintWindow) Update(fn func()) { fn() }

func useTheme(t *testing.T, src mygo.ThemeSource) {
	t.Helper()
	Follow(tintPlatform{}, tintWindow{}, src).Stop()
	t.Cleanup(func() {
		ClearTints()
		Follow(tintPlatform{}, tintWindow{}, mygo.ThemeDark).Stop()
	})
}

func f64(v float64) *float64 { return &v }

func TestTintDefaultsMatchThePalettes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		p := For(dark)
		d := tintDefaults[idx(dark)]
		if got := int(p.GlassWindow.A); got != int(d.window*255+0.5) {
			t.Errorf("dark=%v window alpha %d does not match %v", dark, got, d.window)
		}
		if got := int(p.Panel.A); got != int(d.panel*255+0.5) {
			t.Errorf("dark=%v panel alpha %d does not match %v", dark, got, d.panel)
		}
	}
}

func TestTintOverridesApplyPerTheme(t *testing.T) {
	useTheme(t, mygo.ThemeDark)
	SetTint(true, f64(0.8), f64(0.1))
	SetTint(false, f64(0.2), nil)
	p := Current()
	if p.GlassWindow.A != 204 || p.Panel.A != 26 {
		t.Errorf("dark window %d panel %d, want 204 and 26", p.GlassWindow.A, p.Panel.A)
	}
	if p.GlassWindow.R != Dark.GlassWindow.R || p.Ground != Dark.Ground {
		t.Error("the override changed more than the alpha")
	}
	if Dark.GlassWindow.A == 204 {
		t.Error("the override changed the base palette")
	}
	Follow(tintPlatform{}, tintWindow{}, mygo.ThemeLight).Stop()
	p = Current()
	if p.GlassWindow.A != 51 || p.Panel.A != Light.Panel.A {
		t.Errorf("light window %d panel %d, want 51 and the default", p.GlassWindow.A, p.Panel.A)
	}
}

func TestTintAppliesAtOnceAndClears(t *testing.T) {
	useTheme(t, mygo.ThemeLight)
	w, p := TintValues()
	if w != 0.60 || p != 0.70 {
		t.Errorf("defaults %v %v, want 0.60 0.70", w, p)
	}
	SetTint(false, f64(0.5), f64(0.5))
	if got := Current().Panel.A; got != 128 {
		t.Errorf("panel alpha %d, want 128 at once", got)
	}
	if w, p := TintValues(); w != 0.5 || p != 0.5 {
		t.Errorf("values %v %v, want 0.5 0.5", w, p)
	}
	SetTint(false, nil, nil)
	if Current() != Light {
		t.Error("clearing the overrides did not give back the base palette")
	}
}

func TestTintOfTheOtherThemeKeepsTheCurrentPalette(t *testing.T) {
	useTheme(t, mygo.ThemeDark)
	SetTint(false, f64(0.9), nil)
	if Current() != Dark {
		t.Error("a Light override changed the Dark palette")
	}
}
