package sidebar

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
)

// freshLaunch is twoRepos as at the start of the app: nothing picked yet.
func freshLaunch(t *testing.T) (*spacesHarness, []string) {
	t.Helper()
	h, cwds := twoRepos(t)
	h.sel.Clear()
	h.list.launched = false
	return h, cwds
}

func TestLaunchSelectsTheFirstOrchestratorOfTheFirstProject(t *testing.T) {
	h, cwds := freshLaunch(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead-0", cwds[0]), orchestratorIn("lead-1", cwds[1])},
		Workers:       []agents.Worker{apptest.SpPane("w1:p9", cwds[0], nil)}})
	want := "lead-0"
	if !h.list.holdsAddress(h.group(0), want) {
		want = "lead-1"
	}
	sel, ok := h.sel.Current()
	if !ok || sel.Kind != state.KindOrchestrator || sel.ID != want {
		t.Errorf("selection %+v, %v; want orchestrator %s of the first project", sel, ok, want)
	}
}

func TestLaunchSelectsTheFirstWorkerWithoutOrchestrators(t *testing.T) {
	h, cwds := freshLaunch(t)
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Workers: []agents.Worker{apptest.SpPane("w1:p1", cwds[0], nil), apptest.SpPane("w1:p2", cwds[1], nil)}})
	want := "w1:p1"
	if !h.list.holdsAddress(h.group(0), want) {
		want = "w1:p2"
	}
	sel, ok := h.sel.Current()
	if !ok || sel.Kind != state.KindWorker || sel.ID != want {
		t.Errorf("selection %+v, %v; want worker %s of the first project", sel, ok, want)
	}
}

func TestLaunchStaysEmptyWithNothingLive(t *testing.T) {
	h, cwds := freshLaunch(t)
	w := apptest.SpPane("w1:p1", cwds[0], nil)
	w.Live = false
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18", Workers: []agents.Worker{w}})
	if sel, ok := h.sel.Current(); ok {
		t.Errorf("selection %+v, want none", sel)
	}
}

func TestLaunchKeepsTheOwnersSelection(t *testing.T) {
	h, cwds := freshLaunch(t)
	h.sel.Select(state.Selected{Kind: state.KindWorker, ID: "w1:p2"})
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])},
		Workers:       []agents.Worker{apptest.SpPane("w1:p2", cwds[1], nil)}})
	if sel, _ := h.sel.Current(); sel.ID != "w1:p2" {
		t.Errorf("selection %+v, want w1:p2", sel)
	}
}

func TestLaunchPicksOnlyOnce(t *testing.T) {
	h, cwds := freshLaunch(t)
	snap := agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])}}
	h.send(snap)
	h.sel.Clear()
	h.send(snap)
	if sel, ok := h.sel.Current(); ok {
		t.Errorf("selection %+v came back after the owner cleared it", sel)
	}
}

func TestLaunchWaitsForTheProjectOrder(t *testing.T) {
	h, cwds := freshLaunch(t)
	h.list.HoldLaunch = true
	h.send(agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])}})
	if sel, ok := h.sel.Current(); ok {
		t.Fatalf("selection %+v while the order loads", sel)
	}
	h.list.ReleaseLaunch()
	if sel, ok := h.sel.Current(); !ok || sel.ID != "lead" {
		t.Errorf("selection %+v, %v after release", sel, ok)
	}
}
