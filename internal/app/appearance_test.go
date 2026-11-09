package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/prefs"
	"github.com/contursofran/contr/internal/usage"
)

// fakeTheme is the platform theme: it records the source and answers IsDark.
type fakeTheme struct {
	mu      sync.Mutex
	src     mygo.ThemeSource
	sources []mygo.ThemeSource
	dark    bool
}

func (p *fakeTheme) SetSource(s mygo.ThemeSource) {
	p.mu.Lock()
	p.src = s
	p.sources = append(p.sources, s)
	p.mu.Unlock()
}
func (p *fakeTheme) IsDark() bool                  { return p.dark }
func (p *fakeTheme) OnUpdated(func()) (off func()) { return func() {} }

// restoreTheme puts the dark palette back after a test that changed it.
func restoreTheme(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { theme.Follow(&fakeTheme{}, &fakeWindow{}, mygo.ThemeDark).Stop() })
}

func themeFrame(t *testing.T, load func() (prefs.Prefs, error)) (*Frame, *fakeTheme) {
	t.Helper()
	restoreTheme(t)
	p := &fakeTheme{dark: true}
	f := NewFrame()
	f.LoadPrefs, f.ThemePlatform = load, p
	return f, p
}

func TestStartThemeAppliesTheStoredSource(t *testing.T) {
	for _, tc := range []struct {
		stored mygo.ThemeSource
		dark   bool
	}{{mygo.ThemeLight, false}, {mygo.ThemeDark, true}} {
		f, p := themeFrame(t, func() (prefs.Prefs, error) { return prefs.Prefs{Appearance: tc.stored}, nil })
		f.StartTheme()
		if p.src != tc.stored || f.Appearance() != tc.stored || theme.Current().Dark != tc.dark {
			t.Errorf("stored %q: platform %q, frame %q, dark %v", tc.stored, p.src, f.Appearance(), theme.Current().Dark)
		}
	}
}

func TestStartThemeFallsBackToSystem(t *testing.T) {
	for name, load := range map[string]func() (prefs.Prefs, error){
		"unknown value": func() (prefs.Prefs, error) { return prefs.Prefs{Appearance: "sepia"}, nil },
		"read error":    func() (prefs.Prefs, error) { return prefs.Prefs{Appearance: mygo.ThemeLight}, errors.New("bad file") },
		"no store":      nil,
	} {
		f, p := themeFrame(t, load)
		f.StartTheme()
		if p.src != mygo.ThemeSystem || f.Appearance() != mygo.ThemeSystem {
			t.Errorf("%s: platform %q, frame %q; want system", name, p.src, f.Appearance())
		}
	}
}

func TestStartThemeShowsAReadErrorWhenTheFrameAttaches(t *testing.T) {
	f, _ := themeFrame(t, func() (prefs.Prefs, error) { return prefs.Prefs{}, errors.New("bad file") })
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	f.StartTheme()
	stop := f.Attach(&fakeWindow{visible: true})
	defer stop()
	if msg, _ := lastToast(f); msg != "bad file" {
		t.Errorf("status %q, want the read error", msg)
	}
}

