package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/widths"
)

// drag presses the mouse on a divider at fromX and moves it to toX.
func drag(tt *ui.Tester, fromX, toX float32) {
	y := float32(400)
	tt.Move(fromX, y)
	tt.Press(fromX, y)
	tt.Move((fromX+toX)/2, y)
	tt.Move(toX, y)
	tt.Release(toX, y)
}

// sidebarDivider and filesDivider are the x of the two dividers. Each sits in the
// gap between two panels: 5px from the panel on its first side.
func sidebarDivider(t *testing.T, tt *ui.Tester) float32 {
	t.Helper()
	r := apptest.Box(t, tt, LabelPanelSidebar)
	return r.X + r.W + 5
}

func filesDivider(t *testing.T, tt *ui.Tester) float32 {
	t.Helper()
	return apptest.Box(t, tt, LabelPanelFiles).X - 5
}

func TestDragSidebarDividerChangesWidth(t *testing.T) {
	f, tt := newTester(t)
	drag(tt, sidebarDivider(t, tt), sidebarDivider(t, tt)+28)
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != 340 {
		t.Errorf("sidebar width %v, want 340", got)
	}
	if f.sidebarW != 340 {
		t.Errorf("kept width %v, want 340", f.sidebarW)
	}
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != theme.PanelWidth {
		t.Errorf("files width %v, want it unchanged", got)
	}
}

func TestDragFilesDividerChangesWidth(t *testing.T) {
	f, tt := newTester(t)
	x := filesDivider(t, tt)
	drag(tt, x, x-80)
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != theme.PanelWidth+80 {
		t.Errorf("files width %v, want %d", got, theme.PanelWidth+80)
	}
	if f.panelW != theme.PanelWidth+80 {
		t.Errorf("kept width %v", f.panelW)
	}
}

func TestDraggedWidthsStayInTheirLimits(t *testing.T) {
	f, tt := newTester(t)
	drag(tt, sidebarDivider(t, tt), 20)
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != theme.SidebarMinWidth {
		t.Errorf("sidebar %v, want the minimum %d", got, theme.SidebarMinWidth)
	}
	drag(tt, sidebarDivider(t, tt), 900)
	if got := f.sidebarW; got != theme.SidebarMaxWidth {
		t.Errorf("sidebar %v, want the maximum %d", got, theme.SidebarMaxWidth)
	}
	f.sidebarW = theme.SidebarWidth
	tt.Frame()
	drag(tt, filesDivider(t, tt), 100)
	if got := f.panelW; got != theme.PanelMaxWidth {
		t.Errorf("files %v, want the maximum %d", got, theme.PanelMaxWidth)
	}
	f.sidebarW, f.panelW = 5, 5000
	tt.Frame()
	if f.sidebarW != theme.SidebarMinWidth || f.panelW != theme.PanelMaxWidth {
		t.Errorf("bad widths %v, %v were not clamped", f.sidebarW, f.panelW)
	}
}

func TestHiddenSidesKeepTheirWidths(t *testing.T) {
	f, tt := newTester(t)
	drag(tt, sidebarDivider(t, tt), sidebarDivider(t, tt)+28)
	x := filesDivider(t, tt)
	drag(tt, x, x-60)
	want := apptest.Box(t, tt, LabelPanelFiles).W
	f.ToggleSidebar()
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	f.ToggleSidebar()
	tt.Frame()
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	tt.Advance(theme.MotionPanel) // the panels come in (D263)
	tt.Frame()
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != 340 {
		t.Errorf("sidebar %v after show, want 340", got)
	}
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != want {
		t.Errorf("files %v after show, want %v", got, want)
	}
}

func TestDividerShowsAResizeCursor(t *testing.T) {
	_, tt := newTester(t)
	tt.Move(sidebarDivider(t, tt), 400)
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Errorf("cursor %v over the divider, want resize", got)
	}
}

// fakeTimer is a widths save timer that fires only when the test calls fire.
type fakeTimer struct {
	d       time.Duration
	fn      func()
	stopped bool
}

func (ft *fakeTimer) Stop() bool {
	was := !ft.stopped
	ft.stopped = true
	return was
}

// fakeWidthsTimers replaces widthsAfterFunc for the test and returns the
// timers it started, in order. No timer fires by itself.
func fakeWidthsTimers(t *testing.T) *[]*fakeTimer {
	t.Helper()
	var timers []*fakeTimer
	old := widthsAfterFunc
	widthsAfterFunc = func(d time.Duration, fn func()) stopper {
		ft := &fakeTimer{d: d, fn: fn}
		timers = append(timers, ft)
		return ft
	}
	t.Cleanup(func() { widthsAfterFunc = old })
	return &timers
}

