package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/review"
)

// The state of the toggle is the frame's, and MyGo builds the view for each frame, so this
// checks that the toggle follows the state and keeps its place.
func TestThePanelToggleFollowsTheFrameAndKeepsItsPlace(t *testing.T) {
	f, tt := newTester(t)
	if err := tt.Click(titlebar.LabelFilesToggle); err != nil {
		t.Fatal(err)
	}
	if f.PanelOpen() {
		t.Error("the toggle should close the panel")
	}
	if got := apptest.Box(t, tt, titlebar.LabelFilesToggle).X + theme.SizeIconButton; got != theme.WindowWidth-theme.TitleBarPadEnd {
		t.Errorf("toggle right edge %v, want %d", got, theme.WindowWidth-theme.TitleBarPadEnd)
	}
}

// agentFrame is a frame whose agents come from snap, with a selection. As in [Frame.Attach], the
// frame takes the snapshot now and a selection change computes the tabs again; after a change
// of snap, a test calls [Frame.syncAgents] as the change notify does.
func agentFrame(t *testing.T, snap *agentstate.Snapshot) (*Frame, *ui.Tester) {
	t.Helper()
	f, tt := newTester(t)
	f.Selection = state.NewSelection(func() { f.syncOpen() })
	f.agentSnap = func() agentstate.Snapshot { return *snap }
	f.takeSnapshot()
	return f, tt
}

// tabSnap is project p with the orchestrator lead and the workers b1 (working) and b2 (idle),
// and project q with the worker b3.
func tabSnap() agentstate.Snapshot {
	w1 := apptest.WorkW("w1", "b1", "p")
	w1.Status = "working"
	w2 := apptest.WorkW("w2", "b2", "p")
	w2.Status = "idle"
	other := apptest.WorkW("w3", "b3", "q")
	other.Status = "idle"
	o := apptest.OrchW("o", "lead", "p")
	o.Status = "idle"
	return agentstate.Snapshot{Orchestrators: []agents.Worker{o}, Workers: []agents.Worker{w1, w2, other}}
}

func TestTheBarShowsTheTabsOfTheStartAgent(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo"), apptest.TermW("tq", "term-q", "q", "/work/other")}
	_, tt := agentFrame(t, &snap)
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "$ repo"}) {
		t.Errorf("tabs %q, want the orchestrator and the plain terminal of p", got)
	}
}

func TestTabsFollowTheSelectedAgent(t *testing.T) {
	snap := tabSnap()
	snap.Workers[2] = apptest.WorkW("w3", "b3", "p")
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "b1", "$ repo"}) {
		t.Fatalf("tabs %q, want the session, b1 and the terminal", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w3"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "b3", "$ repo"}) {
		t.Fatalf("tabs %q, want the session, b3 and the terminal", got)
	}
	// A plain terminal keeps the worker whose tab shows.
	f.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "t1"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "b3", "$ repo"}) {
		t.Errorf("tabs %q after selecting the terminal, want b3 kept", got)
	}
	// An agent of another project shows its own project's terminals.
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w4"})
	snap.Workers = append(snap.Workers, apptest.WorkW("w4", "b4", "q"))
	f.syncAgents()
	tt.Frame()
	if slices.Contains(tabTexts(tt), "$ repo") {
		t.Errorf("a plain terminal of project p shows for project q: %q", tt.Texts())
	}
}

func TestClickingATabInTheFrameSelectsIt(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	tt.Frame()
	if err := tt.Click("$ repo"); err != nil {
		t.Fatal(err)
	}
	if sel, ok := f.Selection.Current(); !ok || sel != (state.Selected{Kind: state.KindTerminal, ID: "t1"}) {
		t.Errorf("selection %+v %v", sel, ok)
	}
	tt.Frame()
	if err := tt.Click("lead"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindOrchestrator, ID: "o"}) {
		t.Errorf("selection %+v, want the orchestrator", sel)
	}
}

func TestAWorkerGoneFromTheSnapshotLeavesTheBar(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if !slices.Contains(tabTexts(tt), "b1") {
		t.Fatalf("tabs %q", tabTexts(tt))
	}
	snap.Workers = snap.Workers[1:]
	f.syncAgents()
	tt.Frame()
	if slices.Contains(tabTexts(tt), "b1") {
		t.Errorf("tabs %q still show the gone worker", tabTexts(tt))
	}
}

