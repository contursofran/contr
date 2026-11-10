package apptest

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Box gives the rect of the element with label. It fails the test when there
// is none.
func Box(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no element %q; texts %q", label, tt.Texts())
	}
	return r
}

// Absent reports that no element has label.
func Absent(tt *ui.Tester, label string) bool {
	_, ok := tt.Find(label)
	return !ok
}

// Within fails the test unless the rect named what lies fully inside the rect named in, with
// a quarter DIP for rounding.
func Within(t *testing.T, what string, r ui.Rect, in string, box ui.Rect) {
	t.Helper()
	const e = 0.25
	if r.X < box.X-e || r.Y < box.Y-e || r.X+r.W > box.X+box.W+e || r.Y+r.H > box.Y+box.H+e {
		t.Errorf("%s %+v is not inside %s %+v", what, r, in, box)
	}
}

// Centered fails the test unless the center of r is the center of on, with a quarter DIP for
// rounding.
func Centered(t *testing.T, what string, r ui.Rect, on string, box ui.Rect) {
	t.Helper()
	const e = 0.25
	dx := (r.X + r.W/2) - (box.X + box.W/2)
	dy := (r.Y + r.H/2) - (box.Y + box.H/2)
	if dx < -e || dx > e || dy < -e || dy > e {
		t.Errorf("%s %+v is not centered on %s %+v (off by %v,%v)", what, r, on, box, dx, dy)
	}
}

// ChipOn checks the key chip named chip after a second frame (a chip takes its place from the
// last frame's boxes): it lies inside the element named in and inside the window of w×h. It
// returns the chip's box.
func ChipOn(t *testing.T, tt *ui.Tester, chip, in string, w, h float32) ui.Rect {
	t.Helper()
	tt.Frame()
	r := Box(t, tt, chip)
	Within(t, chip, r, in, Box(t, tt, in))
	Within(t, chip, r, "the window", ui.Rect{W: w, H: h})
	return r
}
