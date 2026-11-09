package app

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
)

var rowsNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// builder is a live worker of the orchestrator parent, in the folder cwd, with
// an issue, the builder profile and the last output ago before rowsNow.
func builder(name, parent, cwd, issue string, ago time.Duration) agents.Worker {
	return agents.Worker{Name: name, Address: name, Role: "builder", Profile: "builder", Parent: parent,
		Issue: issue, Worktree: cwd, Live: true, Status: "working", LastOutput: rowsNow.Add(-ago)}
}

// rowsHarness is the sidebar with agents in project alpha: lead with the
// workers b-241 and b-242, and a source that records the menu actions.
type rowsHarness struct {
	*spacesHarness
	src  *source
	cwds []string
}

func newRows(t *testing.T) *rowsHarness {
	t.Helper()
	h, cwds := twoRepos(t)
	h.list.Now = func() time.Time { return rowsNow }
	h.list.ModelOf = func(p string) string {
		if p == "builder" {
			return "sonnet"
		}
		return "opus"
	}
	src := &source{}
	h.list.Source = src
	h.list.Env.Run = Runner{Go: func(f func()) { f() }}
	lead := orchestratorIn("lead", cwds[0])
	lead.Profile, lead.LastOutput = "orchestrator", rowsNow.Add(-2*time.Minute)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{lead},
		Workers: []agents.Worker{
			builder("b-241", "lead", cwds[0], "241", 6*time.Minute),
			builder("b-242", "lead", cwds[0], "", 90*time.Second),
			spPane("w1:p2", cwds[1], nil),
		}})
	return &rowsHarness{spacesHarness: h, src: src, cwds: cwds}
}

func (h *rowsHarness) y(label string) float32 {
	h.t.Helper()
	return box(h.t, h.tt, label).Y
}

func TestTheOrchestratorComesFirstThenItsWorkersOnTheRail(t *testing.T) {
	h := newRows(t)
	proj, lead := h.y("alpha, main"), h.y("lead, idle, opus")
	b1, b2 := h.y("b-241, working, 241, sonnet"), h.y("b-242, working, sonnet")
	other := h.y("beta, main")
	if !(proj < lead && lead < b1 && b1 < b2 && b2 < other) {
		t.Errorf("order: project %v, lead %v, b-241 %v, b-242 %v, beta %v", proj, lead, b1, b2, other)
	}
	// The workers sit on the rail: indented from the orchestrator row.
	w, o := box(t, h.tt, "b-241, working, 241, sonnet title"), box(t, h.tt, "lead, idle, opus title")
	if w.X < o.X+kit.RailIndent {
		t.Errorf("the worker title %+v is not on the rail of the orchestrator %+v", w, o)
	}
}

func TestAWorkerOfAnotherOrchestratorSitsUnderItsOwn(t *testing.T) {
	h := newRows(t)
	other := orchestratorIn("lead2", h.cwds[0])
	snap := h.live.get()
	snap.Orchestrators = append(snap.Orchestrators, other)
	w := builder("b-300", "lead2", h.cwds[0], "300", time.Minute)
	snap.Workers = append(snap.Workers, w)
	h.send(snap)
	lead2, lead := h.y("lead2, idle, opus"), h.y("lead, idle, opus")
	b300, b242 := h.y("b-300, working, 300, sonnet"), h.y("b-242, working, sonnet")
	if !(lead < b242 && b242 < lead2 && lead2 < b300) {
		t.Errorf("lead %v, b-242 %v, lead2 %v, b-300 %v", lead, b242, lead2, b300)
	}
}

