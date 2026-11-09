package sidebar

// Tests of the sidebar chrome of the mock-up (#436): project groups, the
// This Mac icon and the divider above the Settings button.

import (
	"testing"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/projects"

	"github.com/contursofran/contr/internal/app/theme"
)

func TestProjectGroupsHaveAirBetweenThem(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	s.AddFolder("/proj/delta/.git", "/proj/delta")
	h.list.SetProjects(s)
	h.tt.Frame()
	// Two empty projects, no agent row between: the names are one row height
	// apart, plus the air.
	a, b := apptest.Box(t, h.tt, "gamma, nothing open text"), apptest.Box(t, h.tt, "delta, nothing open text")
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
	a, list := apptest.Box(t, h.tt, "alpha, main"), apptest.Box(t, h.tt, LabelProjectList)
	if a.Y-list.Y > 2 {
		t.Errorf("first project row is %v below the list top", a.Y-list.Y)
	}
}

// The This Mac icon sets its own ink: a selected row gives its content the accent text color,
// which was nearly invisible on the flat row (#436). The ink is a token of the palette, so each
// theme shows it.
func TestRowIconInksAreTokens(t *testing.T) {
	for name, p := range map[string]*theme.Palette{"dark": theme.Dark, "light": theme.Light} {
		if serverIconInk(p) != p.Ink100 {
			t.Errorf("%s: This Mac icon is not ink-100", name)
		}
		if projectIconInk(p, false, false) != p.Ink85 || projectIconInk(p, true, false) != p.Ink100 ||
			projectIconInk(p, false, true) != p.Ink42 || projectIconInk(p, true, true) != p.Ink100 {
			t.Errorf("%s: project icon inks are not ink-85 / ink-100 / ink-42", name)
		}
	}
}

func TestADividerSitsAboveTheSettingsButton(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	tt := h.tt
	tt.Frame()
	b := apptest.Box(t, tt, LabelSettingsOpen)
	img := tt.Image()
	// The hairline is a faint fill: a pixel row between the Servers row and the
	// button that differs from the one above it.
	found := false
	for y := b.Y - 14; y < b.Y; y++ {
		if apptest.PixelAt(img, 100, y) != apptest.PixelAt(img, 100, b.Y-15) {
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