func TestAStatusChangeInTheSnapshotRedrawsTheGlyph(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if apptest.Absent(tt, "working") {
		t.Fatal("a working worker shows no working glyph")
	}
	snap.Workers = append([]agents.Worker(nil), snap.Workers...)
	snap.Workers[0].Status = "blocked"
	f.syncAgents()
	tt.Frame()
	if apptest.Absent(tt, "needs you") || !apptest.Absent(tt, "working") {
		t.Errorf("the tab should show needs you; texts %q", tt.Texts())
	}
}

func TestNoAgentSourceShowsNoTabs(t *testing.T) {
	_, tt := newTester(t)
	if !apptest.Absent(tt, titlebar.LabelTabs) && apptest.Box(t, tt, titlebar.LabelTabs).W != 0 {
		t.Error("the tabs row shows without agents")
	}
	if apptest.Absent(tt, titlebar.LabelFilesToggle) {
		t.Error("the files toggle should stay")
	}
}

func TestArrowKeysChooseTheNextTabAndSelectIt(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	tt.Frame()
	if err := tt.Click("lead"); err != nil { // focus the tab list
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if sel, _ := f.Selection.Current(); sel != (state.Selected{Kind: state.KindTerminal, ID: "t1"}) {
		t.Errorf("selection %+v, want the plain terminal", sel)
	}
}

func TestSelectingATabClosesTheReviewView(t *testing.T) {
	snap := tabSnap()
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Review, _ = newTestReview()
	f.ToggleReview()
	tt.Frame()
	if err := tt.Click("$ repo"); err != nil {
		t.Fatal(err)
	}
	if f.ReviewOpen() {
		t.Error("the Review view stays open after a tab click")
	}
}

func TestTheCloseButtonStopsAPlainTerminalAndNeverAnAgent(t *testing.T) {
	snap := termSnap()
	cf := newCloseFrame(t, &snap)
	// The agent's own tab is selected: no close button, so no way to stop it from the bar.
	cf.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	cf.tt.Frame()
	if !apptest.Absent(cf.tt, titlebar.LabelCloseTab) {
		t.Fatal("an agent tab has a close button")
	}
	cf.closeTab(tabmodel.Tab{Address: "o", Orchestrator: true}) // even a direct call stops nothing
	if len(cf.stopped) != 0 {
		t.Errorf("stopped %v", cf.stopped)
	}
	// The plain terminal's tab is selected: the button stops it.
	cf.Selection.Select(state.Selected{Kind: state.KindTerminal, ID: "tp"})
	cf.tt.Frame()
	if err := cf.tt.Click(titlebar.LabelCloseTab); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cf.stopped, []string{"tp"}) {
		t.Errorf("stopped %v, want [tp]", cf.stopped)
	}
}

func TestTheDiffSummaryFollowsTheSelectedAgentAndReadsOnlyThroughTheTree(t *testing.T) {
	repo := apptest.NewRepo(t)
	repo.Git("checkout", "-b", "feature")
	apptest.Write(t, repo.Dir, "new.go", "a\nb\nc\n") // untracked: +3
	snap := tabSnap()
	snap.Workers[0].Worktree = repo.Dir
	f, tt := agentFrame(t, &snap)
	f.panelOpen = false // the files panel is hidden: the summary still reads
	f.Files.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	tt.Frame()
	if got := f.Files.Shown(); got != repo.Dir {
		t.Fatalf("the tree reads %q, want the selected worker's worktree", got)
	}
	if err := tt.Click("3 lines added, 1 file. Open review"); err != nil {
		t.Fatalf("no diff summary; texts %q: %v", tt.Texts(), err)
	}
	sum, ok := f.Diff.Summary()
	if want := (review.Summary{Added: 3, Files: 1}); !ok || sum != want {
		t.Errorf("summary = %+v, %v; want %+v", sum, ok, want)
	}
	f.Files.Close()
}

func TestTheDiffSummaryClickOpensReviewInTheFrame(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	f.Review, _ = newTestReview()
	f.Diff = files.ReadyDiff(review.Summary{Added: 1, Files: 1})
	tt.Frame()
	if err := tt.Click("1 line added, 1 file. Open review"); err != nil {
		t.Fatal(err)
	}
	if !f.ReviewOpen() {
		t.Error("the click did not open Review")
	}
	tt.Frame()
	if err := tt.Click("1 line added, 1 file. Open review"); err != nil || !f.ReviewOpen() {
		t.Errorf("a second click closed Review (err %v)", err)
	}
}

func tabTexts(tt *ui.Tester) []string {
	var out []string
	for _, s := range tt.Texts() {
		if slices.Contains([]string{"lead", "b1", "b2", "b3", "$ repo", "$ other"}, s) {
			out = append(out, s)
		}
	}
	return slices.Compact(out) // the title is both the label and the text
}
