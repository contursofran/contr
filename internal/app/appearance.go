package app

// The Appearance setting (D119): System, Dark or Light. StartTheme reads the stored value and
// applies it before the window shows. The Appearance row of Settings changes it with
// SetAppearance, which applies it at once and stores it.

import (
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/prefs"
)

// lateWindow gives theme.Follow a window before the window exists. Update does nothing until
// Attach sets the window: a change of the macOS appearance in that short time needs no repaint,
// since the first frame has not drawn yet.
type lateWindow struct {
	mu sync.Mutex
	w  interface{ Update(func()) }
}

func (l *lateWindow) set(w interface{ Update(func()) }) {
	l.mu.Lock()
	l.w = w
	l.mu.Unlock()
}

func (l *lateWindow) Update(fn func()) {
	l.mu.Lock()
	w := l.w
	l.mu.Unlock()
	if w != nil {
		w.Update(fn)
	}
}

// StartTheme reads the stored settings and applies the theme source. Call it on the UI thread
// once the app runs, before the window is made. A missing, bad or unknown stored value gives
// System; a read error shows as a toast once the frame attaches. Without Deps.ThemePlatform
// it only keeps the value.
func (f *Frame) StartTheme() {
	src := mygo.ThemeSystem
	if f.deps.Prefs.Load != nil {
		p, err := f.deps.Prefs.Load()
		if err != nil {
			f.themeErr = err
		} else {
			src = p.Source()
			f.tint = p.Tint
			theme.SetTint(true, f.tint.Dark.Window, f.tint.Dark.Panel)
			theme.SetTint(false, f.tint.Light.Window, f.tint.Light.Panel)
		}
	}
	f.appearance = src
	if f.deps.ThemePlatform != nil {
		f.follower = theme.Follow(f.deps.ThemePlatform, &f.lateWin, src)
	}
}

// Appearance returns the theme source in use.
func (f *Frame) Appearance() mygo.ThemeSource { return prefs.Normalize(f.appearance) }

// SetAppearance changes the theme source, applies it at once and stores it. The Appearance row
// calls it, on the UI thread. The write runs on a goroutine.
func (f *Frame) SetAppearance(src mygo.ThemeSource) {
	src = prefs.Normalize(src)
	f.appearanceMu.Lock()
	f.appearance = src
	f.appearanceMu.Unlock()
	if f.follower != nil {
		f.follower.SetSource(src)
	}
	if f.invalidate != nil {
		f.invalidate()
	}
	f.persist(0)
}

// TintDelay is how long the settings file waits after the last slider move
// before it is written.
const TintDelay = 300 * time.Millisecond

// persist writes the settings to the file after delay, on a goroutine. A new call
// replaces a write that waits. Writes go one at a time, and each writes the latest
// values, so quick changes end with the last choice on disk.
func (f *Frame) persist(delay time.Duration) {
	save, post := f.deps.Prefs.Save, f.post
	if save == nil {
		return
	}
	f.appearanceMu.Lock()
	defer f.appearanceMu.Unlock()
	if f.prefsTimer != nil {
		f.prefsTimer.Stop()
	}
	f.prefsTimer = time.AfterFunc(delay, func() {
		f.saveMu.Lock()
		defer f.saveMu.Unlock()
		f.appearanceMu.Lock()
		cur := prefs.Prefs{Appearance: f.appearance, Tint: f.tint}
		f.appearanceMu.Unlock()
		if err := save(cur); err != nil && post != nil {
			post(func() { f.Toasts.Show(err.Error()) })
		}
	})
}

// Tint returns the alpha of the window ground and of the panels, in percent, for the
// current theme (D162).
func (f *Frame) Tint() (window, panel float64) {
	w, p := theme.TintValues()
	return math.Round(w * 100), math.Round(p * 100)
}

// SetTint sets the window and panel alpha, in percent, for the current theme. It applies
// at once and stores the values after a short wait, so a drag writes the file once.
func (f *Frame) SetTint(window, panel float64) {
	w, p := window/100, panel/100
	dark := theme.Current().Dark
	f.storeTint(dark, &w, &p)
	f.persist(TintDelay)
}

// ResetTint puts the token defaults back for the current theme.
func (f *Frame) ResetTint() {
	f.storeTint(theme.Current().Dark, nil, nil)
	f.persist(TintDelay)
}

func (f *Frame) storeTint(dark bool, w, p *float64) {
	t := prefs.Tint{Window: w, Panel: p}
	f.appearanceMu.Lock()
	if dark {
		f.tint.Dark = t
	} else {
		f.tint.Light = t
	}
	f.appearanceMu.Unlock()
	theme.SetTint(dark, w, p)
	if f.invalidate != nil {
		f.invalidate()
	}
}
