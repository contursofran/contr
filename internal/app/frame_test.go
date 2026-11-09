package app

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/reviewview"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/review"
	"github.com/contursofran/contr/internal/usage"
)

// newTester builds a frame of 1440x900.
// newTestReview returns a Review view that runs reads at once, and the number
// of reads started.
func newTestReview() (*reviewview.Review, *int) {
	reads := 0
	r := reviewview.NewReview(view.Env{})
	r.Env.Run.Go = func(f func()) { reads++; f() }
	return r, &reads
}

func newTester(t *testing.T) (*Frame, *ui.Tester) {
	t.Helper()
	f := NewFrame(Deps{})
	return f, ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
}

func TestFrameMatchesDesignSizes(t *testing.T) {
	_, tt := newTester(t)
	bar := apptest.Box(t, tt, LabelTitleBar)
	if bar.X != 0 || bar.Y != 0 || bar.W != theme.WindowWidth || bar.H != 46 {
		t.Errorf("title bar %+v, want 46 high at the top over the full width", bar)
	}
	// 8px between all panels and the window edge (docs/spec/Window.md).
	sidebar := apptest.Box(t, tt, LabelPanelSidebar)
	if sidebar.X != 8 || sidebar.Y != 46 || sidebar.W != 312 {
		t.Errorf("sidebar %+v, want 312 wide at x 8, right under the title bar (D177)", sidebar)
	}
	files := apptest.Box(t, tt, LabelPanelFiles)
	if files.W != 280 || files.X+files.W != theme.WindowWidth-8 {
		t.Errorf("files %+v, want 280 wide, 8 from the right edge", files)
	}
	term := apptest.Box(t, tt, LabelPanelTerminal)
	if term.X != sidebar.X+sidebar.W+8 || term.X+term.W != files.X-8 {
		t.Errorf("terminal %+v, want 8 from the sidebar and 8 from the files", term)
	}
	for _, r := range []ui.Rect{sidebar, term, files} {
		if r.Y+r.H != theme.WindowHeight-8 {
			t.Errorf("panel %+v should end 8 above the window edge (no footer, D126)", r)
		}
	}
	// The tabs start above the terminal panel.
	if tabs := apptest.Box(t, tt, titlebar.LabelTabs); tabs.X < term.X {
		t.Errorf("tabs at %v, left of the terminal panel at %v", tabs.X, term.X)
	}
}

