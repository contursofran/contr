package app

// Tests of the sidebar chrome of the mock-up (#436): project groups, the
// This Mac icon and the divider above the Settings button.

import (
	"testing"

	"github.com/contursofran/contr/internal/projects"

	"github.com/contursofran/contr/internal/app/theme"
)

func TestProjectGroupsHaveAirBetweenThem(t *testing.T) {
	h := newSpaces(t, spTree())
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	s.AddFolder("/proj/delta/.git", "/proj/delta")
	h.list.SetProjects(s)
	h.tt.Frame()
	// Two empty projects, no agent row between: the names are one row height
	// apart, plus the air.
	a, b := box(t, h.tt, "gamma, nothing open text"), box(t, h.tt, "delta, nothing open text")
	if a.Y > b.Y {
		a, b = b, a
	}
	if step := b.Y - a.Y; step < theme.SizeRowHeight+theme.Space12 {
		t.Errorf("the project names are %v apart, want at least %v", step, float32(theme.SizeRowHeight+theme.Space12))
	}
}

func TestFirstProjectHasNoAirAbove(t *testing.T) {
	h, _ := twoRepos(t)
	h.tt.Frame()
	a, list := box(t, h.tt, "alpha, main"), box(t, h.tt, LabelProjectList)
	if a.Y-list.Y > 2 {
		t.Errorf("first project row is %v below the list top", a.Y-list.Y)
	}
}

func TestThisMacIconIsVisible(t *testing.T) {
	h := newSpaces(t, spTree())
	tt := h.tt
	tt.Frame()
	r := box(t, tt, ServerLabel(true))
	img := tt.Image()
	bg := pixelAt(img, r.X+2, r.Y+r.H/2)
	best := 0
	for y := r.Y + 6; y < r.Y+r.H-6; y++ {
		for x := r.X + theme.Space10; x < r.X+theme.Space10+15; x++ {
			if d := absInt(int(pixelAt(img, x, y).R) - int(bg.R)); d > best {
				best = d
			}
		}
	}
	if best < 120 {
		t.Errorf("the This Mac icon is %d above the row background, want a clear ink", best)
	}
}

func TestADividerSitsAboveTheSettingsButton(t *testing.T) {
	h := newSpaces(t, spTree())
	tt := h.tt
	tt.Frame()
	b := box(t, tt, LabelSettingsOpen)
	img := tt.Image()
	// The hairline is a faint fill: a pixel row between the Servers row and the
	// button that differs from the one above it.
	found := false
	for y := b.Y - 14; y < b.Y; y++ {
		if pixelAt(img, 100, y) != pixelAt(img, 100, b.Y-15) {
			found = true
		}
	}
	if !found {
		t.Error("no hairline above the Settings button")
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
