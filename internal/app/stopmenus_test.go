package app

import (
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/tabmodel"
)

// ---- Stop and Hide in the project and tab menus (D219) ----

func TestProjectMenuClosesAllItsAgents(t *testing.T) {
	h := newRows(t)
	if err := h.tt.RightClick("alpha, main"); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuCloseAll, "-", MenuHideProject}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := h.tt.ChooseMenuItem(MenuCloseAll); err != nil {
		t.Fatal(err)
	}
	stopped, closed := h.src.calls()
	slices.Sort(closed)
	if want := []string{"b-241", "b-242", "lead"}; !slices.Equal(closed, want) || len(stopped) != 0 {
		t.Errorf("closed %v (stopped %v), want %v", closed, stopped, want)
	}
}

func TestProjectMenuHidesTheProject(t *testing.T) {
	h := newRows(t)
	h.tt.RightClick("alpha, main")
	if err := h.tt.ChooseMenuItem(MenuHideProject); err == nil {
		t.Error("Hide project could be chosen with no handler")
	}
	var hidden []string
	h.list.OnHide = func(dir string) { hidden = append(hidden, dir) }
	h.tt.Frame()
	h.tt.RightClick("alpha, main")
	if err := h.tt.ChooseMenuItem(MenuHideProject); err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 || hidden[0] != h.list.groups[0].CommonDir {
		t.Errorf("hidden %v, want %q", hidden, h.list.groups[0].CommonDir)
	}
	if stopped, _ := h.src.calls(); len(stopped) != 0 {
		t.Errorf("Hide stopped %v", stopped)
	}
}

func TestEmptyProjectMenuHasHideAndNoCloseAll(t *testing.T) {
	h, _ := emptyProjectRig(t)
	moveToEmpty(t, h)
	g := spaces.RepoGroup{Name: "gamma"}
	if err := h.tt.RightClick(ProjectLabel(h.list.groups[len(h.list.groups)-1])); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{LabelNewIn(g), "-", MenuHideProject}; !slices.Equal(got, want) {
		t.Errorf("menu %q, want %q", got, want)
	}
}

func TestTabMenuStopsAnAgentTab(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var stopped []tabmodel.Tab
	b.bar.stopTab = func(t tabmodel.Tab) { stopped = append(stopped, t) }
	b.tt.Frame()
	if err := b.tt.RightClick("lead"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(tabmodel.MenuStop); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0].Address != "o" {
		t.Errorf("stopped %+v, want o", stopped)
	}
	if len(b.closed) != 0 {
		t.Errorf("closed %+v", b.closed)
	}
}

func TestTabMenuOnAPlainTabHasNoStop(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	b.bar.stopTab = func(tabmodel.Tab) { t.Error("Stop ran on a plain tab") }
	b.tt.Frame()
	if err := b.tt.RightClick("go test ./..."); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(b.tt.Menu(), tabmodel.MenuStop) {
		t.Errorf("menu %q has Stop", b.tt.Menu())
	}
}

func TestStopAgentWithNoSourceDoesNothing(t *testing.T) {
	h := newRows(t)
	h.list.Source = nil
	h.list.StopAgent("lead", "lead")
	if stopped, _ := h.src.calls(); len(stopped) != 0 {
		t.Errorf("stopped %v", stopped)
	}
}

func TestAgentRowMenuClosesTheAgent(t *testing.T) {
	h := newRows(t)
	var told []string
	h.list.Env.Status = func(s string) { told = append(told, s) }
	h.tt.RightClick("lead, idle, opus")
	if err := h.tt.ChooseMenuItem(MenuCloseAgent); err != nil {
		t.Fatal(err)
	}
	if stopped, closed := h.src.calls(); len(stopped) != 0 || !slices.Equal(closed, []string{"lead"}) {
		t.Errorf("stopped %v, closed %v, want closed lead", stopped, closed)
	}
	if !slices.Equal(told, []string{"lead closed"}) {
		t.Errorf("status %q", told)
	}
}

func TestTabMenuClosesAnAgentTab(t *testing.T) {
	b := newBar(t, tabsOf(), true)
	var gone []tabmodel.Tab
	b.bar.closeAgent = func(t tabmodel.Tab) { gone = append(gone, t) }
	b.tt.Frame()
	b.tt.RightClick("lead")
	if err := b.tt.ChooseMenuItem(MenuClose); err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0].Address != "o" || len(b.closed) != 0 {
		t.Errorf("closed agent %+v, plain %+v", gone, b.closed)
	}
}
