package app

import (
	"context"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/terminal"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// fakeLive is a liveSource the test changes.
type fakeLive struct {
	mu   sync.Mutex
	live herdr.LiveHerdr
}

func (f *fakeLive) Live() herdr.LiveHerdr {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.live
}

func (f *fakeLive) set(s herdr.ConnState) {
	f.mu.Lock()
	f.live = herdr.LiveHerdr{State: s}
	f.mu.Unlock()
}

func notRunning() herdr.ConnState {
	return herdr.ConnState{Kind: herdr.NotRunning, Reason: "no socket"}
}

// newTester builds a frame of 1440x900 over a herdr in state s.
func newTester(t *testing.T, s herdr.ConnState) (*Frame, *fakeLive, *ui.Tester) {
	t.Helper()
	live := &fakeLive{}
	live.set(s)
	f := NewFrame(live)
	return f, live, ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
}

func box(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no element %q; texts %q", label, tt.Texts())
	}
	return r
}

func absent(tt *ui.Tester, label string) bool {
	_, ok := tt.Find(label)
	return !ok
}

func TestFrameMatchesDesignSizes(t *testing.T) {
	_, _, tt := newTester(t, connected())
	sidebar := box(t, tt, LabelSidebar)
	if sidebar.Y != 0 || sidebar.W != theme.SidebarWidth || sidebar.H != theme.WindowHeight-theme.StatusBarHeight {
		t.Errorf("sidebar %+v, want 280 wide from the top to the status bar", sidebar)
	}
	header := box(t, tt, LabelSidebarHeader)
	if header.Y != 0 || header.H != theme.TitleBarHeight {
		t.Errorf("sidebar header %+v, want 40 high at the top", header)
	}
	if got := box(t, tt, LabelTitleBar).H; got != theme.TitleBarHeight {
		t.Errorf("title bar height %v, want 40", got)
	}
	panel := box(t, tt, LabelPanel)
	if panel.W != theme.PanelWidth || panel.X+panel.W != theme.WindowWidth {
		t.Errorf("panel %+v, want 440 wide at the right edge", panel)
	}
	if got := box(t, tt, LabelTerminal).W; got != 720 {
		t.Errorf("terminal width %v, want 720", got)
	}
	status := box(t, tt, LabelStatusBar)
	if status.H != theme.StatusBarHeight || status.W != theme.WindowWidth {
		t.Errorf("status bar %+v, want 1440 x 26", status)
	}
}

func TestToggleSitsRightOfTheWindowButtons(t *testing.T) {
	_, _, tt := newTester(t, connected())
	button := box(t, tt, LabelSidebarToggle)
	if button.X != theme.TrafficLightInset {
		t.Errorf("toggle at x %v, want %d", button.X, theme.TrafficLightInset)
	}
	if !absent(tt, LabelSidebarReopen) {
		t.Error("reopen button shows while the sidebar is open")
	}
}

func TestToggleHidesAndReopensTheSidebar(t *testing.T) {
	f, _, tt := newTester(t, connected())
	if err := tt.Click(LabelSidebarToggle); err != nil {
		t.Fatal(err)
	}
	if f.SidebarOpen() || !absent(tt, LabelSidebar) || !absent(tt, LabelSidebarToggle) {
		t.Error("the sidebar should be hidden")
	}
	if absent(tt, LabelSidebarReopen) {
		t.Error("reopen button should show")
	}
	if term := box(t, tt, LabelTerminal); term.W != 1000 || term.X != 0 {
		t.Errorf("terminal %+v, want 1000 wide from x 0", term)
	}

	if err := tt.Click(LabelSidebarReopen); err != nil {
		t.Fatal(err)
	}
	if !absent(tt, LabelSidebarReopen) {
		t.Error("reopen button should go")
	}
	if got := box(t, tt, LabelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar width %v, want 280", got)
	}
	if got := box(t, tt, LabelTerminal).W; got != 720 {
		t.Errorf("terminal width %v, want 720", got)
	}
}

