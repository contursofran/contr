package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

// lastToast is the newest message that the frame queued and no view took yet.
func lastToast(f *Frame) (string, bool) {
	p := f.Toasts.Pending()
	if len(p) == 0 {
		return "", false
	}
	return p[len(p)-1], true
}

func toastFrame() (*Frame, *ui.Tester) {
	f := NewFrame(Deps{})
	f.Toasts = state.NewToasts(nil)
	return f, ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
}

func TestAQueuedMessageShowsAsAToast(t *testing.T) {
	f, tt := toastFrame()
	tt.Frame()
	if slices.Contains(tt.Texts(), "gh: no browser") {
		t.Fatal("a toast shows before a message")
	}
	f.Toasts.Show("gh: no browser")
	tt.Frame()
	tt.Frame()
	if !slices.Contains(tt.Texts(), "gh: no browser") {
		t.Fatalf("no toast in %q", tt.Texts())
	}
	box(t, tt, LabelToastViewport)
	if left := f.Toasts.Pending(); len(left) != 0 {
		t.Errorf("the view left %q in the queue", left)
	}
}

func TestAToastCloses(t *testing.T) {
	f, tt := toastFrame()
	f.Toasts.Show("widths: parse x: bad")
	tt.Frame()
	tt.Frame()
	if err := tt.Click(LabelToastClose); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		tt.Advance(50 * 1e6)
	}
	if slices.Contains(tt.Texts(), "widths: parse x: bad") {
		t.Errorf("the toast stays after Close: %q", tt.Texts())
	}
}

func TestTheFrameHasNoStatusBar(t *testing.T) {
	_, tt := toastFrame()
	tt.Frame()
	for _, label := range []string{"status bar", "status-right", "host-cpu-status", "host-mem-status", "needs row"} {
		if _, ok := tt.Find(label); ok {
			t.Errorf("the v0.2 part %q is back", label)
		}
	}
}