func TestAnEndedWorkerAndAPlainTerminalHaveNoRow(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	ended := builder("b-old", "lead", h.cwds[0], "9", time.Minute)
	ended.Live = false
	snap.Workers = append(snap.Workers, ended)
	snap.Terminals = []agents.Worker{{Name: "shell", Address: "sh", Role: "terminal", Worktree: h.cwds[0], Live: true}}
	h.send(snap)
	if h.tt.HasText("b-old") || h.tt.HasText("shell") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestLineTwoIsNameModelAndAge(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Workers[0].Task = "Agent rows on ui.List"
	h.send(snap)
	for _, want := range []string{"b-241", "sonnet", "6m", "1m", "opus", "2m"} {
		if !h.tt.HasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	// The age sits at the right of its row, inside the sidebar: the margins of
	// the rail do not push it out. "2m" is the orchestrator's, "6m" a worker's.
	for _, age := range []string{"2m", "6m", "1m"} {
		end := box(t, h.tt, age)
		if right := end.X + end.W; right > theme.SidebarWidth-theme.Space6-theme.Space10+1 || right < theme.SidebarWidth-theme.Space6-theme.Space10-8 {
			t.Errorf("age %q ends at %v, want the right padding of the row at %v", age, right, theme.SidebarWidth-theme.Space6-theme.Space10)
		}
	}
	if box(t, h.tt, "b-241").X > box(t, h.tt, "sonnet").X {
		t.Error("the name comes after the model")
	}
}

func TestAWorkerShowsItsTaskOnLineOneAndItsNameOnLineTwo(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Workers[0].Task = "Agent rows on ui.List"
	h.send(snap)
	a := agentLine{w: snap.Workers[0]}
	if titleOf(a, "") != "Agent rows on ui.List" || nameOf(a, "") != "b-241" {
		t.Errorf("line 1 %q, name on line 2 %q", titleOf(a, ""), nameOf(a, ""))
	}
	title, name := box(t, h.tt, "b-241, working, 241, sonnet title"), box(t, h.tt, "b-241")
	if name.Y <= title.Y {
		t.Errorf("the name %+v is not under the title %+v", name, title)
	}
	if o := (agentLine{w: snap.Orchestrators[0], orchestrator: true}); nameOf(o, "") != "" || titleOf(o, "") != "lead" {
		t.Errorf("orchestrator: title %q, name %q", titleOf(o, ""), nameOf(o, ""))
	}
}

func TestAWorkerWithNoTaskShowsItsNameOnceOnLineOne(t *testing.T) {
	h := newRows(t) // b-242 has no task
	n := 0
	for _, s := range h.tt.Texts() {
		if s == "b-242" {
			n++
		}
	}
	if n != 0 { // the name is in the row label only; no text shows it twice
		t.Errorf("name text shown %d times in %q", n, h.tt.Texts())
	}
	if !h.tt.HasText("b-242, working, sonnet title") {
		t.Errorf("line 1 is not the name: %q", h.tt.Texts())
	}
}

func TestAnOrchestratorIsIndentedUnderItsProject(t *testing.T) {
	h := newRows(t)
	proj, lead := box(t, h.tt, "alpha, main text"), box(t, h.tt, "lead, idle, opus title")
	if lead.X < proj.X+kit.OrchestratorIndent {
		t.Errorf("orchestrator title %+v is not indented from the project name %+v", lead, proj)
	}
	w := box(t, h.tt, "b-241, working, 241, sonnet title")
	if w.X < lead.X+kit.RailIndent {
		t.Errorf("the worker title %+v is not on the rail inside the orchestrator %+v", w, lead)
	}
}

func TestAnOrchestratorOfOtherMakeShowsItsOwnModelAndKeepsTheAgeRight(t *testing.T) {
	h := newRows(t)
	h.list.ModelOf = func(p string) string {
		if p == "" {
			return ""
		}
		return "opus"
	}
	snap := h.live.get()
	f := orchestratorIn("fo", h.cwds[0])
	f.Foreign, f.Profile, f.Model, f.LastOutput = true, "", "opus", rowsNow.Add(-5*time.Minute)
	g := orchestratorIn("fo2", h.cwds[0])
	g.Foreign, g.Profile, g.LastOutput = true, "", rowsNow.Add(-7*time.Minute)
	snap.Orchestrators = append(snap.Orchestrators, f, g)
	h.send(snap)
	if !h.tt.HasText("fo, idle, opus") {
		t.Errorf("no model in the label: %q", h.tt.Texts())
	}
	for _, age := range []string{"5m", "7m"} {
		end := box(t, h.tt, age)
		if right := end.X + end.W; right > theme.SidebarWidth-theme.Space6-theme.Space10+1 || right < theme.SidebarWidth-theme.Space6-theme.Space10-8 {
			t.Errorf("age %q ends at %v, not at the right edge", age, right)
		}
	}
	// With no model the age still has line 2 under the title.
	if box(t, h.tt, "7m").Y <= box(t, h.tt, "fo2, idle title").Y {
		t.Error("the age of the row with no model is not under its title")
	}
}

func TestAgeTextMovesInMinutes(t *testing.T) {
	cases := []struct {
		ago  time.Duration
		want string
	}{{5 * time.Second, "now"}, {59 * time.Second, "now"}, {3 * time.Minute, "3m"}, {2*time.Hour + 5*time.Minute, "2h"}, {50 * time.Hour, "50h"}, {-time.Second, ""}}
	for _, c := range cases {
		if got := AgeText(rowsNow, rowsNow.Add(-c.ago)); got != c.want {
			t.Errorf("%v: %q, want %q", c.ago, got, c.want)
		}
	}
	if got := AgeText(rowsNow, time.Time{}); got != "" {
		t.Errorf("zero time: %q", got)
	}
}

func TestANeedsYouAgentSaysSoInItsLabel(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Workers[0].Status = "blocked"
	h.send(snap)
	if !h.has("b-241, needs you, 241, sonnet") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestAClickOnAnAgentRowSelectsItAndTheTabsFollow(t *testing.T) {
	h := newRows(t)
	if err := h.tt.Click("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	sel, ok := h.sel.Current()
	if !ok || sel.Kind != state.KindWorker || sel.ID != "b-241" {
		t.Fatalf("selection %+v", sel)
	}
	var chosen []string
	snap := h.live.get()
	for i := range snap.Workers {
		snap.Workers[i].Project = "alpha"
	}
	snap.Orchestrators[0].Project = "alpha"
	agent := TabAgent(snap, sel, ok, "alpha", "", "")
	for _, tab := range TitleTabs(snap, "alpha", "", agent, sel.ID) {
		if tab.Selected {
			chosen = append(chosen, tab.Address)
		}
	}
	if len(chosen) != 1 || chosen[0] != "b-241" {
		t.Errorf("selected tabs %v, want b-241", chosen)
	}
	h.tt.Click("lead, idle, opus")
	if sel, _ := h.sel.Current(); sel.Kind != state.KindOrchestrator || sel.ID != "lead" {
		t.Errorf("selection %+v, want the orchestrator", sel)
	}
}

func TestInterruptAndStopFromTheContextMenuCallTheSource(t *testing.T) {
	h := newRows(t)
	var told []string
	h.list.Env.Status = func(s string) { told = append(told, s) }
	if err := h.tt.RightClick("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(h.tt.Menu(), "|"); !strings.Contains(got, MenuInterrupt) || !strings.Contains(got, MenuStop) || !strings.Contains(got, MenuOpenPR) {
		t.Fatalf("menu %q", got)
	}
	if err := h.tt.ChooseMenuItem(MenuInterrupt); err != nil {
		t.Fatal(err)
	}
	if len(h.src.signalled) != 1 || h.src.signalled[0] != "b-241" {
		t.Errorf("interrupted %v", h.src.signalled)
	}
	h.tt.RightClick("lead, idle, opus")
	if err := h.tt.ChooseMenuItem(MenuStop); err != nil {
		t.Fatal(err)
	}
	if stopped, _ := h.src.calls(); len(stopped) != 1 || stopped[0] != "lead" {
		t.Errorf("stopped %v", stopped)
	}
	if len(told) != 2 || told[0] != "b-241 interrupted" || told[1] != "lead stopped" {
		t.Errorf("status %q", told)
	}
}

func TestAFailedMenuActionShowsItsError(t *testing.T) {
	h := newRows(t)
	h.src.actionErr = errors.New("hc: gone")
	var told []string
	h.list.Env.Status = func(s string) { told = append(told, s) }
	h.tt.RightClick("b-242, working, sonnet")
	if err := h.tt.ChooseMenuItem(MenuStop); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0] != "hc: gone" {
		t.Errorf("status %q", told)
	}
}

func TestOpenPRFromTheContextMenu(t *testing.T) {
	h := newRows(t)
	// No pull request yet: Open PR is off, and choosing it does nothing.
	h.tt.RightClick("b-241, working, 241, sonnet")
	if err := h.tt.ChooseMenuItem(MenuOpenPR); err == nil {
		t.Error("Open PR chose with no pull request")
	}
	rig := newPRSourceRig(map[string]string{h.cwds[0]: `{"number":307}`})
	rig.w.Sync(liveWorker(h.cwds[0]))
	rig.flush()
	var opened [][2]any
	paneOpened := false
	h.list.Entry = WorkerEntry{PRs: rig.w,
		OpenPR:    func(string, int) { paneOpened = true },
		OpenPRWeb: func(dir string, n int) { opened = append(opened, [2]any{dir, n}) }}
	h.tt.Frame()
	h.tt.RightClick("b-241, working, 241, sonnet")
	if err := h.tt.ChooseMenuItem(MenuOpenPR); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0][0] != h.cwds[0] || opened[0][1] != 307 {
		t.Errorf("opened %v", opened)
	}
	if paneOpened {
		t.Error("Open PR opened the PR pane (D127)")
	}
}

func TestADeadSelectionLeavesWhenTheAgentIsGone(t *testing.T) {
	h := newRows(t)
	h.tt.Click("b-241, working, 241, sonnet")
	snap := h.live.get()
	snap.Workers = snap.Workers[1:]
	h.send(snap)
	if _, ok := h.sel.Current(); ok {
		t.Error("the selection of a gone agent stays")
	}
	h.tt.Click("lead, idle, opus")
	h.send(snap) // the orchestrator is still there
	if _, ok := h.sel.Current(); !ok {
		t.Error("the selection of a live orchestrator left")
	}
}

func TestADragMovesProjectsNotAgentRows(t *testing.T) {
	h := newRows(t)
	s := &projects.Store{}
	h.list.SetProjects(s)
	h.list.OnReordered = func() { h.list.SetProjects(s) }
	// Rows: 0 alpha, 1 lead, 2 b-241, 3 b-242, 4 beta, 5 w1:p2. An agent row
	// does not move its project.
	h.list.Reorder([]int{5}, 0)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("an agent row moved a project: %s", got)
	}
	// beta (row 4) dropped among the agents of alpha goes after alpha.
	h.list.Reorder([]int{4}, 2)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("groups %s", got)
	}
	// beta dropped before alpha.
	h.list.Reorder([]int{4}, 0)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha" {
		t.Errorf("groups %s", got)
	}
}