func TestTitleBarHasNoSidebarToggle(t *testing.T) {
	f, tt := newTester(t)
	for _, label := range []string{"Toggle sidebar", "Show sidebar"} {
		if !apptest.Absent(tt, label) {
			t.Errorf("the title bar has a %q button; the mock has none", label)
		}
	}
	// The corner is as wide as the sidebar and its gaps, so the tabs start at the terminal.
	if tabs, term := apptest.Box(t, tt, titlebar.LabelTabs), apptest.Box(t, tt, LabelPanelTerminal); tabs.X < term.X {
		t.Errorf("tabs at %v, left of the terminal panel at %v", tabs.X, term.X)
	}
	f.ToggleSidebar()
	tt.Frame()
	if !apptest.Absent(tt, LabelPanelSidebar) {
		t.Error("the sidebar should be hidden")
	}
	if apptest.Absent(tt, LabelTitleBar) || !apptest.Absent(tt, "Show sidebar") {
		t.Error("a hidden sidebar must not bring a title bar button")
	}
	// The tabs keep clear of the window buttons.
	if tabs := apptest.Box(t, tt, titlebar.LabelTabs); tabs.X < theme.TrafficLightInset {
		t.Errorf("tabs at %v, over the window buttons (%d)", tabs.X, theme.TrafficLightInset)
	}
	if term := apptest.Box(t, tt, LabelPanelTerminal); term.W != 1136 || term.X != 8 {
		t.Errorf("terminal %+v, want 1136 wide from x 8", term)
	}
	f.ToggleSidebar()
	tt.Frame()
	if got := apptest.Box(t, tt, LabelPanelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar width %v, want 312", got)
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got != 816 {
		t.Errorf("terminal width %v, want 816", got)
	}
}

func TestPanelToggleHidesAndShowsThePanel(t *testing.T) {
	f, tt := newTester(t)
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() || !apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the files panel should be hidden")
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got != 1104 {
		t.Errorf("terminal width %v, want 1104", got)
	}
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	if got := apptest.Box(t, tt, LabelPanelFiles).W; got != theme.PanelWidth {
		t.Errorf("files width %v, want 280", got)
	}
	if got := apptest.Box(t, tt, LabelPanelTerminal).W; got != 816 {
		t.Errorf("terminal width %v, want 816", got)
	}
}

// The shortcut does what the button does (docs/spec/Window.md).
func TestCmdShiftEHidesAndShowsTheFilesPanel(t *testing.T) {
	f, tt := newTester(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	if f.PanelOpen() || !apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the key should hide the files panel")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyE)
	if !f.PanelOpen() || apptest.Absent(tt, LabelPanelFiles) {
		t.Error("the key should show the files panel again")
	}
}

func TestPanelsHaveNamesForScreenReaders(t *testing.T) {
	_, tt := newTester(t)
	for _, name := range []string{"Sidebar", "Terminal", "Files"} {
		apptest.Box(t, tt, name)
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
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	if f.Visibility == nil || f.Selection == nil || f.Toasts == nil {
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
	f.Toasts.Show("hello")
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
		f := NewFrame(Deps{})
		f.Host = host.New(func() (host.Stats, error) { reads <- struct{}{}; return host.Stats{}, nil }, theme.MemBarWidth, nil)
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
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
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
	a, b := apptest.NewRepo(t), apptest.NewRepo(t)
	f, _ := reviewFrame(t)
	f.ToggleReview()
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: a.Dir})
	if f.Review.Dir() != a.Dir {
		t.Fatalf("repo %q, want %q", f.Review.Dir(), a.Dir)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: b.Dir})
	if f.Review.Dir() != b.Dir {
		t.Fatalf("repo %q, want %q", f.Review.Dir(), b.Dir)
	}
	f.Selection.Clear()
	if f.Review.Dir() != "" {
		t.Fatalf("repo %q after clear, want none", f.Review.Dir())
	}
}

func TestFrameReviewHiddenNoReads(t *testing.T) {
	a := apptest.NewRepo(t)
	f, reads := reviewFrame(t)
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: a.Dir})
	if *reads != 0 {
		t.Fatalf("%d reads while the view is hidden", *reads)
	}
	f.ToggleReview()
	if *reads == 0 || f.Review.Dir() != a.Dir {
		t.Fatalf("showing the view did not read; reads %d, repo %q", *reads, f.Review.Dir())
	}
	f.ToggleReview()
	n := *reads
	f.Selection.Clear()
	if *reads != n {
		t.Fatal("a read ran after the view was hidden again")
	}
}

func TestFrameReviewReplacesTerminal(t *testing.T) {
	f := NewFrame(Deps{})
	f.Review, _ = newTestReview()
	f.Diff = files.ReadyDiff(review.Summary{Added: 3, Removed: 1, Files: 2})
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	if _, ok := tt.Find(LabelReview); ok {
		t.Fatal("review area shows before the toggle")
	}
	tt.Click(kit.DiffSummaryLabel(kit.DiffSummaryOpts{Added: 3, Removed: 1, Files: 2}))
	apptest.Box(t, tt, LabelReview)
	if _, ok := tt.Find(LabelTerminal); ok {
		t.Fatal("terminal area still shows with Review on")
	}
}

func TestAttachStartsAndStopsTheAgentState(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var notify func()
	stopped := 0
	f.deps.StartAgents = func(n func()) (agentstate.AgentSource, func()) {
		notify = n
		return &source{Snap: agentstate.Snapshot{Workers: []agents.Worker{{Name: "b1", Address: "a", Live: true}}}}, func() { stopped++ }
	}
	stop := f.Attach(w)
	if f.Card == nil {
		t.Error("Attach should wire the questions card")
	}
	if f.SpacesList == nil || len(f.SpacesList.Snapshot().Workers) != 1 {
		t.Fatal("the sidebar list should read the snapshot of StartAgents")
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

func TestAttachTiesTheAgentSourceToTheWindow(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &source{}
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) { return src, func() {} }
	stop := f.Attach(w)
	defer stop()
	last := func() []bool {
		src.Mu.Lock()
		defer src.Mu.Unlock()
		return slices.Clone(src.Visible)
	}
	if v := last(); !slices.Equal(v, []bool{true}) {
		t.Fatalf("visible = %v after Attach, want [true]", v)
	}
	w.hide()
	if v := last(); len(v) == 0 || v[len(v)-1] {
		t.Fatalf("visible = %v after hide, want the last false", v)
	}
	w.show()
	if v := last(); len(v) == 0 || !v[len(v)-1] {
		t.Fatalf("visible = %v after show, want the last true", v)
	}
	if f.SpacesList.OnRetry == nil {
		t.Fatal("the This Mac row has no retry")
	}
	f.SpacesList.OnRetry()
	src.Mu.Lock()
	defer src.Mu.Unlock()
	if src.Reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", src.Reconnects)
	}
}

