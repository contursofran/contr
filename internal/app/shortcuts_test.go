package app

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/shortcut"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/view"
)

// tabKeysFrame is a frame with the tabs lead, b1 and $ repo (b1 selected).
func tabKeysFrame(t *testing.T) (*Frame, *ui.Tester) {
	t.Helper()
	snap := tabSnap()
	snap.Terminals = []agents.Worker{apptest.TermW("t1", "term-1", "p", "/work/repo")}
	f, tt := agentFrame(t, &snap)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if got := tabTexts(tt); !slices.Equal(got, []string{"lead", "b1", "$ repo"}) {
		t.Fatalf("tabs %q", got)
	}
	return f, tt
}

func selected(f *Frame) string {
	sel, _ := f.Selection.Current()
	return sel.ID
}

func press(tt *ui.Tester, id shortcut.ID) {
	s := shortcut.Of(id)
	tt.Key(s.Mods, s.Key)
	tt.Frame()
}

func TestCmdNumberSelectsThatTabInTheOrderOfTheBar(t *testing.T) {
	f, tt := tabKeysFrame(t)
	for _, c := range []struct {
		key  shortcut.ID
		want string
	}{
		{shortcut.Tab3, "t1"}, {shortcut.Tab2, "w1"}, {shortcut.LastTab, "t1"}, {shortcut.Tab1, "o"},
		// The orchestrator's tabs are lead and $ repo: no third tab, so nothing changes.
		{shortcut.Tab3, "o"}, {shortcut.LastTab, "t1"},
	} {
		press(tt, c.key)
		if got := selected(f); got != c.want {
			t.Fatalf("after %s: %q, want %q", c.key, got, c.want)
		}
	}
}

func TestCmdShiftBracketsStepTheTabsAroundTheEnds(t *testing.T) {
	f, tt := tabKeysFrame(t)
	for _, c := range []struct {
		key  shortcut.ID
		want string
	}{
		// From b1 of lead, b1, $ repo; the orchestrator's tabs are lead and $ repo.
		{shortcut.NextTab, "t1"}, {shortcut.NextTab, "o"}, {shortcut.PrevTab, "t1"}, {shortcut.PrevTab, "o"},
	} {
		press(tt, c.key)
		if got := selected(f); got != c.want {
			t.Fatalf("after %s: %q, want %q", c.key, got, c.want)
		}
	}
}

func TestStepTabWithNoSelectedTabTakesAnEnd(t *testing.T) {
	f, _ := tabKeysFrame(t)
	f.Selection.Clear()
	f.stepTab(-1)
	if got := selected(f); got != f.titleTabs()[len(f.titleTabs())-1].Address {
		t.Errorf("⌘⇧[ with no selected tab: %q, want the last tab", got)
	}
	f.Selection.Clear()
	f.stepTab(1)
	if got := selected(f); got != f.titleTabs()[0].Address {
		t.Errorf("⌘⇧] with no selected tab: %q, want the first tab", got)
	}
	empty := NewFrame(Deps{})
	empty.stepTab(1) // no tabs: nothing to do
}

func TestCmdOOpensTheProjectsModalAndLeavesItsOwnCmdOToIt(t *testing.T) {
	f, tt := newTester(t)
	picks := 0
	f.ProjectsModal = &projectsview.ProjectsModal{
		Groups:     func() []spaces.RepoGroup { return nil },
		PickFolder: func() (string, error) { picks++; return "", projectsview.ErrPickCancelled },
		Env:        view.Env{Run: view.Runner{Go: func(fn func()) { fn() }}},
	}
	press(tt, shortcut.OpenProject)
	if !f.ProjectsModal.Opened() {
		t.Fatal("⌘O did not open the projects modal")
	}
	press(tt, shortcut.OpenProject)
	if !f.ProjectsModal.Opened() || picks != 1 {
		t.Errorf("⌘O in the modal: open %v, folder dialogs %d; want open and one dialog", f.ProjectsModal.Opened(), picks)
	}
}

func TestCmdNStartsANewOrchestrator(t *testing.T) {
	f, tt := newTester(t)
	picks := 0
	f.NewOrch = &NewOrchestrator{
		Pick:  func() (string, error) { picks++; return "", projectsview.ErrPickCancelled },
		Spawn: func(string) (string, error) { return "", nil },
	}
	press(tt, shortcut.NewOrchestrator)
	if picks != 1 {
		t.Errorf("⌘N: %d folder dialogs, want 1", picks)
	}
}

