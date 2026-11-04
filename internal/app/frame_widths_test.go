package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
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

func TestDragSidebarDividerChangesWidth(t *testing.T) {
	f, tt := newTester(t)
	drag(tt, theme.SidebarWidth, 340)
	if got := box(t, tt, LabelSidebar).W; got != 340 {
		t.Errorf("sidebar width %v, want 340", got)
	}
	if f.sidebarW != 340 {
		t.Errorf("kept width %v, want 340", f.sidebarW)
	}
	if got := box(t, tt, LabelPanel).W; got != theme.PanelWidth {
		t.Errorf("panel width %v, want it unchanged", got)
	}
}

func TestDragPanelDividerChangesWidth(t *testing.T) {
	f, tt := newTester(t)
	panel := box(t, tt, LabelPanel)
	drag(tt, panel.X-1, panel.X-81)
	if got := box(t, tt, LabelPanel).W; got != theme.PanelWidth+80 {
		t.Errorf("panel width %v, want %d", got, theme.PanelWidth+80)
	}
	if f.panelW != theme.PanelWidth+80 {
		t.Errorf("kept width %v", f.panelW)
	}
}

func TestDraggedWidthsStayInTheirLimits(t *testing.T) {
	f, tt := newTester(t)
	drag(tt, theme.SidebarWidth, 20)
	if got := box(t, tt, LabelSidebar).W; got != theme.SidebarMinWidth {
		t.Errorf("sidebar %v, want the minimum %d", got, theme.SidebarMinWidth)
	}
	drag(tt, theme.SidebarMinWidth, 900)
	if got := box(t, tt, LabelSidebar).W; got != theme.SidebarMaxWidth {
		t.Errorf("sidebar %v, want the maximum %d", got, theme.SidebarMaxWidth)
	}
	panel := box(t, tt, LabelPanel)
	drag(tt, panel.X-1, panel.X-700)
	if got := box(t, tt, LabelPanel).W; got != theme.PanelMaxWidth {
		t.Errorf("panel %v, want the maximum %d", got, theme.PanelMaxWidth)
	}
	panel = box(t, tt, LabelPanel)
	drag(tt, panel.X-1, panel.X+500)
	if got := box(t, tt, LabelPanel).W; got != theme.PanelMinWidth {
		t.Errorf("panel %v, want the minimum %d", got, theme.PanelMinWidth)
	}
	f.sidebarW, f.panelW = 5, 5000
	tt.Frame()
	if f.sidebarW != theme.SidebarMinWidth || f.panelW != theme.PanelMaxWidth {
		t.Errorf("bad widths %v, %v were not clamped", f.sidebarW, f.panelW)
	}
}

func TestHiddenSidesKeepTheirWidths(t *testing.T) {
	_, tt := newTester(t)
	drag(tt, theme.SidebarWidth, 340)
	panel := box(t, tt, LabelPanel)
	drag(tt, panel.X-1, panel.X-61)
	want := box(t, tt, LabelPanel).W
	for _, toggle := range []string{LabelSidebarToggle, LabelPanelToggle} {
		if err := tt.Click(toggle); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click(LabelSidebarReopen); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if got := box(t, tt, LabelSidebar).W; got != 340 {
		t.Errorf("sidebar %v after show, want 340", got)
	}
	if got := box(t, tt, LabelPanel).W; got != want {
		t.Errorf("panel %v after show, want %v", got, want)
	}
}

func TestDividerShowsAResizeCursor(t *testing.T) {
	_, tt := newTester(t)
	tt.Move(theme.SidebarWidth, 400)
	if got := tt.Cursor(); got != ui.CursorResizeEW {
		t.Errorf("cursor %v over the divider, want resize", got)
	}
}

func TestWidthsSaveAfterTheDragEnds(t *testing.T) {
	old := widthsSaveDelay
	widthsSaveDelay = 100 * time.Millisecond
	defer func() { widthsSaveDelay = old }()
	f, tt := newTester(t)
	var mu sync.Mutex
	var saved []widths.Widths
	done := make(chan struct{}, 8)
	f.SaveWidths = func(w widths.Widths) error {
		mu.Lock()
		saved = append(saved, w)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	y := float32(400)
	tt.Move(theme.SidebarWidth, y)
	tt.Press(theme.SidebarWidth, y)
	tt.Move(320, y)
	mu.Lock()
	n := len(saved)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("saved %d times during the drag, want 0", n)
	}
	tt.Release(320, y)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("no save after the drag")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(saved) != 1 || saved[0].Sidebar != 320 || saved[0].Panel != theme.PanelWidth {
		t.Errorf("saved %+v, want one save of 320 and %d", saved, theme.PanelWidth)
	}
}

func TestNoSaverSavesNothing(t *testing.T) {
	_, tt := newTester(t)
	drag(tt, theme.SidebarWidth, 340) // must not panic with SaveWidths nil
	tt.Frame()
}

// attachWithWidths attaches a frame whose loader gives w and err, and waits
// until the load landed.
func attachWithWidths(t *testing.T, w widths.Widths, err error) *Frame {
	t.Helper()
	win := &fakeWindow{visible: true}
	f := NewFrame()
	loaded := make(chan struct{})
	f.LoadWidths = func() (widths.Widths, error) { defer close(loaded); return w, err }
	f.StartAgents = func(func()) (AgentSource, func()) {
		return &source{snap: agentstate.Snapshot{}}, func() {}
	}
	stop := f.Attach(win)
	t.Cleanup(stop)
	<-loaded
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		win.flush()
		if err != nil {
			if _, ok := f.Status.Text(); ok {
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
	if text, _ := f.Status.Text(); text != "widths: parse x: bad" {
		t.Errorf("message %q", text)
	}
	if f.sidebarW != theme.SidebarWidth || f.panelW != theme.PanelWidth {
		t.Errorf("widths %v, %v; want the defaults", f.sidebarW, f.panelW)
	}
}

func TestDividerIsTheDesignLine(t *testing.T) {
	_, tt := newTester(t)
	tt.Move(700, 400) // away from the dividers
	img := tt.Image()
	r, g, b, _ := img.At(theme.SidebarWidth, 400).RGBA()
	wr, wg, wb := uint32(theme.Line>>16)*0x101, uint32(theme.Line>>8&0xff)*0x101, uint32(theme.Line&0xff)*0x101
	if r != wr || g != wg || b != wb {
		t.Errorf("divider pixel %x %x %x, want the line colour %x %x %x", r, g, b, wr, wg, wb)
	}
}
