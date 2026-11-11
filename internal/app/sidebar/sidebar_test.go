package sidebar

import (
	"github.com/egoist/mygo/ui"

	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/projects"
)

// ---- The server row ----

func TestServerRowIsLocalWhenHCIsUp(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.tt.Frame()
	if !h.has("This Mac, local") || !h.tt.HasText("This Mac") || !h.tt.HasText("local") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	for _, text := range h.tt.Texts() {
		if strings.HasPrefix(text, "hc:") {
			t.Errorf("the problem line %q shows while hc is up", text)
		}
	}
	if !h.tt.HasText("Servers") || !h.tt.HasText("Projects") {
		t.Errorf("the heads are missing: %q", h.tt.Texts())
	}
}

func TestServerRowShowsCPUAndMemory(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.host.set(stats(38, 62, 100))
	h.tt.Frame()
	if want := "local · 38% CPU · 62% MEM"; !h.tt.HasText(want) {
		t.Errorf("want %q in %q", want, h.tt.Texts())
	}
}

func TestSettingsButtonIsAtTheBottomRight(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.tt.Frame()
	b, side := apptest.Box(t, h.tt, LabelSettingsOpen), apptest.Box(t, h.tt, LabelNoProjects)
	if win := apptest.Box(t, h.tt, LabelServersHead+" header"); b.X+b.W < win.X+win.W-24 {
		t.Errorf("Settings ends at x %v, not at the right edge %v", b.X+b.W, win.X+win.W)
	}
	if b.Y < side.Y+side.H-1 {
		t.Errorf("Settings at y %v, above the end of the empty project list %v", b.Y, side.Y+side.H)
	}
	head := apptest.Box(t, h.tt, LabelServersHead+" header")
	if b.Y < head.Y+head.H {
		t.Error("Settings still sits in the Servers head")
	}

	h.list.Compact = true
	h.tt.Frame()
	b, side = apptest.Box(t, h.tt, LabelSettingsOpen), apptest.Box(t, h.tt, LabelProjectList)
	if b.Y < side.Y+side.H-1 {
		t.Errorf("icon column: Settings at y %v, above the project list end %v", b.Y, side.Y+side.H)
	}
}

func TestSettingsButtonOpensSettings(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	opened := 0
	h.list.OnSettings = func() { opened++ }
	h.tt.Frame()
	if h.has(LabelSettingsUpdate) {
		t.Error("the update label shows with no update")
	}
	if err := h.tt.Click(LabelSettingsOpen); err != nil || opened != 1 {
		t.Fatalf("click: %v, opened %d", err, opened)
	}
}

func TestSettingsButtonNamesAnUpdateThatWaits(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	ready := false
	h.list.UpdateReady = func() bool { return ready }
	h.tt.Frame()
	if !h.has(LabelSettingsOpen) {
		t.Fatal("no Settings button")
	}
	ready = true
	h.tt.Frame()
	if !h.has(LabelSettingsUpdate) || h.has(LabelSettingsOpen) {
		t.Errorf("the button does not say that an update waits: %q", h.tt.Texts())
	}
}

func TestSettingsButtonNamesSetupStepsLeft(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	steps := 0
	h.list.SetupSteps = func() int { return steps }
	h.tt.Frame()
	if h.has(LabelSettingsSetup) || !h.has(LabelSettingsOpen) {
		t.Fatal("the setup label shows with no step left")
	}
	steps = 2
	h.tt.Frame()
	if !h.has(LabelSettingsSetup) {
		t.Errorf("the button does not say that setup steps are left: %q", h.tt.Texts())
	}
}

func TestServerRowHasNoPlusUntilRemoteServers(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.tt.Frame()
	// The only icon button of the Projects head is the `+` (#436).
	if !h.has(LabelProjectsAdd) {
		t.Errorf("no %q button", LabelProjectsAdd)
	}
	for _, label := range []string{LabelProjectsOpen, LabelProjectsNew} {
		if h.has(label) {
			t.Errorf("a %q button shows in the head, not in the + menu", label)
		}
	}
	if h.has("Add server") {
		t.Error("a + shows in the Servers head")
	}
}

