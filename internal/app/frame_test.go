package app

import (
	"context"
	"errors"
	"os"
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
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// newTester builds a frame of 1440x900.
func newTester(t *testing.T) (*Frame, *ui.Tester) {
	t.Helper()
	f := NewFrame()
	return f, ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
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
	_, tt := newTester(t)
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
	if got := box(t, tt, LabelTerminal).W; got != 718 {
		t.Errorf("terminal width %v, want 718 (two 1 px dividers)", got)
	}
	status := box(t, tt, LabelStatusBar)
	if status.H != theme.StatusBarHeight || status.W != theme.WindowWidth {
		t.Errorf("status bar %+v, want 1440 x 26", status)
	}
}

func TestToggleSitsRightOfTheWindowButtons(t *testing.T) {
	_, tt := newTester(t)
	button := box(t, tt, LabelSidebarToggle)
	if button.X != theme.TrafficLightInset {
		t.Errorf("toggle at x %v, want %d", button.X, theme.TrafficLightInset)
	}
	if !absent(tt, LabelSidebarReopen) {
		t.Error("reopen button shows while the sidebar is open")
	}
}

func TestToggleHidesAndReopensTheSidebar(t *testing.T) {
	f, tt := newTester(t)
	if err := tt.Click(LabelSidebarToggle); err != nil {
		t.Fatal(err)
	}
	if f.SidebarOpen() || !absent(tt, LabelSidebar) || !absent(tt, LabelSidebarToggle) {
		t.Error("the sidebar should be hidden")
	}
	if absent(tt, LabelSidebarReopen) {
		t.Error("reopen button should show")
	}
	if term := box(t, tt, LabelTerminal); term.W != 999 || term.X != 0 {
		t.Errorf("terminal %+v, want 999 wide from x 0", term)
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
	if got := box(t, tt, LabelTerminal).W; got != 718 {
		t.Errorf("terminal width %v, want 718 (two 1 px dividers)", got)
	}
}

func TestPanelToggleHidesAndShowsThePanel(t *testing.T) {
	f, tt := newTester(t)
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() || !absent(tt, LabelPanel) {
		t.Error("the panel should be hidden")
	}
	if got := box(t, tt, LabelTerminal).W; got != 1159 {
		t.Errorf("terminal width %v, want 1159", got)
	}
	if err := tt.Click(LabelPanelToggle); err != nil {
		t.Fatal(err)
	}
	if got := box(t, tt, LabelPanel).W; got != theme.PanelWidth {
		t.Errorf("panel width %v, want 440", got)
	}
	if got := box(t, tt, LabelTerminal).W; got != 718 {
		t.Errorf("terminal width %v, want 718 (two 1 px dividers)", got)
	}
}

// MyGo builds the view again for each frame, so the slot builds once per
// redraw. The terminal's own cache comes with the terminal view (M5).
func TestAFrameRedrawDoesNotRenderTheTerminalAgain(t *testing.T) {
	f, _ := newTester(t)
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
// the test runs the queue with flush on its own goroutine, so no agent or
// board goroutine runs view code.
type fakeWindow struct {
	visible, focused bool
	focus, blur      func()
	show, hide       func()
	minimize, restor func()

	// mu guards visible, focused, invalidated and updates: the frame reads
	// and writes them from more than one goroutine.
	mu          sync.Mutex
	invalidated int
	updates     []func()
}

func (w *fakeWindow) IsVisible() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.visible
}
func (w *fakeWindow) IsFocused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.focused
}

// setVisible changes what IsVisible reports. Goroutines of the frame read it.
func (w *fakeWindow) setVisible(v bool) {
	w.mu.Lock()
	w.visible = v
	w.mu.Unlock()
}
func (w *fakeWindow) OnFocus(fn func()) func() {
	w.focus = fn
	return func() { w.focus = nil }
}
func (w *fakeWindow) OnBlur(fn func()) func() {
	w.blur = fn
	return func() { w.blur = nil }
}

// Invalidate counts a redraw. Goroutines of the frame call it, so mu guards
// the count.
func (w *fakeWindow) Invalidate() {
	w.mu.Lock()
	w.invalidated++
	w.mu.Unlock()
}

// redraws is the number of Invalidate calls so far.
func (w *fakeWindow) redraws() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.invalidated
}

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

