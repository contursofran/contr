package sidebar

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

func compactRows(t *testing.T) *rowsHarness {
	t.Helper()
	h := newRows(t)
	h.list.Compact = true
	h.tt.SetSize(theme.SidebarIconWidth, theme.WindowHeight)
	return h
}

func TestIconColumnRowsShowNoText(t *testing.T) {
	h := compactRows(t)
	shown := map[string]bool{}
	for _, s := range h.tt.Texts() {
		shown[s] = true
	}
	for _, s := range []string{"This Mac", "alpha", "main", "lead", "b-241", "Servers", "Projects", "local"} {
		if shown[s] {
			t.Errorf("the icon column shows the text %q; texts %q", s, h.tt.Texts())
		}
	}
	for _, label := range []string{"This Mac, local text", "alpha, main text", "lead, idle, opus title", "Servers header", "Projects header"} {
		if shown[label] {
			t.Errorf("the icon column draws %q", label)
		}
	}
	for _, label := range []string{"This Mac, local", "alpha, main", "lead, idle, opus", "b-241, working, 241, sonnet", "beta, main"} {
		r := apptest.Box(t, h.tt, label)
		if r.W > theme.SidebarIconWidth {
			t.Errorf("%q is %v wide, wider than the column", label, r.W)
		}
		// The icon column keeps its 6px margins; the 10px of D231 is for rows with text.
		if r.X < theme.Space6-0.5 || r.X > theme.Space6+0.5 {
			t.Errorf("%q fill at x %v, want %v", label, r.X, theme.Space6)
		}
	}
}

func TestIconColumnRowsKeepTheirClick(t *testing.T) {
	h := compactRows(t)
	if err := h.tt.Click("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	if sel, ok := h.sel.Current(); !ok || sel.Kind != state.KindWorker || sel.ID != "b-241" {
		t.Errorf("selection %+v after a click on the worker", sel)
	}
	if err := h.tt.Click("lead, idle, opus"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := h.sel.Current(); sel.Kind != state.KindOrchestrator || sel.ID != "lead" {
		t.Errorf("selection %+v after a click on the orchestrator", sel)
	}
	// A click on the project selects its orchestrator, then a second click folds it.
	if err := h.tt.Click("beta, main"); err != nil {
		t.Fatal(err)
	}
	if sel, _ := h.sel.Current(); sel.Kind != state.KindWorktree {
		t.Errorf("selection %+v after a click on the project", sel)
	}
}

func TestIconColumnAgentRowKeepsItsContextMenu(t *testing.T) {
	h := compactRows(t)
	if err := h.tt.RightClick("b-241, working, 241, sonnet"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(MenuInterrupt); err != nil {
		t.Fatal(err)
	}
	if len(h.src.Signalled) != 1 || h.src.Signalled[0] != "b-241" {
		t.Errorf("interrupted %v", h.src.Signalled)
	}
}

func TestIconColumnRowsShowTheirNameInATooltip(t *testing.T) {
	for label, tip := range map[string]string{
		"This Mac, local":             "This Mac",
		"alpha, main":                 "alpha",
		"lead, idle, opus":            "lead",
		"b-241, working, 241, sonnet": "b-241",
	} {
		h := compactRows(t)
		r := apptest.Box(t, h.tt, label)
		has := func() bool {
			for _, s := range h.tt.Texts() {
				if s == tip {
					return true
				}
			}
			return false
		}
		if has() {
			t.Errorf("%q: the tooltip %q shows before the pointer rests", label, tip)
		}
		h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
		h.tt.Advance(time.Second)
		if !has() {
			t.Errorf("%q: no tooltip %q; texts %q", label, tip, h.tt.Texts())
		}
	}
}

func TestIconColumnKeepsTheProjectButtons(t *testing.T) {
	h := compactRows(t)
	var opened, made bool
	h.list.OnProjects = func() { opened = true }
	h.list.OnNew = func() { made = true }
	if err := h.tt.Click(LabelProjectsOpen); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click(LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if !opened || !made {
		t.Errorf("open %v, new %v", opened, made)
	}
}

func TestTheWideSidebarStillShowsText(t *testing.T) {
	h := newRows(t)
	for _, s := range []string{"This Mac, local text", "alpha, main text", "lead, idle, opus title", "Servers header", "Projects header"} {
		if apptest.Absent(h.tt, s) {
			t.Errorf("the wide sidebar lost the text %q", s)
		}
	}
}