func TestServerRowIsOfflineWithTheProblemLine(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.host.set(stats(38, 62, 100))
	h.send(hcDown())
	if !h.has("This Mac, offline") || !h.tt.HasText("offline") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if !h.tt.HasText("hc: connection refused") {
		t.Errorf("no problem line in %q", h.tt.Texts())
	}
	// The problem line sits 20px from the panel side, as the row content (D231).
	line, side := apptest.Box(t, h.tt, "hc: connection refused"), apptest.Box(t, h.tt, LabelNoProjects)
	if line.X < side.X+19.5 || line.X > side.X+20.5 {
		t.Errorf("problem line at x %v, want %v", line.X, side.X+20)
	}
	for _, text := range h.tt.Texts() {
		if strings.Contains(text, "CPU") {
			t.Errorf("an offline row shows %q", text)
		}
	}
	// The problem goes away with the connection.
	h.send(apptest.SpTree())
	if h.tt.HasText("hc: connection refused") || !h.has("This Mac, local") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestServerRowChangesWithTheProblem(t *testing.T) {
	h := newSpaces(t, hcDown())
	h.send(agentstate.Snapshot{Problem: "hc: no socket"})
	if !h.tt.HasText("hc: no socket") || h.tt.HasText("hc: connection refused") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

// ---- The project rows ----

func TestProjectRowsShowNameAndBranchInStoredOrder(t *testing.T) {
	h, _ := twoRepos(t)
	h.tt.Frame()
	if !h.has("alpha, main") || !h.has("beta, main") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	a, _ := h.tt.Find("alpha, main")
	b, _ := h.tt.Find("beta, main")
	if a.Y >= b.Y {
		t.Errorf("alpha at %v, beta at %v: want tree order", a.Y, b.Y)
	}
	s := &projects.Store{}
	s.SetOrder([]string{h.group(1).CommonDir, h.group(0).CommonDir})
	h.list.SetProjects(s)
	h.tt.Frame()
	a, _ = h.tt.Find("alpha, main")
	b, _ = h.tt.Find("beta, main")
	if b.Y >= a.Y {
		t.Errorf("alpha at %v, beta at %v: want stored order", a.Y, b.Y)
	}
}

// dragRow drags the row with label from onto the top of the row with label to.
func dragRow(h *spacesHarness, from, to string) {
	h.t.Helper()
	f, ok1 := h.tt.Find(from)
	d, ok2 := h.tt.Find(to)
	if !ok1 || !ok2 {
		h.t.Fatalf("rows %q, %q not found in %q", from, to, h.tt.Texts())
	}
	h.tt.Press(f.X+f.W/2, f.Y+f.H/2)
	h.tt.Move(f.X+f.W/2, f.Y)
	h.tt.Move(d.X+d.W/2, d.Y+2)
	h.tt.Release(d.X+d.W/2, d.Y+2)
	h.tt.Frame()
}

func TestADragReordersTheProjectsAndStoresTheOrder(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	h.list.SetProjects(s)
	saved := 0
	h.list.OnReordered = func() { saved++; h.list.SetProjects(s) }
	h.tt.Frame()
	dragRow(h, "beta, main", "alpha, main")
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "beta,alpha" {
		t.Errorf("groups %s, want beta,alpha", got)
	}
	if saved != 1 {
		t.Errorf("OnReordered ran %d times, want 1", saved)
	}
	order := s.Order([]string{h.group(0).CommonDir, h.group(1).CommonDir})
	if len(order) != 2 || filepath.Base(filepath.Dir(order[0])) != "beta" {
		t.Errorf("stored order %v", order)
	}
}

func TestReorderWithoutAStoreDoesNothing(t *testing.T) {
	h, _ := twoRepos(t)
	h.list.Reorder([]int{1}, 0)
	if got := strings.Join(groupNames(h.list.Groups()), ","); got != "alpha,beta" {
		t.Errorf("groups %s", got)
	}
}

func TestAnEmptyProjectShowsDimWithNothingOpen(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	h.list.SetProjects(s)
	h.tt.Frame()
	if !h.has("gamma, nothing open") || !h.tt.HasText("nothing open") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	// Projects with terminals show their branch, not "nothing open".
	if h.has("alpha, nothing open") {
		t.Error("a project with a terminal shows as empty")
	}
}

func TestAProjectWithoutABranchShowsOnlyItsName(t *testing.T) {
	g := spaces.RepoGroup{Name: "detached"}
	if got := ProjectLabel(g); got != "detached" {
		t.Errorf("label %q", got)
	}
}

// ---- Click and fold ----

// orchestratorIn is a live orchestrator in the folder cwd.
func orchestratorIn(name, cwd string) agents.Worker {
	return agents.Worker{Name: name, Address: name, Role: "orchestrator", Worktree: cwd, Live: true}
}

func TestAClickSelectsTheOrchestratorElseTheProject(t *testing.T) {
	h, cwds := twoRepos(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])},
		Workers:       []agents.Worker{apptest.SpPane("w1:p2", cwds[1], nil)}})
	if err := h.tt.Click("alpha, main"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := h.sel.Current(); sel.Kind != state.KindOrchestrator || sel.ID != "lead" {
		t.Errorf("selection %+v, want the orchestrator", sel)
	}
	h.tt.Frame()
	if err := h.tt.Click("beta, main"); err != nil {
		t.Fatal(err)
	}
	sel, _ := h.sel.Current()
	if sel.Kind != state.KindWorktree || sel.ID != projectRoot(h.group(1)) {
		t.Errorf("selection %+v, want the project beta", sel)
	}
}