func TestAttachWiresTheStateToTheWindow(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	if f.Visibility == nil || f.Selection == nil || f.Status == nil {
		t.Fatal("Attach should set the state")
	}
	if !f.Visibility.Visible() {
		t.Error("the window is shown, so Visibility should be")
	}
	w.hide()
	if f.Visibility.Visible() {
		t.Error("Visibility should follow the window to hidden")
	}
	before := w.redraws()
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "a1"})
	f.Status.Show("hello")
	if got := w.redraws(); got != before+2 {
		t.Errorf("redraws %d, want 2 more", got-before)
	}
	stop()
	if w.hide != nil || w.show != nil {
		t.Error("stop should end the watching")
	}
}

// The samplers follow the window: they read while it is shown and stay idle while it is hidden (D9).
func TestAttachTiesTheSamplersToTheWindowVisibility(t *testing.T) {
	reads := make(chan struct{}, 8)
	attach := func(visible bool) (*fakeWindow, func()) {
		w := &fakeWindow{visible: visible}
		f := NewFrame()
		f.Host = host.New(func() (host.Stats, error) { reads <- struct{}{}; return host.Stats{}, nil }, nil)
		f.Usage = usage.NewSampler(usage.Unread, nil)
		return w, f.Attach(w)
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
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
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
	f, _ := newTester(t)
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
	f := NewFrame()
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
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var notify func()
	stopped := 0
	f.StartAgents = func(n func()) (AgentSource, func()) {
		notify = n
		return &source{snap: agentstate.Snapshot{Workers: []agents.Worker{{Name: "b1", Address: "a", Live: true}}}}, func() { stopped++ }
	}
	stop := f.Attach(w)
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
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var asked []string
	f.TerminalSource = func(addr string) ghostty.Source {
		asked = append(asked, addr)
		return idleSource{addr}
	}
	stop := f.Attach(w)
	defer stop()
	if len(asked) != 0 {
		t.Fatalf("a terminal was asked for before a selection: %v", asked)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "t2"})
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "t1"})
	if len(asked) != 2 || asked[0] != "t2" || asked[1] != "t1" {
		t.Errorf("terminals asked = %v, want t2 then t1", asked)
	}
	// A worktree has no hc terminal; the same terminal again does not restart.
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

// ---- Plain terminals: "+", Cmd-T and the plain tab (#246) ----

// termFrame is an agentFrame with a New Terminal action. opened records the
// calls to Open; the runner works at once.
type termFrame struct {
	*Frame
	tt       *ui.Tester
	opened   [][2]string
	openErr  error
	statuses []string
}

func newTermFrame(t *testing.T, snap *agentstate.Snapshot) *termFrame {
	t.Helper()
	f, tt := agentFrame(t, snap)
	tf := &termFrame{Frame: f, tt: tt}
	f.NewTerm = &NewTerminal{
		Open: func(project, dir string) (string, error) {
			tf.opened = append(tf.opened, [2]string{project, dir})
			return "tNew", tf.openErr
		},
		Run:    Runner{Go: func(f func()) { f() }},
		Status: func(s string) { tf.statuses = append(tf.statuses, s) },
		Select: func(addr string) { f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: addr}) },
	}
	return tf
}