func TestHerdrLineIsCentredInTheTerminalWhileNotRunning(t *testing.T) {
	_, _, tt := newTester(t, notRunning())
	line, term := box(t, tt, herdr.NotRunningLine), box(t, tt, LabelTerminal)
	if line.X <= term.X || line.X+line.W >= term.X+term.W {
		t.Errorf("line %+v is not inside terminal %+v", line, term)
	}
	dx := float64(line.X + line.W/2 - (term.X + term.W/2))
	dy := float64(line.Y + line.H/2 - (term.Y + term.H/2))
	if math.Abs(dx) >= 1 || math.Abs(dy) >= 1 {
		t.Errorf("line is off centre by (%v, %v)", dx, dy)
	}
}

func TestHerdrLineFollowsTheConnection(t *testing.T) {
	_, live, tt := newTester(t, connected())
	if !tt.HasText("[terminal]") {
		t.Fatalf("connected: texts %q, want the terminal slot", tt.Texts())
	}
	lost := notRunning()
	notRunningLine, _ := lost.Line()
	connectingLine, _ := herdr.ConnState{Kind: herdr.Connecting}.Line()
	step := func(s herdr.ConnState, line string) {
		t.Helper()
		live.set(s)
		tt.Frame()
		if line == "" {
			if !tt.HasText("[terminal]") || tt.HasText(notRunningLine) || tt.HasText(connectingLine) {
				t.Fatalf("state %+v: texts %q, want the terminal and no line", s, tt.Texts())
			}
			return
		}
		if !tt.HasText(line) || tt.HasText("[terminal]") {
			t.Fatalf("state %+v: texts %q, want only the line %q", s, tt.Texts(), line)
		}
	}
	step(lost, notRunningLine)
	step(herdr.ConnState{Kind: herdr.Connecting}, connectingLine)
	step(connected(), "")
}

func TestPanelToggleHidesAndShowsThePanel(t *testing.T) {
	f, _, tt := newTester(t, connected())
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() || !absent(tt, LabelPanel) {
		t.Error("the panel should be hidden")
	}
	if got := box(t, tt, LabelTerminal).W; got != 1160 {
		t.Errorf("terminal width %v, want 1160", got)
	}
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if got := box(t, tt, LabelPanel).W; got != theme.PanelWidth {
		t.Errorf("panel width %v, want 440", got)
	}
	if got := box(t, tt, LabelTerminal).W; got != 720 {
		t.Errorf("terminal width %v, want 720", got)
	}
}

// The Rust frame counted the title bar's herdr commands. The frame has no
// herdr command path: the toggle only changes the layout. M6 adds the tab
// commands and must keep this test.
func TestThePanelToggleSendsNoHerdrCommand(t *testing.T) {
	f, live, tt := newTester(t, connected())
	before := live.Live()
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if live.Live() != before {
		t.Error("the toggle changed the herdr state")
	}
	if f.PanelOpen() {
		t.Error("the toggle should hide the panel")
	}
}

// MyGo builds the view again for each frame, so the slot builds once per
// redraw. The terminal's own cache comes with the terminal view (M5).
func TestAFrameRedrawDoesNotRenderTheTerminalAgain(t *testing.T) {
	f, _, _ := newTester(t, connected())
	builds := 0
	f.Slots.Terminal = func(c *ui.Context) {
		builds++
		ui.Text(c, "[terminal]")
	}
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	base := builds
	tt.Frame()
	if got := builds - base; got != 1 {
		t.Errorf("a redraw built the terminal slot %d times, want 1", got)
	}
}

// fakeWindow is a Window the test drives.
type fakeWindow struct {
	visible, focused bool
	focus, blur      func()
	show, hide       func()
	minimize, restor func()
	invalidated      int
}

func (w *fakeWindow) IsVisible() bool { return w.visible }
func (w *fakeWindow) IsFocused() bool { return w.focused }
func (w *fakeWindow) OnFocus(fn func()) func() {
	w.focus = fn
	return func() { w.focus = nil }
}
func (w *fakeWindow) OnBlur(fn func()) func() {
	w.blur = fn
	return func() { w.blur = nil }
}
func (w *fakeWindow) Invalidate()      { w.invalidated++ }
func (w *fakeWindow) Update(fn func()) { fn() }
func (w *fakeWindow) OnShow(fn func()) func() {
	w.show = fn
	return func() { w.show = nil }
}
func (w *fakeWindow) OnHide(fn func()) func() {
	w.hide = fn
	return func() { w.hide = nil }
}
func (w *fakeWindow) OnMinimize(fn func()) func() {
	w.minimize = fn
	return func() { w.minimize = nil }
}
func (w *fakeWindow) OnRestore(fn func()) func() {
	w.restor = fn
	return func() { w.restor = nil }
}

