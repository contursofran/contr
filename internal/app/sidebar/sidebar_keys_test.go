package sidebar

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/ghsource/ghsourcetest"
	"github.com/contursofran/contr/internal/projects"
)

func selID(h *rowsHarness) string {
	sel, _ := h.sel.Current()
	return sel.ID
}

func TestArrowKeysMoveTheSelectionOverAgentRows(t *testing.T) {
	h := newRows(t)
	if err := h.tt.Click("lead, idle, opus"); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyDown)
	if got := selID(h); got != "b-241" {
		t.Fatalf("after Down: %q, want b-241", got)
	}
	h.tt.Key(0, ui.KeyDown)
	if got := selID(h); got != "b-242" {
		t.Fatalf("after Down: %q, want b-242", got)
	}
	h.tt.Key(0, ui.KeyUp)
	if got := selID(h); got != "b-241" {
		t.Fatalf("after Up: %q, want b-241", got)
	}
}

func TestUpFromTheFirstAgentReachesTheProjectRowAndDownComesBack(t *testing.T) {
	h := newRows(t)
	h.tt.Click("lead, idle, opus")
	h.tt.Key(0, ui.KeyUp) // the project row: it stands for its orchestrator
	if got := selID(h); got != "lead" {
		t.Fatalf("on the project row: %q, want lead", got)
	}
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyDown)
	if got := selID(h); got != "b-241" {
		t.Fatalf("two Down from the project row: %q, want b-241", got)
	}
}

func TestDownCrossesIntoTheNextProject(t *testing.T) {
	h := newRows(t)
	h.tt.Click("b-242, working, sonnet")
	h.tt.Key(0, ui.KeyDown)
	sel, ok := h.sel.Current()
	if !ok || sel.ID == "b-242" || sel.ID == "" {
		t.Fatalf("selection %+v, want the next project", sel)
	}
}

func TestEnterOpensTheSelectedRow(t *testing.T) {
	h := newRows(t)
	opened := 0
	h.list.OnOpen = func() { opened++ }
	h.tt.Click("b-241, working, 241, sonnet")
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyEnter)
	if opened != 1 || selID(h) != "b-242" {
		t.Errorf("opened %d, selection %q; want 1, b-242", opened, selID(h))
	}
}

func TestArrowKeysAndEnterWorkInTheIconColumn(t *testing.T) {
	h := compactRows(t)
	opened := 0
	h.list.OnOpen = func() { opened++ }
	if err := h.tt.Click("lead, idle, opus"); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyDown)
	if got := selID(h); got != "b-241" {
		t.Fatalf("after Down: %q, want b-241", got)
	}
	h.tt.Key(0, ui.KeyEnter)
	if opened != 1 {
		t.Errorf("opened %d, want 1", opened)
	}
}

func TestAnIdleWorkerWithPendingChecksShowsCIOnLineTwo(t *testing.T) {
	h := newRows(t)
	rig := ghsourcetest.NewPRSourceRig(map[string]string{h.cwds[0]: `{"number":307,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`})
	rig.W.Sync(apptest.LiveWorker(h.cwds[0]))
	rig.Flush()
	h.list.Entry = ghsource.WorkerEntry{PRs: rig.W}
	snap := h.live.get()
	snap.Workers[0].Status = "idle"
	h.send(snap)
	if !h.has("b-241, idle, 241, sonnet, waiting on CI") {
		t.Error("idle worker with pending checks has no CI label")
	}
	if !h.has("b-242, working, sonnet") {
		t.Error("a working worker must not show CI")
	}
	if _, ok := h.tt.Find("CI"); !ok {
		t.Error("no CI text on line 2")
	}
}

// emptyProjectRig is the sidebar with the projects alpha and beta and an empty project gamma.
func emptyProjectRig(t *testing.T) (*rowsHarness, *[]string) {
	t.Helper()
	h := newRows(t)
	s := &projects.Store{}
	s.AddFolder("/proj/gamma/.git", "/proj/gamma")
	h.list.SetProjects(s)
	var started []string
	h.list.OnNewIn = func(dir string) { started = append(started, dir) }
	h.tt.Frame()
	return h, &started
}