func TestAttachShowsTheAgentsNoticeOnce(t *testing.T) {
	f, _ := themeFrame(t, nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	f.AgentsNotice = "agents.json is in the old format"
	stop := f.Attach(&fakeWindow{visible: true})
	defer stop()
	if got := f.Toasts.Pending(); len(got) != 1 || got[0] != f.AgentsNotice {
		t.Errorf("toasts %q", got)
	}
}

func TestSetAppearanceAppliesAtOnceAndStores(t *testing.T) {
	f, p := themeFrame(t, nil)
	var mu sync.Mutex
	var saved []prefs.Prefs
	done := make(chan struct{}, 4)
	f.SavePrefs = func(v prefs.Prefs) error {
		mu.Lock()
		saved = append(saved, v)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	f.StartTheme()
	f.SetAppearance(mygo.ThemeLight)
	if p.src != mygo.ThemeLight || theme.Current().Dark {
		t.Errorf("platform %q, dark %v; want light at once", p.src, theme.Current().Dark)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the setting was not stored")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 1 || saved[0].Appearance != mygo.ThemeLight {
		t.Errorf("saved %+v, want light", saved)
	}
}

func TestSetAppearanceShowsASaveError(t *testing.T) {
	f, _ := themeFrame(t, nil)
	shown := make(chan string, 1)
	f.Toasts = state.NewToasts(nil)
	f.post = func(fn func()) { fn(); msg, _ := lastToast(f); shown <- msg }
	f.SavePrefs = func(prefs.Prefs) error { return errors.New("disk full") }
	f.SetAppearance(mygo.ThemeDark)
	select {
	case msg := <-shown:
		if msg != "disk full" {
			t.Errorf("status %q, want the save error", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the save error did not reach the status bar")
	}
}

func TestSetAppearanceKeepsAnUnknownValueOut(t *testing.T) {
	f, p := themeFrame(t, nil)
	f.StartTheme()
	f.SetAppearance("sepia")
	if p.src != mygo.ThemeSystem || f.Appearance() != mygo.ThemeSystem {
		t.Errorf("platform %q, frame %q; want system", p.src, f.Appearance())
	}
}

func tintFrame(t *testing.T, stored prefs.Prefs) (*Frame, chan prefs.Prefs) {
	t.Helper()
	f, _ := themeFrame(t, func() (prefs.Prefs, error) { return stored, nil })
	t.Cleanup(theme.ClearTints)
	saved := make(chan prefs.Prefs, 8)
	f.SavePrefs = func(v prefs.Prefs) error { saved <- v; return nil }
	f.StartTheme()
	return f, saved
}

func pf(v float64) *float64 { return &v }

func TestStartThemeAppliesTheStoredTint(t *testing.T) {
	f, _ := tintFrame(t, prefs.Prefs{Appearance: mygo.ThemeDark,
		Tint: prefs.Tints{Dark: prefs.Tint{Window: pf(0.8)}, Light: prefs.Tint{Panel: pf(0.1)}}})
	if w, p := f.Tint(); w != 80 || p != 85 {
		t.Errorf("Tint = %v %v, want 80 85", w, p)
	}
	if theme.Current().GlassWindow.A != 204 {
		t.Error("the palette does not have the stored window alpha")
	}
}

func TestSetTintAppliesAtOnceAndStoresOnceAfterTheDrag(t *testing.T) {
	f, saved := tintFrame(t, prefs.Prefs{Appearance: mygo.ThemeDark})
	for _, v := range []float64{10, 20, 30, 40} {
		f.SetTint(v, 60)
	}
	if theme.Current().GlassWindow.A != 102 || theme.Current().Panel.A != 153 {
		t.Errorf("window %d panel %d, want 102 and 153 at once", theme.Current().GlassWindow.A, theme.Current().Panel.A)
	}
	select {
	case v := <-saved:
		if v.Tint.Dark.Window == nil || *v.Tint.Dark.Window != 0.4 || v.Appearance != mygo.ThemeDark {
			t.Errorf("saved %+v, want the last value and the appearance", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tint was not stored")
	}
	select {
	case v := <-saved:
		t.Errorf("a second write: %+v", v)
	case <-time.After(2 * TintDelay):
	}
}

func TestResetTintPutsBackTheDefaultsOfTheCurrentTheme(t *testing.T) {
	f, saved := tintFrame(t, prefs.Prefs{Appearance: mygo.ThemeDark,
		Tint: prefs.Tints{Dark: prefs.Tint{Window: pf(0.9), Panel: pf(0.9)}, Light: prefs.Tint{Window: pf(0.2)}}})
	f.ResetTint()
	if w, p := f.Tint(); w != 70 || p != 85 {
		t.Errorf("Tint = %v %v, want the Dark defaults 70 85", w, p)
	}
	v := <-saved
	if v.Tint.Dark != (prefs.Tint{}) || v.Tint.Light.Window == nil {
		t.Errorf("saved %+v: Dark must be empty and Light kept", v.Tint)
	}
}
