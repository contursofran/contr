package app

// The Appearance setting (D119): System, Dark or Light. StartTheme reads the stored value and
// applies it before the window shows. The Appearance row of Settings changes it with
// SetAppearance, which applies it at once and stores it.

import (
	"sync"

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
// System; a read error shows in the status bar once the frame attaches. Without ThemePlatform
// it only keeps the value.
func (f *Frame) StartTheme() {
	src := mygo.ThemeSystem
	if f.LoadPrefs != nil {
		p, err := f.LoadPrefs()
		if err != nil {
			f.themeErr = err
		} else {
			src = p.Source()
		}
	}
	f.appearance = src
	if f.ThemePlatform != nil {
		f.follower = theme.Follow(f.ThemePlatform, &f.lateWin, src)
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
	save, post := f.SavePrefs, f.post
	if save == nil {
		return
	}
	go func() {
		// Writes go one at a time, and each writes the latest value, so quick clicks end with
		// the last choice on disk.
		f.saveMu.Lock()
		defer f.saveMu.Unlock()
		f.appearanceMu.Lock()
		cur := f.appearance
		f.appearanceMu.Unlock()
		if err := save(prefs.Prefs{Appearance: cur}); err != nil && post != nil {
			post(func() { f.Status.Show(err.Error()) })
		}
	}()
}
