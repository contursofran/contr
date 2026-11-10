package chatview

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/view"
)

// cornerRig is the switch of terminal term in its corner, in a terminal-sized window of width w.
type cornerRig struct {
	k  *Corner
	tt *ui.Tester
}

func newCorner(t *testing.T, w float32) *cornerRig {
	t.Helper()
	ch := New(view.Env{}, t.TempDir(), func(string, func()) (func(), error) { return func() {}, nil })
	r := &cornerRig{k: &Corner{Switch: func() func(c *ui.Context) {
		return func(c *ui.Context) { ch.Switch(c, term) }
	}}}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() { r.k.View(c) })
	}, int(w), 400)
	return r
}

// overlaps tells whether two boxes share an area.
func overlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// TestSwitchPlaceFollowsTheOutlineColumn is D253 (the D237 rule, now for the switch): the switch
// sits alone, top space-8, 10px from the right side; 40px while the outline column's top is above
// the switch's bottom (34px), in a wide and a narrow terminal.
func TestSwitchPlaceFollowsTheOutlineColumn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		width   float32
		top     float32 // the outline column's top
		column  bool    // the outline column shows
		noField bool    // OutlineTop is nil
		right   float32 // the switch's space to the right side
	}{
		{"no outline source", 900, 0, false, true, 10},
		{"no column", 900, 0, false, false, 10},
		{"short column", 900, 180, true, false, 10},
		{"column at the switch's bottom", 900, 34, true, false, 10},
		{"column reaches the switch", 900, 33, true, false, 40},
		{"tall column", 900, -20, true, false, 40},
		{"narrow, short column", 480, 180, true, false, 10},
		{"narrow, tall column", 480, 0, true, false, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCorner(t, tc.width)
			if !tc.noField {
				top, ok := tc.top, tc.column
				r.k.OutlineTop = func() (float32, bool) { return top, ok }
			}
			r.tt.Frame()
			sw := apptest.Box(t, r.tt, LabelSwitch)
			if got := tc.width - (sw.X + sw.W); got != tc.right {
				t.Errorf("switch %+v is %vpx from the right side, want %vpx", sw, got, tc.right)
			}
			if sw.Y != 8 {
				t.Errorf("switch top %v, want 8", sw.Y)
			}
			if sw.Y+sw.H > switchBottom {
				t.Errorf("switch bottom %v is below the row bottom %v of the rule", sw.Y+sw.H, switchBottom)
			}
			if tc.column {
				outline := ui.Rect{X: tc.width - 10 - 20, Y: tc.top, W: 20, H: 400}
				if overlaps(sw, outline) {
					t.Errorf("switch %+v overlaps the outline column %+v", sw, outline)
				}
			}
		})
	}
}

// A terminal with no switch, or a corner with no source, draws nothing.
func TestCornerWithoutASwitchIsEmpty(t *testing.T) {
	r := newCorner(t, 900)
	r.k.Switch = func() func(c *ui.Context) { return nil }
	r.tt.Frame()
	if !apptest.Absent(r.tt, LabelSwitch) {
		t.Error("a nil switch shows")
	}
	r.k.Switch = nil
	r.tt.Frame()
	if !apptest.Absent(r.tt, LabelSwitch) {
		t.Error("a corner with no source shows a switch")
	}
}
