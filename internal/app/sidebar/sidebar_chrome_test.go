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
		if projectIconInk(p, false, false) != p.Ink88 || projectIconInk(p, true, false) != p.Ink100 ||
			projectIconInk(p, false, true) != p.Ink30 || projectIconInk(p, true, true) != p.Ink100 {
			t.Errorf("%s: project icon inks are not ink-88 / ink-100 / ink-30", name)
		}
	}
}

// No hairline sits over the sidebar foot (D258): the pixel rows between the
// list and the Settings button are all the same.
func TestNoDividerSitsAboveTheSettingsButton(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	tt := h.tt
	tt.Frame()
	b := apptest.Box(t, tt, LabelSettingsOpen)
	img := tt.Image()
	for y := b.Y - 14; y < b.Y; y++ {
		if apptest.PixelAt(img, 100, y) != apptest.PixelAt(img, 100, b.Y-15) {
			t.Errorf("a line at y %v above the Settings button %+v", y, b)
			break
		}
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