func TestAClickOnTheSelectedProjectFoldsAndOpensIt(t *testing.T) {
	h, cwds := twoRepos(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])},
		Workers:       []agents.Worker{apptest.SpPane("w1:p2", cwds[1], nil)}})
	dir := h.group(0).CommonDir
	h.tt.Click("alpha, main") // selects
	h.tt.Frame()
	if h.list.Folded(dir) || !h.tt.HasText("lead") {
		t.Fatal("the first click folded the project")
	}
	h.tt.Click("alpha, main") // folds
	h.tt.Frame()
	if !h.list.Folded(dir) || h.tt.HasText("lead") {
		t.Error("the second click did not fold, or its agent still shows")
	}
	if !h.tt.HasText("w1:p2") {
		t.Error("a fold hid the agent of another project")
	}
	h.tt.Click("alpha, main") // opens
	h.tt.Frame()
	if h.list.Folded(dir) || !h.tt.HasText("lead") {
		t.Error("the third click did not open")
	}
}

func TestEmptyProjectMenuStartsAnOrchestratorThere(t *testing.T) {
	h, _ := twoRepos(t)
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	h.list.SetProjects(s)
	var started []string
	h.list.OnNewIn = func(dir string) { started = append(started, dir) }
	h.tt.Frame()
	if _, ok := h.tt.Find(MenuNewOrchestrator); ok {
		t.Fatal("an empty project row shows a + button")
	}
	if err := h.tt.RightClick("gamma, nothing open"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(MenuNewOrchestrator); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(started, []string{"/proj/gamma"}) {
		t.Errorf("started %v", started)
	}
}

func TestThePlusMenuRunsItsActions(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	var opened, created int
	h.list.OnProjects = func() { opened++ }
	h.list.OnNew = func() { created++ }
	h.tt.Frame()
	if err := addItem(h.tt, LabelProjectsOpen); err != nil {
		t.Fatal(err)
	}
	if err := addItem(h.tt, LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || created != 1 {
		t.Errorf("opened %d, created %d", opened, created)
	}
}

// ---- The branch ----

func TestTheBranchIsReadOffTheUIThread(t *testing.T) {
	base := apptest.CanonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &spSnap{s: apptest.SpTree(apptest.SpPane("w1:p1", wt, nil))}
	var started []func()
	read := 0
	l := NewSpacesList(live.get, nil, view.Env{})
	l.Env.Run.Go = func(f func()) { started = append(started, f) }
	scan := l.scanFn
	l.scanFn = func(cwds []string) (s git.Scan) { read++; return scan(cwds) }
	l.Sync()
	if read != 0 {
		t.Fatalf("git was read %d times on the calling thread", read)
	}
	started[0]()
	if read != 1 || l.Groups()[0].Branch != "main" {
		t.Errorf("reads %d, groups %+v", read, l.Groups())
	}
}

func TestAHeadChangeReadsTheBranchAgain(t *testing.T) {
	base := apptest.CanonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, apptest.SpTree(apptest.SpPane("w1:p1", wt, nil)))
	commonDir := h.group(0).CommonDir
	head, ok := h.heads[filepath.Join(commonDir, "HEAD")]
	if !ok {
		t.Fatalf("no watch of HEAD in %v", h.heads)
	}
	h.tt.Frame()
	if !h.has("repo, main") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	apptest.WriteFakeFile(t, filepath.Join(commonDir, "HEAD"), "ref: refs/heads/release\n")
	head.changed()
	h.tt.Frame()
	if !h.has("repo, release") || h.has("repo, main") {
		t.Errorf("texts %q: want the new branch", h.tt.Texts())
	}
	if h.list.scans != 2 {
		t.Errorf("scans %d, want 2", h.list.scans)
	}
}

func TestAHeadChangeReadsGitOnAGoroutine(t *testing.T) {
	base := apptest.CanonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, apptest.SpTree(apptest.SpPane("w1:p1", wt, nil)))
	var started []func()
	h.list.Env.Run.Go = func(f func()) { started = append(started, f) }
	for _, head := range h.heads {
		head.changed()
	}
	if len(started) != 1 || h.list.scans != 2 {
		t.Fatalf("started %d, scans %d: the scan must wait for a goroutine", len(started), h.list.scans)
	}
}

