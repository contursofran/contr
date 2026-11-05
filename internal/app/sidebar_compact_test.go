package app

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

func TestSidebarIconRuleBothWays(t *testing.T) {
	for _, tc := range []struct {
		win     float32
		compact bool
	}{{0, false}, {760, true}, {799, true}, {800, false}, {1099, false}, {1440, false}} {
		if got := compactSidebar(tc.win); got != tc.compact {
			t.Errorf("compactSidebar(%v) = %v, want %v", tc.win, got, tc.compact)
		}
	}
	// Under 800 an open sidebar is the icon column, whatever width was stored;
	// from 800 up the stored width is drawn; a closed sidebar keeps its width.
	for _, tc := range []struct {
		win, stored float32
		open        bool
		want        float32
	}{
		{799, 312, true, theme.SidebarIconWidth},
		{600, 480, true, theme.SidebarIconWidth},
		{800, 312, true, 290}, // the sidebar gives way for the terminal minimum, as before
		{900, 312, true, 312},
		{799, 312, false, 312},
	} {
		got, _, files := fitWidths(tc.win, tc.stored, 280, tc.open, true)
		if got != tc.want || (tc.win < theme.FilesHideBelow && files) {
			t.Errorf("fitWidths(%v, %v, open %v) = %v, files %v; want %v, no files", tc.win, tc.stored, tc.open, got, files, tc.want)
		}
	}
}

func TestNarrowWindowDrawsTheSidebarAsIconsAndRestoresIt(t *testing.T) {
	f, tt := newTester(t)
	tt.SetSize(760, 800)
	if got := box(t, tt, LabelPanelSidebar).W; got != theme.SidebarIconWidth {
		t.Errorf("sidebar %v wide at 760, want the icon column %d", got, theme.SidebarIconWidth)
	}
	if got := box(t, tt, LabelPanelTerminal).W; got < theme.TerminalMinWidth {
		t.Errorf("terminal %v wide at 760", got)
	}
	// The stored width (D111) is not changed by the collapse.
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("stored width %v after the collapse, want %d", f.sidebarW, theme.SidebarWidth)
	}
	// A drag of the divider in the icon column does not store a width either.
	drag(tt, sidebarDivider(t, tt), sidebarDivider(t, tt)+90)
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("a drag in the icon column stored %v", f.sidebarW)
	}
	box(t, tt, LabelSidebarToggle)
	tt.SetSize(900, 800)
	if got := box(t, tt, LabelPanelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar %v wide at 900, want the stored %d", got, theme.SidebarWidth)
	}
	if f.sidebarW != theme.SidebarWidth {
		t.Errorf("stored width %v after the window grew", f.sidebarW)
	}
}

func TestFilesPanelRuleStaysAt1100(t *testing.T) {
	_, tt := newTester(t)
	tt.SetSize(1099, 800)
	if !absent(tt, LabelPanelFiles) {
		t.Error("the files panel shows at 1099")
	}
	if got := box(t, tt, LabelPanelSidebar).W; got != theme.SidebarWidth {
		t.Errorf("sidebar %v wide at 1099, want it whole", got)
	}
}

// The title bar keeps the sidebar toggle clear of the window buttons in the
// icon column.
func TestTheSidebarToggleStaysInsideTheIconColumnCorner(t *testing.T) {
	_, tt := newTester(t)
	tt.SetSize(760, 800)
	r := box(t, tt, LabelSidebarToggle)
	if r.X < theme.TrafficLightInset || r.X+r.W > float32(760) {
		t.Errorf("toggle %+v is over the window buttons or outside the window", r)
	}
}

func compactRows(t *testing.T) *rowsHarness {
	t.Helper()
	h := newRows(t)
	h.list.compact = true
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
		r := box(t, h.tt, label)
		if r.W > theme.SidebarIconWidth {
			t.Errorf("%q is %v wide, wider than the column", label, r.W)
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
	if len(h.src.signalled) != 1 || h.src.signalled[0] != "b-241" {
		t.Errorf("interrupted %v", h.src.signalled)
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
		r := box(t, h.tt, label)
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
		if absent(h.tt, s) {
			t.Errorf("the wide sidebar lost the text %q", s)
		}
	}
}
