package app

import (
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/app/spaces"
)

// ---- Stop and Hide in the project and tab menus (D219) ----

func TestProjectMenuStopsAllItsAgents(t *testing.T) {
	h := newRows(t)
	if err := h.tt.RightClick("alpha, main"); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuStopAll, "-", MenuHideProject}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := h.tt.ChooseMenuItem(MenuStopAll); err != nil {
		t.Fatal(err)
	}
	stopped, _ := h.src.calls()
	slices.Sort(stopped)
	if want := []string{"b-241", "b-242", "lead"}; !slices.Equal(stopped, want) {
		t.Errorf("stopped %v, want %v", stopped, want)
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

func TestEmptyProjectMenuHasHideAndNoStopAll(t *testing.T) {
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
	var stopped []Tab
	b.bar.stopTab = func(t Tab) { stopped = append(stopped, t) }
	b.tt.Frame()
	if err := b.tt.RightClick("lead"); err != nil {
		t.Fatal(err)
	}
	if err := b.tt.ChooseMenuItem(MenuStop); err != nil {
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
	b.bar.stopTab = func(Tab) { t.Error("Stop ran on a plain tab") }
	b.tt.Frame()
	if err := b.tt.RightClick("go test ./..."); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(b.tt.Menu(), MenuStop) {
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
