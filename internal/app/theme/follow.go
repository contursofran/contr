package theme

import (
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// current is the palette of the theme in use. It starts dark, until Follow
// reads the appearance.
var current atomic.Pointer[Palette]

func init() {
	tintState.dark = true
	current.Store(Dark)
}

// Current returns the palette of the current theme. Views read tokens from
// it; they never keep it across a theme change.
func Current() *Palette { return current.Load() }

// Platform is the part of mygo.Theme that Follow uses. mygo.Theme is one.
type Platform interface {
	SetSource(mygo.ThemeSource)
	IsDark() bool
	OnUpdated(fn func()) (off func())
}

// Window is the part of the window that Follow uses.
type Window interface {
	Update(fn func())
}

// Follower keeps the current palette in step with the theme source and with
// the macOS appearance (D119).
type Follower struct {
	p   Platform
	w   Window
	off func()

	mu  sync.Mutex
	src mygo.ThemeSource
}

// Follow applies src (System, Dark or Light) with Platform.SetSource, sets the
// current palette, and updates it again whenever the appearance changes. The
// update runs through Window.Update, which also repaints the window. Call it
// on the UI thread once the app runs (for example in App.WhenReady): reading
// the appearance needs the running app.
func Follow(p Platform, w Window, src mygo.ThemeSource) *Follower {
	f := &Follower{p: p, w: w}
	f.SetSource(src)
	f.off = p.OnUpdated(func() { w.Update(f.refresh) })
	return f
}

// SetSource changes the source, for the Appearance row of Settings. An empty
// source is System.
func (f *Follower) SetSource(src mygo.ThemeSource) {
	if src == "" {
		src = mygo.ThemeSystem
	}
	f.mu.Lock()
	f.src = src
	f.mu.Unlock()
	f.p.SetSource(src)
	f.refresh()
}

// Stop ends the watching of the appearance.
func (f *Follower) Stop() {
	if f.off != nil {
		f.off()
		f.off = nil
	}
}

// refresh sets the current palette. A forced source decides at once; MyGo
// applies it later and not every backend reports its own change. Only System
// asks the platform.
func (f *Follower) refresh() {
	f.mu.Lock()
	src := f.src
	f.mu.Unlock()
	var dark bool
	switch src {
	case mygo.ThemeDark:
		dark = true
	case mygo.ThemeLight:
	default:
		dark = f.p.IsDark()
	}
	publish(dark)
}

// UITheme returns MyGo's theme for a palette, so MyGo's own widgets match the
// tokens. MyGo has no token for some of its roles; they take the nearest
// grey, since the only hues are blocked and claude.
func UITheme(p *Palette) *ui.Theme {
	return &ui.Theme{
		Dark:           p.Dark,
		Background:     p.GlassWindow,
		Surface:        p.Hover,
		SurfaceHover:   p.HoverStrong,
		SurfacePressed: p.Press,
		Border:         p.InputLine,
		Text:           p.Ink95,
		TextMuted:      p.Ink50,
		Accent:         p.Ink100,
		AccentHover:    p.Ink95,
		AccentPressed:  p.Ink90,
		AccentText:     p.Ground,
		Danger:         p.Blocked,
		Warning:        p.Ink85,
		Success:        p.Ink60,
		Selection:      p.Selected,
		Focus:          p.FocusRing,
		Inverse:        p.Ink100,
		InverseText:    p.Ground,
		Scrollbar:      p.Ink30,
		Radius:         Radius8,
		Spacing:        4,
		FontSize:       StyleBody.Size,
	}
}

// uiCache keeps MyGo's theme of the last palette, so a build that finds the
// same palette does not make a new one.
var uiCache atomic.Pointer[uiEntry]

type uiEntry struct {
	p *Palette
	t *ui.Theme
}

// Apply gives MyGo the theme of the current palette (Context.SetTheme). Call
// it first in the root view, on every build.
func Apply(c *ui.Context) {
	p := Current()
	e := uiCache.Load()
	if e == nil || e.p != p {
		e = &uiEntry{p: p, t: UITheme(p)}
		uiCache.Store(e)
	}
	c.SetTheme(e.t)
}