func TestAttachTellsAHiddenWindowToTheAgentSource(t *testing.T) {
	w := &fakeWindow{visible: false}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &source{}
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) { return src, func() {} }
	stop := f.Attach(w)
	defer stop()
	src.Mu.Lock()
	defer src.Mu.Unlock()
	if len(src.Visible) == 0 || src.Visible[0] {
		t.Fatalf("visible = %v, want the first value false", src.Visible)
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
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	var asked []string
	f.deps.TerminalSource = func(addr string) ghostty.Source {
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
		Env:    view.Env{Status: func(s string) { tf.statuses = append(tf.statuses, s) }},
		Select: func(addr string) { f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: addr}) },
	}
	return tf
}

func TestPlusTabStartsTerminal(t *testing.T) {
	snap := tabSnap()
	snap.Orchestrators[0].Worktree = "/work/p"
	other := apptest.OrchW("o2", "other-lead", "q") // another project's orchestrator, listed first
	other.Worktree = "/work/q"
	snap.Orchestrators = append([]agents.Worker{other}, snap.Orchestrators...)
	tf := newTermFrame(t, &snap)
	tf.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"}) // opens project p
	tf.tt.Frame()
	if err := tf.tt.Click(titlebar.LabelNewTab); err != nil {
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
	if apptest.Absent(tf.tt, titlebar.LabelNewTab) {
		t.Fatal("the + button must show with no tabs")
	}
	if err := tf.tt.Click(titlebar.LabelNewTab); err != nil {
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
	if !apptest.Absent(tt, titlebar.LabelNewTab) {
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
		Env: view.Env{Run: view.Runner{Go: func(f func()) { jobs = append(jobs, f) }}}}
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
	if err := tt.Click("$ p"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "tp"}) {
		t.Errorf("selection %+v, want a terminal", sel)
	}
	tt.Frame()
	if apptest.Absent(tt, titlebar.LabelCloseTab) {
		t.Error("the selected plain tab has no close button")
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

// TestPanelGapsAreEqual: the gap between two panels, between a panel and the window
// edge (left, right, bottom) and between the title bar and the panels is PanelGap (D168).
func TestPanelGapsAreEqual(t *testing.T) {
	_, tt := newTester(t)
	bar := apptest.Box(t, tt, LabelTitleBar)
	sb, tm, fl := apptest.Box(t, tt, LabelPanelSidebar), apptest.Box(t, tt, LabelPanelTerminal), apptest.Box(t, tt, LabelPanelFiles)
	gap := float32(theme.PanelGap)
	// The 30pt tabs center in the title bar; the panels start PanelGap below them (D177).
	tabs := bar.Y + (bar.H+theme.SizeRowHeight)/2
	for name, got := range map[string]float32{
		"left edge to sidebar":   sb.X,
		"sidebar to terminal":    tm.X - (sb.X + sb.W),
		"terminal to files":      fl.X - (tm.X + tm.W),
		"files to right edge":    theme.WindowWidth - (fl.X + fl.W),
		"window top to tab row":  tabs - theme.SizeRowHeight,
		"tab row to terminal":    tm.Y - tabs,
		"panels to bottom edge":  theme.WindowHeight - (tm.Y + tm.H),
		"tab row to sidebar":     sb.Y - tabs,
		"tab row to files":       fl.Y - tabs,
		"sidebar to bottom edge": theme.WindowHeight - (sb.Y + sb.H),
		"files to bottom edge":   theme.WindowHeight - (fl.Y + fl.H),
	} {
		if got != gap {
			t.Errorf("%s = %v, want %v", name, got, gap)
		}
	}
}