func TestEachRepoIsWatchedOnceAndTheWatchEndsWithTheRepo(t *testing.T) {
	h, _ := twoRepos(t)
	if len(h.heads) != 2 {
		t.Fatalf("watches %d, want 2", len(h.heads))
	}
	h.send(apptest.SpTree(apptest.SpPane("w1:p1", h.list.scanned[0], nil)))
	h.send(apptest.SpTree(apptest.SpPane("w1:p1", h.list.scanned[0], nil)))
	if len(h.heads) != 2 {
		t.Errorf("watches %d: a second sync started more", len(h.heads))
	}
	var ended, live int
	for _, head := range h.heads {
		if head.ctx.Err() != nil {
			ended++
		} else {
			live++
		}
	}
	if ended != 1 || live != 1 {
		t.Errorf("ended %d, live %d: the repo that lost its terminal must stop", ended, live)
	}
}

func TestClosingTheListEndsTheWatches(t *testing.T) {
	h, _ := twoRepos(t)
	h.list.Close()
	for path, head := range h.heads {
		if head.ctx.Err() == nil {
			t.Errorf("watch %s still runs", path)
		}
	}
	before := h.list.scans
	h.list.rescan()
	if h.list.scans != before {
		t.Errorf("scans %d: a closed list scanned", h.list.scans)
	}
}

func TestAFailedWatchIsReported(t *testing.T) {
	base := apptest.CanonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &spSnap{s: apptest.SpTree(apptest.SpPane("w1:p1", wt, nil))}
	l := NewSpacesList(live.get, nil, view.Env{})
	l.Env.Run.Go = func(f func()) { f() }
	l.WatchHead = func(context.Context, string, func()) error { return errors.New("watch limit") }
	var got error
	l.OnError = func(err error) { got = err }
	l.Sync()
	if got == nil || got.Error() != "watch limit" {
		t.Errorf("error %v", got)
	}
}