func TestCmdAltArrowsStepTheAgentRowsOfTheSidebar(t *testing.T) {
	snap := tabSnap()
	f, tt := agentFrame(t, &snap)
	dir := apptest.CanonicalTmp(t)
	apptest.MainRepo(t, dir, "ref: refs/heads/main\n")
	o := apptest.OrchW("o", "lead", "p")
	o.Worktree = dir
	w1, w2 := apptest.WorkW("w1", "b1", "p"), apptest.WorkW("w2", "b2", "p")
	w1.Parent, w1.Worktree, w2.Parent, w2.Worktree = "o", dir, "o", dir
	live := apptest.SpTree(apptest.SpPane("w1:p1", dir, nil))
	live.Orchestrators, live.Workers = []agents.Worker{o}, []agents.Worker{w1, w2}
	f.SpacesList = sidebar.NewSpacesList(func() agentstate.Snapshot { return live }, f.Selection, view.Env{})
	f.SpacesList.Env.Run.Go = func(fn func()) { fn() }
	f.SpacesList.Sync()
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	tt.Frame()
	for _, c := range []struct {
		key  shortcut.ID
		want string
	}{
		{shortcut.NextAgent, "w1"}, {shortcut.NextAgent, "w2"}, {shortcut.NextAgent, "w2"}, {shortcut.PrevAgent, "w1"},
	} {
		press(tt, c.key)
		if got := selected(f); got != c.want {
			t.Fatalf("after %s: %q, want %q", c.key, got, c.want)
		}
	}
}

// chips counts the key chips of the last frame.
func chips(tt *ui.Tester) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == kit.LabelKeyHint {
			n++
		}
	}
	return n
}

// holdCmd holds ⌘ alone until the chips show.
func holdCmd(t *testing.T, tt *ui.Tester) {
	t.Helper()
	tt.HoldModifiers(ui.Cmd)
	tt.Advance(shortcut.HintDelay)
	if chips(tt) == 0 {
		t.Fatalf("no chips after %v: %q", shortcut.HintDelay, tt.Texts())
	}
}

func TestHoldingCmdShowsTheChipsAfterTheDelayAndNotBefore(t *testing.T) {
	_, tt := tabKeysFrame(t)
	tt.HoldModifiers(ui.Cmd)
	tt.Advance(shortcut.HintDelay - time.Millisecond)
	if n := chips(tt); n != 0 {
		t.Fatalf("%d chips before the delay", n)
	}
	tt.Advance(time.Millisecond)
	// The three tabs, Search and Hide files.
	if n := chips(tt); n != 5 {
		t.Errorf("%d chips, want 5: %q", n, tt.Texts())
	}
	for _, key := range []string{"1", "2", "3", "F", "⇧E"} {
		if !slices.Contains(tt.Texts(), key) {
			t.Errorf("no chip %q in %q", key, tt.Texts())
		}
	}
	if tt.FramePending() {
		t.Error("a frame waits while the chips show and nothing changes")
	}
}

func TestTheChipsHideOnReleaseAnotherKeyBlurAndAMenuKey(t *testing.T) {
	cases := map[string]func(f *Frame, tt *ui.Tester){
		"release":     func(_ *Frame, tt *ui.Tester) { tt.HoldModifiers(0) },
		"another key": func(_ *Frame, tt *ui.Tester) { tt.KeyDown(ui.KeyC) },
		"blur":        func(_ *Frame, tt *ui.Tester) { tt.SetFocused(false) },
		"menu key": func(f *Frame, tt *ui.Tester) {
			f.usedKey(func() {})()
			tt.Frame()
		},
	}
	for name, do := range cases {
		f, tt := tabKeysFrame(t)
		holdCmd(t, tt)
		do(f, tt)
		tt.Frame()
		if n := chips(tt); n != 0 {
			t.Errorf("%s: %d chips", name, n)
		}
	}
}

func TestAShortCmdKeyShowsNoChips(t *testing.T) {
	_, tt := tabKeysFrame(t)
	tt.HoldModifiers(ui.Cmd)
	tt.Advance(50 * time.Millisecond)
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Advance(time.Second)
	if n := chips(tt); n != 0 {
		t.Errorf("%d chips after ⌘C", n)
	}
	if tt.FramePending() {
		t.Error("a frame waits after ⌘C")
	}
}