// moveToEmpty puts the key cursor on the empty project, the last row.
func moveToEmpty(t *testing.T, h *rowsHarness) {
	t.Helper()
	// The list has the focus by Tab (D145); the keys then move the cursor.
	for i := 0; i < 20 && !h.tt.Focused(LabelProjectList); i++ {
		h.tt.Key(0, ui.KeyTab)
	}
	if !h.tt.Focused(LabelProjectList) {
		t.Fatal("the project list never took the focus")
	}
	for range 12 {
		h.tt.Key(0, ui.KeyDown)
		if h.list.cursor >= 0 && h.list.cursor < len(h.list.items()) {
			if it := h.list.items()[h.list.cursor]; it.agent == nil && h.list.groups[it.group].Path != "" {
				return
			}
		}
	}
	t.Fatalf("the cursor never reached the empty project: row %d", h.list.cursor)
}

func TestEnterOnAnEmptyProjectStartsNothing(t *testing.T) {
	h, started := emptyProjectRig(t)
	opened := 0
	h.list.OnOpen = func() { opened++ }
	moveToEmpty(t, h)
	h.tt.Key(0, ui.KeyEnter)
	if len(*started) != 0 {
		t.Errorf("Enter started %v", *started)
	}
	if opened != 0 {
		t.Error("Enter on an empty project gave the focus to a terminal")
	}
}

func TestTheMenuKeyOnAnEmptyProjectOpensItsMenu(t *testing.T) {
	h, started := emptyProjectRig(t)
	moveToEmpty(t, h)
	h.tt.Key(0, ui.KeyContextMenu)
	if err := h.tt.ChooseMenuItem(MenuNewOrchestrator); err != nil {
		t.Fatalf("no menu on the menu key: %v", err)
	}
	if !slices.Equal(*started, []string{"/proj/gamma"}) {
		t.Errorf("started %v", *started)
	}
}

func TestTheMenuKeyOnAProjectWithAgentsOpensItsMenu(t *testing.T) {
	h, _ := emptyProjectRig(t)
	for i := 0; i < 20 && !h.tt.Focused(LabelProjectList); i++ {
		h.tt.Key(0, ui.KeyTab)
	}
	h.tt.Key(0, ui.KeyDown) // the first row: a project with agents
	h.tt.Key(0, ui.KeyContextMenu)
	if slices.Contains(h.tt.Menu(), MenuNewOrchestrator) {
		t.Error("the menu of a row with an orchestrator has a new orchestrator")
	}
	if !slices.Contains(h.tt.Menu(), MenuHideProject) {
		t.Errorf("menu %q, want Close project (D219, D291)", h.tt.Menu())
	}
}

func TestEnterOnAProjectWithAgentsStillOpensIt(t *testing.T) {
	h, started := emptyProjectRig(t)
	opened := 0
	h.list.OnOpen = func() { opened++ }
	h.tt.Click("lead, idle, opus")
	h.tt.Key(0, ui.KeyEnter)
	if opened != 1 || len(*started) != 0 {
		t.Errorf("opened %d, started %v", opened, *started)
	}
}

func TestAClickOnAnAgentRowFocusesTheTerminal(t *testing.T) {
	h := newRows(t)
	picked, opened := 0, 0
	h.list.OnPick = func() { picked++ }
	h.list.OnOpen = func() { opened++ }
	if err := h.tt.Click("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	if picked != 1 || opened != 0 || selID(h) != "b-241" {
		t.Fatalf("picked %d, opened %d, selection %q; want 1, 0, b-241", picked, opened, selID(h))
	}
	// A click on the row that is already selected focuses the terminal again.
	if err := h.tt.Click("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	if picked != 2 {
		t.Errorf("picked %d after a second click, want 2", picked)
	}
}

func TestArrowKeysDoNotFocusTheTerminal(t *testing.T) {
	h := newRows(t)
	picked := 0
	h.tt.Click("lead, idle, opus")
	h.list.OnPick = func() { picked++ }
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyUp)
	if picked != 0 {
		t.Errorf("picked %d after arrow keys, want 0", picked)
	}
}