func TestWatchFileReportsAChangeAndEndsWithItsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "HEAD")
	if err := os.WriteFile(path, []byte("ref: refs/heads/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() { done <- files.WatchFile(ctx, path, func() { changed <- struct{}{} }) }()
	for range 50 {
		_ = os.WriteFile(path, []byte("ref: refs/heads/b\n"), 0o644)
		select {
		case <-changed:
			cancel()
			if err := <-done; err != nil {
				t.Errorf("WatchFile ended with %v", err)
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	cancel()
	t.Fatal("no change reported")
}

// manyRepos is a harness with n projects, one pane each.
func manyRepos(t *testing.T, n int) *spacesHarness {
	t.Helper()
	base := apptest.CanonicalTmp(t)
	var ws []agents.Worker
	for i := 0; i < n; i++ {
		name := "repo" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		gitDir := apptest.MainRepo(t, filepath.Join(base, name), "ref: refs/heads/main\n")
		wt := filepath.Join(base, name+"-wt")
		linkedWorktree(t, gitDir, "w1", wt, "ref: refs/heads/feature\n")
		ws = append(ws, apptest.SpPane("w1:p"+name, wt, nil))
	}
	return newSpaces(t, apptest.SpTree(ws...))
}

// Servers sits under the project list and above Settings, wide and in the
// icon column (D154).
func TestServersSitBelowTheProjectsAndAboveSettings(t *testing.T) {
	h, _ := twoRepos(t)
	h.tt.Frame()
	proj, list := apptest.Box(t, h.tt, LabelProjectsHead+" header"), apptest.Box(t, h.tt, LabelProjectList)
	srv, row := apptest.Box(t, h.tt, LabelServersHead+" header"), apptest.Box(t, h.tt, "This Mac, local text")
	set := apptest.Box(t, h.tt, LabelSettingsOpen)
	if proj.Y >= list.Y || srv.Y < list.Y+list.H-1 || row.Y < srv.Y+srv.H-1 || set.Y < row.Y+row.H-1 {
		t.Errorf("order: projects %v list %v servers %v row %v settings %v", proj, list, srv, row, set)
	}

	h.list.Compact = true
	h.tt.Frame()
	list, row, set = apptest.Box(t, h.tt, LabelProjectList), apptest.Box(t, h.tt, "This Mac, local"), apptest.Box(t, h.tt, LabelSettingsOpen)
	if row.Y < list.Y+list.H-1 || set.Y < row.Y+row.H-1 {
		t.Errorf("icon column: list %v row %v settings %v", list, row, set)
	}
}

// A long project list scrolls inside its own height; Servers and Settings stay visible.
func TestALongProjectListScrollsAndServersStayInView(t *testing.T) {
	h := manyRepos(t, 40)
	h.tt.Frame()
	list, row, set := apptest.Box(t, h.tt, LabelProjectList), apptest.Box(t, h.tt, "This Mac, local text"), apptest.Box(t, h.tt, LabelSettingsOpen)
	if bottom := float32(theme.WindowHeight); row.Y+row.H > bottom || set.Y+set.H > bottom+1 {
		t.Errorf("Servers row %v or Settings %v leave the window (%v)", row, set, bottom)
	}
	if row.Y < list.Y+list.H-1 {
		t.Errorf("Servers row %v overlaps the list %v", row, list)
	}
}

// addItem opens the `+` menu of the Projects head and chooses an item (#436).
func addItem(tt *ui.Tester, item string) error {
	if err := tt.Click(LabelProjectsAdd); err != nil {
		return err
	}
	return tt.Click(item)
}

func TestServerRowClickRetriesWhileOffline(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	retries := 0
	h.list.OnRetry = func() { retries++ }
	h.send(hcDown())
	if err := h.tt.Click("This Mac, offline"); err != nil {
		t.Fatal(err)
	}
	if retries != 1 {
		t.Fatalf("retries = %d after a click on the offline row, want 1", retries)
	}
	h.send(apptest.SpTree())
	_ = h.tt.Click("This Mac, local") // the online row has no click
	if retries != 1 {
		t.Fatalf("retries = %d after a click on the online row, want 1", retries)
	}
}

func TestServerRowWithoutOnRetryIsSafe(t *testing.T) {
	h := newSpaces(t, hcDown())
	h.send(hcDown())
	_ = h.tt.Click("This Mac, offline")
	h.tt.Frame()
	if !h.has("This Mac, offline") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

// With no project the list says so, with the button that opens the projects modal (D255).
func TestEmptyProjectListNamesTheFirstStep(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	opened := 0
	h.list.OnProjects = func() { opened++ }
	h.tt.Frame()
	if !h.tt.HasText(LabelNoProjects) || !h.tt.HasText("⌘O") {
		t.Errorf("texts %q, want %q and ⌘O", h.tt.Texts(), LabelNoProjects)
	}
	// Plain: no fill or border; the one filled button is the terminal's (D255).
	b := apptest.Box(t, h.tt, LabelOpenProject)
	img := h.tt.Image()
	if in, out := apptest.PixelAt(img, b.X+2, b.Y+b.H/2), apptest.PixelAt(img, b.X-4, b.Y+b.H/2); in != out {
		t.Errorf("button edge %v, want the sidebar ground %v (no fill)", in, out)
	}
	if err := h.tt.Click(LabelOpenProject); err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Errorf("Open a project ran OnProjects %d times, want 1", opened)
	}
	// The icon column has its folder button: no text.
	h.list.Compact = true
	h.tt.Frame()
	if h.tt.HasText(LabelNoProjects) {
		t.Error("the icon column shows the empty text")
	}
}

// While the stored projects load or a scan runs, an empty list is not "No projects yet".
func TestEmptyProjectListWaitsForTheProjectsAndTheScan(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.list.HoldLaunch = true
	h.tt.Frame()
	if h.tt.HasText(LabelNoProjects) {
		t.Error("the empty text shows while the stored projects load")
	}
	h.list.HoldLaunch = false
	var land func()
	h.list.Env.Run.Go = func(f func()) { land = f }
	h.list.scanFn = func([]string) git.Scan { return git.Scan{} }
	h.send(apptest.SpTree(apptest.ForeignTerm("t1", "shell", "", t.TempDir())))
	if land == nil {
		t.Fatal("no scan started")
	}
	if h.tt.HasText(LabelNoProjects) {
		t.Error("the empty text shows while a scan runs")
	}
	land()
	if !h.list.settled() {
		t.Error("the scan landed but the list still waits")
	}
}
