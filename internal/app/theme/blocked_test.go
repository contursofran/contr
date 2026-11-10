package theme

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// wing is what a sidebar row sits on: the window glass over the ground (D250).
func wing(p *Palette) ui.Color { return Over(p.GlassWindow, p.Ground) }

// The red words of a row ("needs you", the item count) are `blocked-text` on
// the wing, and on `selected-quiet` over it for a selected row (D249). They
// keep 4.5:1 in both themes.
func TestBlockedTextReadsOnTheWing(t *testing.T) {
	for _, p := range []*Palette{Dark, Light} {
		for name, bg := range map[string]ui.Color{
			"wing":           wing(p),
			"selected-quiet": Over(p.SelectedQuiet, wing(p)),
		} {
			if got := contrast(p.BlockedText, bg); got < 4.5 {
				t.Errorf("dark=%v: blocked-text has %.2f:1 on %s, want 4.5:1", p.Dark, got, name)
			}
		}
	}
}

// `selected-quiet` reads the same on the wing in both themes: the step from
// the wing to the fill differs by less than 0.01 in contrast (D249).
func TestSelectedQuietReadsTheSameInBothThemes(t *testing.T) {
	step := func(p *Palette) float64 { return contrast(Over(p.SelectedQuiet, wing(p)), wing(p)) }
	if d, l := step(Dark), step(Light); math.Abs(d-l) > 0.01 {
		t.Errorf("selected-quiet step: Dark %.3f, Light %.3f", d, l)
	}
}
