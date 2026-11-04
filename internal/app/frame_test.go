package app

import (
	"context"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
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

// fakeWindow is a Window the test drives. Update queues fn, as MyGo does:
// the test runs the queue with flush on its own goroutine, so no herdr or
// board goroutine runs view code.
type fakeWindow struct {
	visible, focused bool
	focus, blur      func()
	show, hide       func()
	minimize, restor func()
	invalidated      int

	mu      sync.Mutex
	updates []func()
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
func (w *fakeWindow) Invalidate() { w.invalidated++ }
func (w *fakeWindow) Update(fn func()) {
	w.mu.Lock()
	w.updates = append(w.updates, fn)
	w.mu.Unlock()
}

// queued is the number of funcs that wait in the Update queue.
func (w *fakeWindow) queued() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.updates)
}

// flush runs the queued funcs on the caller's goroutine, as the UI thread
// runs Window.Update funcs.
func (w *fakeWindow) flush() {
	w.mu.Lock()
	fns := w.updates
	w.updates = nil
	w.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

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

// A herdr change reaches the views only through Window.Update: the herdr
// goroutine queues the sync and does not run it. Before the fix the fake ran
// the sync on the herdr goroutine, which raced SetVisible under -race.
func TestAttachSendsHerdrChangesThroughWindowUpdate(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	deadline := time.Now().Add(2 * time.Second)
	for w.queued() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the herdr change should queue a sync with Window.Update")
		}
		time.Sleep(time.Millisecond)
	}
	// The views stay on this goroutine while the queue runs.
	w.hide()
	w.flush()
	w.show()
	f.SpacesList.Sync()
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

// reviewFrame returns a frame whose Review runs reads at once, with a selection and the
// number of reads started.
func reviewFrame(t *testing.T) (*Frame, *int) {
	t.Helper()
	f, _, _ := newTester(t, notRunning())
	r, reads := newTestReview()
	f.Review = r
	f.Selection = state.NewSelection(f.syncReview)
	return f, reads
}

func TestFrameReviewFollowsSelection(t *testing.T) {
	a, b := reviewScratch(t), reviewScratch(t)
	f, _ := reviewFrame(t)
	f.ToggleReview()
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: a.dir})
	if f.Review.repo.Dir != a.dir {
		t.Fatalf("repo %q, want %q", f.Review.repo.Dir, a.dir)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: b.dir})
	if f.Review.repo.Dir != b.dir {
		t.Fatalf("repo %q, want %q", f.Review.repo.Dir, b.dir)
	}
	f.Selection.Clear()
	if f.Review.repo.Dir != "" {
		t.Fatalf("repo %q after clear, want none", f.Review.repo.Dir)
	}
}

func TestFrameReviewFollowsAgentFolder(t *testing.T) {
	a := reviewScratch(t)
	f, live, _ := newTester(t, notRunning())
	r, _ := newTestReview()
	f.Review = r
	f.Selection = state.NewSelection(f.syncReview)
	live.mu.Lock()
	live.live.Tree = &herdr.HerdrTree{Workspaces: []herdr.Workspace{{Tabs: []herdr.Tab{{
		Panes: []herdr.Pane{{ID: "p1", Cwd: &a.dir}}}}}}}
	live.mu.Unlock()
	f.ToggleReview()
	f.Selection.Select(state.Selected{Kind: state.KindAgent, ID: "p1"})
	if f.Review.repo.Dir != a.dir {
		t.Fatalf("repo %q, want %q", f.Review.repo.Dir, a.dir)
	}
}

func TestFrameReviewHiddenNoReads(t *testing.T) {
	a := reviewScratch(t)
	f, reads := reviewFrame(t)
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: a.dir})
	if *reads != 0 {
		t.Fatalf("%d reads while the view is hidden", *reads)
	}
	f.ToggleReview()
	if *reads == 0 || f.Review.repo.Dir != a.dir {
		t.Fatalf("showing the view did not read; reads %d, repo %q", *reads, f.Review.repo.Dir)
	}
	f.ToggleReview()
	n := *reads
	f.Selection.Clear()
	if *reads != n {
		t.Fatal("a read ran after the view was hidden again")
	}
}

func TestFrameReviewReplacesTerminal(t *testing.T) {
	live := &fakeLive{}
	live.set(herdr.ConnState{Kind: herdr.Connected})
	f := NewFrame(live)
	f.Review, _ = newTestReview()
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	if _, ok := tt.Find(LabelReview); ok {
		t.Fatal("review area shows before the toggle")
	}
	tt.Click(LabelReviewToggle)
	box(t, tt, LabelReview)
	if _, ok := tt.Find(LabelTerminal); ok {
		t.Fatal("terminal area still shows with Review on")
	}
}

func TestAttachStartsAndStopsTheAgentState(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var notify func()
	stopped := 0
	f.StartAgents = func(n func()) (AgentSource, func()) {
		notify = n
		return &source{snap: agentstate.Snapshot{Workers: []agents.Worker{{Name: "b1", Address: "a", Live: true}}}}, func() { stopped++ }
	}
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	if f.Card == nil || f.Worker == nil || f.Needs == nil || f.Needs.Open == nil || f.Slots.Card == nil {
		t.Error("Attach should wire the needs row, the answer card and the worker view")
	}
	if f.AgentsList == nil || len(f.AgentsList.snapshot().Workers) != 1 {
		t.Fatal("the agents list should read the snapshot of StartAgents")
	}
	before := w.queued()
	notify()
	if w.queued() != before+1 {
		t.Error("a notify should queue a sync with Window.Update")
	}
	stop()
	if stopped != 1 {
		t.Errorf("the agent state was stopped %d times, want 1", stopped)
	}
}

// idleSource is a terminal source that never answers, so no link finishes an attach.
type idleSource struct{ addr string }

func (idleSource) Attach(ctx context.Context, _ hc.AttachParams) (ghostty.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (idleSource) Watch(context.Context) (<-chan hc.Event, func(), error) { return nil, func() {}, nil }

func TestTheTerminalFollowsTheSelectedAgent(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var asked []string
	f.TerminalSource = func(addr string) ghostty.Source {
		asked = append(asked, addr)
		return idleSource{addr}
	}
	stop := f.Attach(context.Background(), w, herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock")))
	defer stop()
	if len(asked) != 0 {
		t.Fatalf("a terminal was asked for before a selection: %v", asked)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "t1"})
	if len(asked) != 2 || asked[0] != "t2" || asked[1] != "t1" {
		t.Errorf("terminals asked = %v, want t2 then t1", asked)
	}
	// A herdr pane or a worktree has no hc terminal; the same terminal again does not restart.
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: "/repo"})
	f.syncTerminal()
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t1"})
	if len(asked) != 3 || asked[2] != "t1" {
		t.Errorf("terminals asked = %v, want t1 asked again once after a worktree", asked)
	}
	f.syncTerminal() // the same terminal: no new source, no restart
	if len(asked) != 3 {
		t.Errorf("a sync with the same terminal asked for a new source: %v", asked)
	}
}
