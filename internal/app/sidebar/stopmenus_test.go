package sidebar

import (
	"slices"
	"testing"
)

// ---- Stop and Hide in the project and tab menus (D219) ----

func TestProjectMenuClosesAllItsAgents(t *testing.T) {
	h := newRows(t)
	if err := h.tt.RightClick("alpha, main"); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuNewTerminal, "-", MenuCloseAll, MenuHideProject}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := h.tt.ChooseMenuItem(MenuCloseAll); err != nil {
		t.Fatal(err)
	}
	stopped, closed := h.src.Calls()
	slices.Sort(closed)
	if want := []string{"b-241", "b-242", "lead"}; !slices.Equal(closed, want) || len(stopped) != 0 {
		t.Errorf("closed %v (stopped %v), want %v", closed, stopped, want)
	}
}

func TestProjectMenuHidesTheProject(t *testing.T) {
	h := newRows(t)
	h.tt.RightClick("alpha, main")
	if err := h.tt.ChooseMenuItem(MenuHideProject); err == nil {
		t.Error("Close project could be chosen with no handler")
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
	if stopped, _ := h.src.Calls(); len(stopped) != 0 {
		t.Errorf("Hide stopped %v", stopped)
	}
}

func TestEmptyProjectMenuHasHideAndNoCloseAll(t *testing.T) {
	h, _ := emptyProjectRig(t)
	moveToEmpty(t, h)
	if err := h.tt.RightClick(ProjectLabel(h.list.groups[len(h.list.groups)-1])); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuNewOrchestrator, MenuNewTerminal, "-", MenuHideProject}; !slices.Equal(got, want) {
		t.Errorf("menu %q, want %q", got, want)
	}
}

func TestStopAgentWithNoSourceDoesNothing(t *testing.T) {
	h := newRows(t)
	h.list.Source = nil
	h.list.StopAgent("lead", "lead")
	if stopped, _ := h.src.Calls(); len(stopped) != 0 {
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
	if stopped, closed := h.src.Calls(); len(stopped) != 0 || !slices.Equal(closed, []string{"lead"}) {
		t.Errorf("stopped %v, closed %v, want closed lead", stopped, closed)
	}
	if !slices.Equal(told, []string{"lead closed"}) {
		t.Errorf("status %q", told)
	}
}

// ---- The starts of a project row's menu (D291) ----

func TestProjectWithTerminalsButNoAgentsHasStartsAndNoCloseAll(t *testing.T) {
	h, _ := twoRepos(t)
	h.list.OnNewIn = func(string) {}
	h.list.OnNewTerminalIn = func(string, string) {}
	h.tt.Frame()
	if err := h.tt.RightClick(ProjectLabel(h.list.groups[0])); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuNewOrchestrator, MenuNewTerminal, "-", MenuHideProject}; !slices.Equal(got, want) {
		t.Errorf("menu %q, want %q", got, want)
	}
}

func TestProjectWithWorkersButNoOrchestratorStartsOneInItsFolder(t *testing.T) {
	h := newRows(t)
	var started []string
	h.list.OnNewIn = func(dir string) { started = append(started, dir) }
	h.tt.Frame()
	g := h.list.groups[1]
	if err := h.tt.RightClick(ProjectLabel(g)); err != nil {
		t.Fatal(err)
	}
	if got, want := h.tt.Menu(), []string{MenuNewOrchestrator, MenuNewTerminal, "-", MenuCloseAll, MenuHideProject}; !slices.Equal(got, want) {
		t.Fatalf("menu %q, want %q", got, want)
	}
	if err := h.tt.ChooseMenuItem(MenuNewOrchestrator); err != nil {
		t.Fatal(err)
	}
	if want := projectRoot(g); !slices.Equal(started, []string{want}) || want == "" {
		t.Errorf("started %v, want [%s]", started, want)
	}
}

func TestProjectMenuNewTerminalOpensInThatProject(t *testing.T) {
	h, _ := emptyProjectRig(t)
	var got []string
	h.list.OnNewTerminalIn = func(project, dir string) { got = append(got, project, dir) }
	h.tt.Frame()
	if err := h.tt.RightClick(ProjectLabel(h.list.groups[len(h.list.groups)-1])); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(MenuNewTerminal); err != nil {
		t.Fatal(err)
	}
	if want := []string{"gamma", "/proj/gamma"}; !slices.Equal(got, want) {
		t.Errorf("opened %v, want %v", got, want)
	}
}

func TestProjectMenuStartsAreOffWithNoHandler(t *testing.T) {
	h, _ := emptyProjectRig(t)
	h.list.OnNewIn, h.list.OnNewTerminalIn = nil, nil
	h.tt.Frame()
	h.tt.RightClick(ProjectLabel(h.list.groups[len(h.list.groups)-1]))
	for _, item := range []string{MenuNewOrchestrator, MenuNewTerminal} {
		if err := h.tt.ChooseMenuItem(item); err == nil {
			t.Errorf("%s could be chosen with no handler", item)
		}
	}
}

func TestAClickOnAnEmptyProjectSelectsIt(t *testing.T) {
	h, _ := emptyProjectRig(t)
	if _, _, ok := h.list.SelectedProject(); ok {
		t.Fatal("a project row is selected before the click")
	}
	if err := h.tt.Click(ProjectLabel(h.list.groups[len(h.list.groups)-1])); err != nil {
		t.Fatal(err)
	}
	if name, dir, ok := h.list.SelectedProject(); !ok || name != "gamma" || dir != "/proj/gamma" {
		t.Errorf("selected %q %q %v, want gamma /proj/gamma", name, dir, ok)
	}
}
