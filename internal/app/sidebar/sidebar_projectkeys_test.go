package sidebar

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/projects"
)

// repos makes one repo for each name, each with a linked worktree and a terminal in it, in
// the order of names.
func repos(t *testing.T, names ...string) *spacesHarness {
	t.Helper()
	base := apptest.CanonicalTmp(t)
	var panes []agents.Worker
	for i, name := range names {
		gitDir := apptest.MainRepo(t, filepath.Join(base, name), "ref: refs/heads/main\n")
		wt := filepath.Join(base, name+"-wt")
		linkedWorktree(t, gitDir, "w1", wt, "ref: refs/heads/feature\n")
		panes = append(panes, apptest.SpPane("w1:p"+string(rune('1'+i)), wt, nil))
	}
	h := newSpaces(t, apptest.SpTree(panes...))
	if got := len(h.list.Groups()); got != len(names) {
		t.Fatalf("%d projects, want %d", got, len(names))
	}
	return h
}

// openProject is the name of the project that holds the selection; "" for none.
func openProject(h *spacesHarness) string {
	if i := h.list.openGroup(); i >= 0 {
		return h.list.Groups()[i].Name
	}
	return ""
}

func TestCmdAltArrowsStepTheProjectsAroundTheEnds(t *testing.T) {
	h := repos(t, "alpha", "beta", "gamma")
	h.sel.Clear() // the list opens a project at start
	for _, c := range []struct {
		d    int
		want string
	}{
		{1, "alpha"}, // nothing open: ⌘⌥→ takes the first
		{1, "beta"}, {1, "gamma"},
		{1, "alpha"},  // around the end
		{-1, "gamma"}, // around the start
		{-1, "beta"},
	} {
		h.list.StepProject(c.d)
		if got := openProject(h); got != c.want {
			t.Fatalf("step %d: %q, want %q", c.d, got, c.want)
		}
	}
	h.sel.Clear()
	h.list.StepProject(-1) // nothing open: ⌘⌥← takes the last
	if got := openProject(h); got != "gamma" {
		t.Errorf("⌘⌥← with nothing open: %q, want gamma", got)
	}
}

func TestTheProjectStepIsAClickOnTheRow(t *testing.T) {
	h := repos(t, "alpha", "beta")
	g := h.list.Groups()[1]
	h.sel.Clear()
	h.list.StepProject(1)
	h.list.StepProject(1)
	stepped, _ := h.sel.Current()
	h.sel.Clear()
	h.list.Activate(g)
	clicked, _ := h.sel.Current()
	if stepped != clicked {
		t.Errorf("step selected %+v, a click %+v", stepped, clicked)
	}
}

func TestTheProjectStepSkipsHiddenProjects(t *testing.T) {
	h := repos(t, "alpha", "beta", "gamma")
	s := &projects.Store{}
	s.Hide(h.list.Groups()[1].CommonDir)
	h.list.SetProjects(s)
	h.list.Switch(h.list.Groups()[0].CommonDir)
	h.list.StepProject(1)
	if got := openProject(h); got != "gamma" {
		t.Errorf("⌘⌥→ from alpha with beta hidden: %q, want gamma", got)
	}
}

func TestTheProjectStepDoesNothingWithOneProject(t *testing.T) {
	h := repos(t, "alpha")
	h.sel.Clear()
	h.list.StepProject(1)
	if _, ok := h.sel.Current(); ok {
		t.Fatal("⌘⌥→ with one project and nothing open selected it")
	}
	h.list.Switch(h.list.Groups()[0].CommonDir)
	before, _ := h.sel.Current()
	h.list.StepProject(1)
	h.list.StepProject(-1)
	if after, _ := h.sel.Current(); after != before {
		t.Errorf("one project: %+v, then %+v", before, after)
	}
	h.list.Hints = true
	h.tt.Frame()
	for _, key := range []string{"⌥←", "⌥→"} {
		if !apptest.Absent(h.tt, kit.KeyHintLabel(key)) {
			t.Errorf("one project shows the chip %q", key)
		}
	}
}

// folderCenterX is the x of the center of a project row's folder icon: the row's padding and
// half the 15px icon from the row's left.
func folderCenterX(row ui.Rect) float32 { return row.X + theme.Space10 + 15.0/2 }

func TestTheProjectRowsBeforeAndAfterTheOpenOneShowTheirChips(t *testing.T) {
	h := repos(t, "alpha", "beta", "gamma")
	groups := h.list.Groups()
	h.list.Switch(groups[1].CommonDir)
	h.list.Hints = true
	h.tt.Frame()
	for _, c := range []struct {
		key string
		g   int
	}{{"⌥←", 0}, {"⌥→", 2}} {
		row := ProjectLabel(groups[c.g])
		chip := apptest.ChipOn(t, h.tt, kit.KeyHintLabel(c.key), row, theme.SidebarWidth, theme.WindowHeight)
		r := apptest.Box(t, h.tt, row)
		if cx := chip.X + chip.W/2; cx < folderCenterX(r)-0.25 || cx > folderCenterX(r)+0.25 {
			t.Errorf("chip %q center x %v, folder icon %v", c.key, cx, folderCenterX(r))
		}
	}
}

func TestWithTwoProjectsTheOtherRowShowsNext(t *testing.T) {
	h := repos(t, "alpha", "beta")
	h.list.Switch(h.list.Groups()[0].CommonDir)
	h.list.Hints = true
	h.tt.Frame()
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("⌥→"), ProjectLabel(h.list.Groups()[1]), theme.SidebarWidth, theme.WindowHeight)
	if !apptest.Absent(h.tt, kit.KeyHintLabel("⌥←")) {
		t.Error("two projects: a ⌥← chip shows")
	}
}

func TestTheIconColumnChipsStayInTheirRows(t *testing.T) {
	h := compactRows(t)
	h.list.Switch(h.list.Groups()[0].CommonDir)
	h.list.Hints = true
	h.tt.Frame()
	w, ht := float32(theme.SidebarIconWidth), float32(theme.WindowHeight)
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("O"), LabelProjectsOpen, w, ht)
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("N"), LabelProjectsNew, w, ht)
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("⌥→"), ProjectLabel(h.list.Groups()[1]), w, ht)
	// The agent row below the open project's orchestrator: on its glyph.
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("⌥↓"), "b-241, working, 241, sonnet", w, ht)
}

func TestTheAgentRowChipsStayInTheirRows(t *testing.T) {
	h := newRows(t)
	h.list.Hints = true
	h.list.StepAgent(1) // lead
	h.list.StepAgent(1) // b-241: lead above, b-242 below, a nested worker row
	h.tt.Frame()
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("⌥↑"), "lead, idle, opus", theme.SidebarWidth, theme.WindowHeight)
	apptest.ChipOn(t, h.tt, kit.KeyHintLabel("⌥↓"), "b-242, working, sonnet", theme.SidebarWidth, theme.WindowHeight)
}