// live returns the timers that were not stopped.
func live(timers []*fakeTimer) []*fakeTimer {
	var out []*fakeTimer
	for _, ft := range timers {
		if !ft.stopped {
			out = append(out, ft)
		}
	}
	return out
}

// Each move of a drag restarts the wait, so the widths save once, after the
// drag ends (D111). The timers are fake: no part of the test waits on wall time.
func TestWidthsSaveAfterTheDragEnds(t *testing.T) {
	timers := fakeWidthsTimers(t)
	f, tt := newTester(t)
	var saved []widths.Widths
	f.deps.Widths.Save = func(w widths.Widths) error {
		saved = append(saved, w)
		return nil
	}
	y := float32(400)
	x := sidebarDivider(t, tt)
	tt.Move(x, y)
	tt.Press(x, y)
	tt.Move(x+4, y)
	first := len(*timers)
	if first == 0 {
		t.Fatal("a move started no save timer")
	}
	tt.Move(x+8, y)
	if len(*timers) <= first {
		t.Fatal("a second move started no new save timer")
	}
	for _, ft := range (*timers)[:first] {
		if !ft.stopped {
			t.Error("a later move did not stop the wait of an earlier move")
		}
	}
	tt.Release(x+8, y)
	if len(saved) != 0 {
		t.Fatalf("saved %d times during the drag, want 0", len(saved))
	}
	wait := live(*timers)
	if len(wait) != 1 {
		t.Fatalf("%d save timers wait after the drag, want 1", len(wait))
	}
	if wait[0].d != widthsSaveDelay {
		t.Errorf("save delay %v, want %v", wait[0].d, widthsSaveDelay)
	}
	wait[0].fn()
	if len(saved) != 1 || saved[0].Sidebar != 320 || saved[0].Panel != theme.PanelWidth {
		t.Errorf("saved %+v, want one save of 320 and %d", saved, theme.PanelWidth)
	}
}

// With no saver, a drag starts no save timer.
func TestNoSaverSavesNothing(t *testing.T) {
	timers := fakeWidthsTimers(t)
	_, tt := newTester(t)
	drag(tt, sidebarDivider(t, tt), 340) // must not panic with SaveWidths nil
	tt.Frame()
	if len(*timers) != 0 {
		t.Errorf("%d save timers with no saver, want 0", len(*timers))
	}
}

// attachWithWidths attaches a frame whose loader gives w and err, and waits
// until the load landed.
func attachWithWidths(t *testing.T, w widths.Widths, err error) *Frame {
	t.Helper()
	win := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	loaded := make(chan struct{})
	f.deps.Widths.Load = func() (widths.Widths, error) { defer close(loaded); return w, err }
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) {
		return &source{Snap: agentstate.Snapshot{}}, func() {}
	}
	stop := f.Attach(win)
	t.Cleanup(stop)
	<-loaded
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		win.flush()
		if err != nil {
			if _, ok := lastToast(f); ok {
				break
			}
		} else if f.sidebarW != theme.SidebarWidth || f.panelW != theme.PanelWidth {
			break
		} else if w == (widths.Widths{}) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return f
}

func TestLoadedWidthsApplyWithLimits(t *testing.T) {
	f := attachWithWidths(t, widths.Widths{Sidebar: 9999, Panel: 360}, nil)
	if f.sidebarW != theme.SidebarMaxWidth || f.panelW != 360 {
		t.Errorf("widths %v, %v; want %d and 360", f.sidebarW, f.panelW, theme.SidebarMaxWidth)
	}
}

func TestMissingWidthsGiveTheDefaults(t *testing.T) {
	f := attachWithWidths(t, widths.Widths{}, nil)
	if f.sidebarW != theme.SidebarWidth || f.panelW != theme.PanelWidth {
		t.Errorf("widths %v, %v; want the defaults", f.sidebarW, f.panelW)
	}
}

func TestBadWidthsFileShowsAnErrorAndKeepsDefaults(t *testing.T) {
	f := attachWithWidths(t, widths.Widths{}, errors.New("widths: parse x: bad"))
	if text, _ := lastToast(f); text != "widths: parse x: bad" {
		t.Errorf("message %q", text)
	}
	if f.sidebarW != theme.SidebarWidth || f.panelW != theme.PanelWidth {
		t.Errorf("widths %v, %v; want the defaults", f.sidebarW, f.panelW)
	}
}