// ---- The age clock ----

// fakeTimers is a clock that the test drives: it holds the timers started and
// not stopped.
type fakeTimers struct {
	mu     sync.Mutex
	active map[int]func()
	next   int
	made   int
}

func newFakeTimers() *fakeTimers { return &fakeTimers{active: map[int]func(){}} }

func (f *fakeTimers) after(d time.Duration, fn func()) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.next
	f.next++
	f.made++
	f.active[id] = fn
	return func() { f.mu.Lock(); delete(f.active, id); f.mu.Unlock() }
}

func (f *fakeTimers) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.active) }

// fire runs every armed timer once.
func (f *fakeTimers) fire() {
	f.mu.Lock()
	fns := make([]func(), 0, len(f.active))
	for id, fn := range f.active {
		fns = append(fns, fn)
		delete(f.active, id)
	}
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func newClock(f *fakeTimers, ticks *int) *AgeClock {
	return &AgeClock{AfterFunc: f.after, Tick: func() { *ticks++ }}
}

func TestTheClockRunsOnlyWhileVisibleWithAnAgeOnScreen(t *testing.T) {
	f, ticks := newFakeTimers(), 0
	a := newClock(f, &ticks)
	if f.count() != 0 || a.Running() {
		t.Fatal("a timer runs with no age on screen")
	}
	a.SetShown(true)
	if f.count() != 1 || !a.Running() {
		t.Fatalf("no timer with an age on screen: %d", f.count())
	}
	a.SetVisible(false)
	if f.count() != 0 || a.Running() {
		t.Error("the timer runs in a hidden window")
	}
	a.SetVisible(true)
	if f.count() != 1 {
		t.Error("the timer did not return with the window")
	}
	a.SetShown(false)
	if f.count() != 0 {
		t.Error("the timer runs with no age on screen")
	}
}

func TestTheClockTicksOnceAMinuteWithOneTimer(t *testing.T) {
	f, ticks := newFakeTimers(), 0
	a := newClock(f, &ticks)
	for i := 0; i < 5; i++ { // many rows tell the clock; there is still one timer
		a.SetShown(true)
	}
	if f.count() != 1 || f.made != 1 {
		t.Fatalf("%d timers armed, %d made", f.count(), f.made)
	}
	f.fire()
	f.fire()
	if ticks != 2 || f.count() != 1 {
		t.Errorf("ticks %d, timers %d", ticks, f.count())
	}
	a.SetVisible(false)
	f.fire()
	if ticks != 2 {
		t.Errorf("a hidden window ticked: %d", ticks)
	}
}

func TestTheDefaultEveryIsOneMinute(t *testing.T) {
	var got time.Duration
	a := &AgeClock{AfterFunc: func(d time.Duration, fn func()) func() { got = d; return func() {} }}
	a.SetShown(true)
	if got != time.Minute {
		t.Errorf("every %v, want 1m", got)
	}
}

func TestAStoppedTimerThatFiresLateDoesNothing(t *testing.T) {
	var late func()
	ticks := 0
	a := &AgeClock{Tick: func() { ticks++ }, AfterFunc: func(d time.Duration, fn func()) func() { late = fn; return func() {} }}
	a.SetShown(true)
	a.SetShown(false)
	late()
	if ticks != 0 || a.Running() {
		t.Errorf("a stopped timer ticked: %d, running %v", ticks, a.Running())
	}
}

func TestTheSidebarTellsTheClockWhetherAnAgeShows(t *testing.T) {
	f, ticks := newFakeTimers(), 0
	h := newRows(t)
	h.list.Ages = newClock(f, &ticks)
	h.list.Sync()
	if f.count() != 1 {
		t.Fatalf("no timer with ages on screen: %d", f.count())
	}
	// Folding the project hides the orchestrator and its workers; the other
	// project's agent has no output time, so no age shows.
	h.list.Fold(h.group(0).CommonDir)
	if f.count() != 0 {
		t.Error("the timer runs with every age folded away")
	}
	h.list.Fold(h.group(0).CommonDir)
	if f.count() != 1 {
		t.Error("the timer did not return when the ages did")
	}
	snap := h.live.get()
	snap.Orchestrators[0].LastOutput = time.Time{}
	for i := range snap.Workers {
		snap.Workers[i].LastOutput = time.Time{}
	}
	h.send(snap)
	if f.count() != 0 {
		t.Error("the timer runs with no age to show")
	}
}

func TestAnAgeMovesWithTheClockOnARedraw(t *testing.T) {
	h := newRows(t)
	if !h.tt.HasText("6m") {
		t.Fatal("no 6m")
	}
	h.list.Now = func() time.Time { return rowsNow.Add(time.Minute) }
	h.tt.Frame()
	h.send(h.live.get()) // a state change reads the clock again
	if !h.tt.HasText("7m") || h.tt.HasText("6m") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestTheFrameTiesTheClockToTheWindowVisibility(t *testing.T) {
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	defer stop()
	ages := f.SpacesList.Ages
	if ages == nil || ages.Running() {
		t.Fatalf("clock %v: want one that waits for an age", ages)
	}
	ages.SetShown(true)
	if !ages.Running() {
		t.Fatal("the clock does not run with an age on screen")
	}
	w.hide()
	if ages.Running() {
		t.Error("the clock runs in a hidden window")
	}
	w.show()
	if !ages.Running() {
		t.Error("the clock did not return with the window")
	}
}

func TestAttachStartsTheWindowHiddenWithNoClock(t *testing.T) {
	w := &fakeWindow{visible: false}
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(w)
	defer stop()
	f.SpacesList.Ages.SetShown(true)
	if f.SpacesList.Ages.Running() {
		t.Error("the clock runs in a window that started hidden")
	}
}

func TestADragKeepsTheStoredPlaceOfAnEmptyProject(t *testing.T) {
	h := newRows(t)
	alpha, beta, gamma := h.group(0).CommonDir, h.group(1).CommonDir, "/proj/gamma/.git"
	s := &projects.Store{}
	// Stored order: gamma (empty) first, then beta, alpha. It shows last.
	s.AddFolder(gamma, "/proj/gamma")
	s.SetOrder([]string{gamma, beta, alpha})
	h.list.SetProjects(s)
	h.list.OnReordered = func() { h.list.SetProjects(s) }
	h.tt.Frame()
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha,gamma" {
		t.Fatalf("shown %s", got)
	}
	row := -1
	for i, it := range h.list.items() {
		if it.agent == nil && h.list.groups[it.group].CommonDir == alpha {
			row = i
		}
	}
	h.list.Reorder([]int{row}, 0)
	got := s.Order([]string{alpha, beta, gamma})
	if want := []string{gamma, alpha, beta}; !slices.Equal(got, want) {
		t.Errorf("stored order %v, want %v", got, want)
	}
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta,gamma" {
		t.Errorf("shown %s", got)
	}
}