func TestAttachWiresTheStateToTheWindow(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	if f.Herdr == nil || f.Visibility == nil || f.Selection == nil || f.Status == nil {
		t.Fatal("Attach should set the herdr and the state")
	}
	if !f.Visibility.Visible() {
		t.Error("the window is shown, so Visibility should be")
	}
	w.hide()
	if f.Visibility.Visible() {
		t.Error("Visibility should follow the window to hidden")
	}
	before := w.invalidated
	f.Selection.Select(state.Selected{Kind: state.KindAgent, ID: "a1"})
	f.Status.Show("hello")
	if w.invalidated != before+2 {
		t.Errorf("redraws %d, want 2 more", w.invalidated-before)
	}
	stop()
	if w.hide != nil || w.show != nil {
		t.Error("stop should end the watching")
	}
}

func (f *fakeLive) setLive(l herdr.LiveHerdr) {
	f.mu.Lock()
	f.live = l
	f.mu.Unlock()
}

// The Rust frame kept the commands it sent in a list. The Go frame hands them to Send.
func TestTitleBarClicksReachTheCommandHandlerInOrder(t *testing.T) {
	f, live, tt := newTester(t, connected())
	live.setLive(connectedWith(sampleTree()))
	var sent []TabCommand
	f.Send = func(c TabCommand) { sent = append(sent, c) }
	tt.Frame()
	for _, label := range []string{"docs, blocked", LabelTabNew, LabelSplit} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	want := []TabCommand{{TabFocus, "w1:t2"}, {TabCreate, "w1"}, {TabSplitRight, "w1:p1"}}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent %v, want %v", sent, want)
	}
}

// The samplers follow the window: they read while it is shown and stay idle while it is hidden (D9).
func TestAttachTiesTheSamplersToTheWindowVisibility(t *testing.T) {
	reads := make(chan struct{}, 8)
	attach := func(visible bool) (*fakeWindow, func()) {
		w := &fakeWindow{visible: visible}
		f := NewFrame(nil)
		f.Host = host.New(func() (host.Stats, error) { reads <- struct{}{}; return host.Stats{}, nil }, nil)
		f.Usage = usage.NewSampler(usage.Unread, nil)
		return w, f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	}
	_, stop := attach(false)
	select {
	case <-reads:
		t.Fatal("a hidden window should cause no host reading")
	case <-time.After(50 * time.Millisecond):
	}
	stop()

	w, stop := attach(true)
	defer stop()
	select {
	case <-reads:
	case <-time.After(2 * time.Second):
		t.Fatal("a shown window should cause a host reading")
	}
	w.hide()
	w.show()
	select {
	case <-reads:
	case <-time.After(2 * time.Second):
		t.Fatal("showing the window again should cause a host reading")
	}
}

func TestAttachWiresTheTerminalToTheWindowFocus(t *testing.T) {
	w := &fakeWindow{visible: true, focused: true}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	if f.Slots.Terminal == nil {
		t.Fatal("Attach should put the terminal view in its slot")
	}
	if w.focus == nil || w.blur == nil {
		t.Fatal("Attach should follow the window focus and blur")
	}
	w.blur()
	w.focus()
	stop()
	if w.focus != nil || w.blur != nil {
		t.Error("stop should end the focus watching")
	}
}

func TestTheTerminalGridIsInsideTheTerminalArea(t *testing.T) {
	f, _, tt := newTester(t, connected())
	v := terminal.NewView(terminal.Options{Invalidate: func() {}})
	t.Cleanup(v.Close)
	f.Slots.Terminal = v.Build
	tt.Frame()
	grid, area := box(t, tt, terminal.LabelGrid), box(t, tt, LabelTerminal)
	if grid.X < area.X || grid.Y < area.Y || grid.X+grid.W > area.X+area.W+0.5 || grid.Y+grid.H > area.Y+area.H+0.5 {
		t.Errorf("grid %+v is not inside the terminal area %+v", grid, area)
	}
}
