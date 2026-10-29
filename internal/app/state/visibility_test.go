package state

import "testing"

func TestVisibilitySetNotifiesOncePerChange(t *testing.T) {
	n := 0
	v := NewVisibility(func(bool) { n++ })

	// Starts visible, so visible again is no change.
	v.Set(true)
	if n != 0 {
		t.Fatalf("notified %d times, want 0", n)
	}
	v.Set(false)
	v.Set(false)
	if n != 1 {
		t.Fatalf("notified %d times, want 1", n)
	}
	if v.Visible() {
		t.Fatal("want hidden")
	}
	v.Set(true)
	if n != 2 {
		t.Fatalf("notified %d times, want 2", n)
	}
}

type fakeWindow struct {
	visible                       bool
	show, hide, minimize, restore func()
	offs                          int
}

func (f *fakeWindow) IsVisible() bool { return f.visible }
func (f *fakeWindow) off() func()     { return func() { f.offs++ } }
func (f *fakeWindow) OnShow(fn func()) func() {
	f.show = fn
	return f.off()
}
func (f *fakeWindow) OnHide(fn func()) func() {
	f.hide = fn
	return f.off()
}
func (f *fakeWindow) OnMinimize(fn func()) func() {
	f.minimize = fn
	return f.off()
}
func (f *fakeWindow) OnRestore(fn func()) func() {
	f.restore = fn
	return f.off()
}

func TestVisibilityWatchFollowsTheWindow(t *testing.T) {
	w := &fakeWindow{visible: true}
	v := NewVisibility(nil)
	stop := v.Watch(w)
	if !v.Visible() {
		t.Fatal("want visible at start")
	}

	w.visible = false
	w.hide()
	if v.Visible() {
		t.Fatal("want hidden after hide")
	}
	w.visible = true
	w.show()
	if !v.Visible() {
		t.Fatal("want visible after show")
	}
	w.minimize()
	if v.Visible() {
		t.Fatal("want hidden after minimize")
	}
	w.restore()
	if !v.Visible() {
		t.Fatal("want visible after restore")
	}

	stop()
	if w.offs != 4 {
		t.Fatalf("stop removed %d listeners, want 4", w.offs)
	}
}

func TestVisibilityWatchReadsTheWindowNow(t *testing.T) {
	v := NewVisibility(nil)
	v.Watch(&fakeWindow{visible: false})
	if v.Visible() {
		t.Fatal("want hidden when the window starts hidden")
	}
}