func TestRoleText(t *testing.T) {
	cases := []struct {
		orch    bool
		workers int
		want    string
	}{{false, 0, ""}, {true, 0, "orchestrator"}, {false, 1, "1 worker"}, {true, 3, "3 workers"}}
	for _, c := range cases {
		if got := roleText(c.orch, c.workers); got != c.want {
			t.Errorf("roleText(%v, %d) = %q, want %q", c.orch, c.workers, got, c.want)
		}
	}
}

// Line 2 of an orchestrator row says what it runs; a Claude session that does
// not orchestrate shows no role (D191).
func TestLineTwoOfAnOrchestratorShowsItsRole(t *testing.T) {
	h := newRows(t)
	if !slices.Contains(h.tt.Texts(), "2 workers") {
		t.Errorf("lead with two workers: texts %v", h.tt.Texts())
	}
	solo := orchestratorIn("solo", h.cwds[1])
	solo.Profile = "orchestrator"
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18", Orchestrators: []agents.Worker{solo}})
	for _, s := range h.tt.Texts() {
		if s == "orchestrator" || strings.HasSuffix(s, " workers") {
			t.Errorf("a plain session shows the role %q", s)
		}
	}
	solo.Orchestrating = true
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18", Orchestrators: []agents.Worker{solo}})
	if !slices.Contains(h.tt.Texts(), "orchestrator") {
		t.Errorf("a session that ran /contr:orchestrate: texts %v", h.tt.Texts())
	}
}
