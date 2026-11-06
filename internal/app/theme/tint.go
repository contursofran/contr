package theme

import (
	"math"
	"sync"
)

// Tint overrides (D162): the owner tunes the alpha of `glass-window` and of
// `panel` for each theme in Settings. A nil value keeps the token default.
// Current returns the palette with the overrides applied, so every view
// follows. The contrast numbers of docs/spec/tokens.md hold for the defaults
// only.

// tintDefaults are the token alphas of Dark and Light (D163), as fractions.
// A test keeps them equal to the palettes.
var tintDefaults = [2]struct{ window, panel float64 }{
	{window: 0.35, panel: 0.60}, // Light
	{window: 0.70, panel: 0.85}, // Dark
}

var tintState struct {
	mu            sync.Mutex
	dark          bool
	window, panel [2]*float64
}

func idx(dark bool) int {
	if dark {
		return 1
	}
	return 0
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Min(1, math.Max(0, v))
}

// publish sets the current palette: the base palette of the theme with the
// overrides of that theme on top.
func publish(dark bool) {
	tintState.mu.Lock()
	defer tintState.mu.Unlock()
	tintState.dark = dark
	i := idx(dark)
	base := For(dark)
	w, p := tintState.window[i], tintState.panel[i]
	if w == nil && p == nil {
		current.Store(base)
		return
	}
	q := *base
	if w != nil {
		q.GlassWindow.A = uint8(math.Round(clamp01(*w) * 255))
	}
	if p != nil {
		q.Panel.A = uint8(math.Round(clamp01(*p) * 255))
	}
	current.Store(&q)
}

// SetTint sets the overrides of one theme and applies them at once when that
// theme is the current one. A nil value goes back to the token default.
func SetTint(dark bool, window, panel *float64) {
	tintState.mu.Lock()
	i := idx(dark)
	tintState.window[i], tintState.panel[i] = copyf(window), copyf(panel)
	cur := tintState.dark
	tintState.mu.Unlock()
	if cur == dark {
		publish(dark)
	}
}

// ClearTints drops every override. Tests call it to leave no state.
func ClearTints() {
	tintState.mu.Lock()
	tintState.window, tintState.panel = [2]*float64{}, [2]*float64{}
	cur := tintState.dark
	tintState.mu.Unlock()
	publish(cur)
}

// TintValues returns the alpha of glass-window and of panel, 0 to 1, that
// the current theme uses: the override, or the token default.
func TintValues() (window, panel float64) {
	tintState.mu.Lock()
	defer tintState.mu.Unlock()
	i := idx(tintState.dark)
	window, panel = tintDefaults[i].window, tintDefaults[i].panel
	if v := tintState.window[i]; v != nil {
		window = clamp01(*v)
	}
	if v := tintState.panel[i]; v != nil {
		panel = clamp01(*v)
	}
	return window, panel
}

func copyf(v *float64) *float64 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