func TestPlusTabStartsTerminal(t *testing.T) {
	snap := tabSnap()
	snap.Orchestrators[0].Worktree = "/work/p"
	other := orchW("o2", "other-lead", "q") // another project's orchestrator, listed first
	other.Worktree = "/work/q"
	snap.Orchestrators = append([]agents.Worker{other}, snap.Orchestrators...)
	tf := newTermFrame(t, &snap)
	tf.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"}) // opens project p
	tf.tt.Frame()
	if err := tf.tt.Click(LabelNewTab); err != nil {
		t.Fatal(err)
	}
	if len(tf.opened) != 1 || tf.opened[0] != [2]string{"p", "/work/p"} {
		t.Fatalf("opened %v, want project p in the orchestrator's worktree", tf.opened)
	}
	if sel, _ := tf.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "tNew"}) {
		t.Errorf("selection %+v, want the new terminal", sel)
	}
}

func TestPlusNoProjectUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	snap := agentstate.Snapshot{}
	tf := newTermFrame(t, &snap)
	tf.tt.Frame()
	if absent(tf.tt, LabelNewTab) {
		t.Fatal("the + button must show with no tabs")
	}
	if err := tf.tt.Click(LabelNewTab); err != nil {
		t.Fatal(err)
	}
	if len(tf.opened) != 1 || tf.opened[0] != [2]string{"", home} {
		t.Errorf("opened %v, want no project in the home folder", tf.opened)
	}
}

func TestNoPlusWithoutNewTerminal(t *testing.T) {
	snap := tabSnap()
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	if !absent(tt, LabelNewTab) {
		t.Error("the + button shows with no open action")
	}
}

func TestPlusTabStartsInTheSelectedProject(t *testing.T) {
	snap := tabSnap()
	snap.Workers[2].Worktree = "/work/q"
	tf := newTermFrame(t, &snap)
	tf.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w3"}) // project q, no orchestrator
	tf.tt.Frame()
	tf.newTerminal()
	home, _ := os.UserHomeDir()
	if len(tf.opened) != 1 || tf.opened[0] != [2]string{"q", home} {
		t.Errorf("opened %v, want project q in the home folder", tf.opened)
	}
}

func TestNewTerminalErrorStatus(t *testing.T) {
	snap := tabSnap()
	tf := newTermFrame(t, &snap)
	tf.openErr = errors.New("hc is not running")
	tf.tt.Frame()
	tf.newTerminal()
	if len(tf.statuses) != 1 || tf.statuses[0] != "Terminal: hc is not running" {
		t.Errorf("statuses %q", tf.statuses)
	}
	if _, ok := tf.Selection.Current(); ok {
		t.Error("an error must select nothing")
	}
}

func TestNewTerminalBusy(t *testing.T) {
	var jobs []func()
	n := &NewTerminal{Open: func(string, string) (string, error) { return "t", nil },
		Run: Runner{Go: func(f func()) { jobs = append(jobs, f) }}}
	n.Start("p", "/d")
	n.Start("p", "/d")
	if len(jobs) != 1 {
		t.Fatalf("started %d opens, want 1 while one runs", len(jobs))
	}
	jobs[0]()
	n.Start("p", "/d")
	if len(jobs) != 2 {
		t.Errorf("a new open after the first ended did not start")
	}
}

func TestNewTerminalWithoutOpenDoesNothing(t *testing.T) {
	(&NewTerminal{}).Start("p", "/d")
}

func TestPlainTabSelectsKindTerminal(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{{Name: "term-1", Address: "tp", Project: "p", Role: agents.RoleTerminal, Worktree: "/work/p", Live: true}}
	f, tt := agentFrame(t, &snap)
	tt.Frame()
	label := "$ p, " + workerIcon(snap.Terminals[0]).Label()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "tp"}) {
		t.Errorf("selection %+v, want a terminal", sel)
	}
	tt.Frame()
	if absent(tt, LabelTabActive) {
		t.Error("the plain tab is not underlined while selected")
	}
}

func TestSyncTerminalAttachesAPlainTerminal(t *testing.T) {
	f, w, _, srcs := terminalFrame(t, nil)
	f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	w.flush()
	if f.termAddr != "tp" || f.conn == nil {
		t.Fatalf("termAddr %q, conn set %v, want an attach to tp", f.termAddr, f.conn != nil)
	}
	if srcs["tp"] == nil {
		t.Error("no source made for tp")
	}
}
