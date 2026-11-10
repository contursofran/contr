package sidebar

// The item badge of an orchestrator row (D240).

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/owner"
)

// badgeSnap is the snapshot of newRows with a second orchestrator lead2 and
// its worker b-300.
func badgeSnap(h *rowsHarness) agentstate.Snapshot {
	snap := h.live.get()
	snap.Orchestrators = append(snap.Orchestrators, orchestratorIn("lead2", h.cwds[0]))
	snap.Workers = append(snap.Workers, builder("b-300", "lead2", h.cwds[0], "300", time.Minute))
	return snap
}

func TestTheOrchestratorRowCountsItsItemsAndThoseOfItsWorkers(t *testing.T) {
	h := newRows(t)
	snap := badgeSnap(h)
	// b-300 is blocked at a menu: lead2's.
	for i, w := range snap.Workers {
		if w.Name == "b-300" {
			snap.Workers[i].Status, snap.Workers[i].Project = "blocked", "alpha"
		}
	}
	// An ended worker of lead2 asked before it ended.
	snap.Workers = append(snap.Workers, agents.Worker{Name: "b-301", Address: "b-301", Parent: "lead2", Worktree: h.cwds[0]})
	snap.Questions = []owner.Question{
		{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "lead asks"},
		{ID: "D2", Project: "alpha", Coordinator: "b-300", Question: "a worker of lead2 asks"},
		{ID: "D3", Project: "alpha", Question: "no terminal: the first orchestrator"},
		{ID: "D4", Project: "beta", Coordinator: "lead", Question: "another project"},
		{ID: "D5", Project: "alpha", Coordinator: "b-301", Question: "an ended worker of lead2"},
		{ID: "D6", Project: "alpha", Coordinator: "lead", Question: "resolved"},
	}
	snap.Todos = []owner.Todo{{ID: "T1", Project: "alpha", Coordinator: "lead2", Kind: "do", Text: "merge"}}
	snap.Resolved = []string{"D6"}
	h.send(snap)
	// lead: D1 and D3. lead2: D2, D5, T1 and the menu of b-300.
	h.y("lead, idle, opus, 2 items for you")
	h.y("lead2, idle, opus, 4 items for you")
	if !h.tt.HasText("4") {
		t.Errorf("no badge number; texts %q", h.tt.Texts())
	}
}

func TestTheBadgeShowsOneItemAndGoesAtZero(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Questions = []owner.Question{{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "q"}}
	h.send(snap)
	h.y("lead, idle, opus, 1 item for you")
	if _, ok := h.tt.Find(kit.LabelBadge); !ok {
		t.Fatal("no badge for one item")
	}
	snap.Questions = nil
	h.send(snap)
	h.y("lead, idle, opus")
	if _, ok := h.tt.Find(kit.LabelBadge); ok {
		t.Error("the badge stays at 0")
	}
}

func TestAWorkerRowHasNoBadge(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Questions = []owner.Question{{ID: "D1", Project: "alpha", Coordinator: "b-241", Question: "q"}}
	h.send(snap)
	h.y("lead, idle, opus, 1 item for you")
	h.y("b-241, working, 241, sonnet")
}

func TestNoBadgeWhileTheQuestionsFileIsBroken(t *testing.T) {
	h := newRows(t)
	snap := h.live.get()
	snap.Questions = []owner.Question{{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "q"}}
	snap.OwnerProblem = "owner.json: bad"
	h.send(snap)
	if _, ok := h.tt.Find(kit.LabelBadge); ok {
		t.Error("a badge shows while the questions file is broken")
	}
}

func TestTheBadgeSpringsOnArrivalThenAsksForNoFrame(t *testing.T) {
	h, cwds := twoRepos(t)
	// One idle orchestrator: no glyph moves, so only the badge can ask for frames.
	snap := agentstate.Snapshot{CoreVersion: "0.1.18",
		Orchestrators: []agents.Worker{orchestratorIn("lead", cwds[0])},
		Workers:       []agents.Worker{apptest.SpPane("w1:p2", cwds[1], nil)}}
	h.send(snap)
	h.tt.Advance(time.Second)
	if h.tt.FramePending() {
		t.Fatal("the sidebar asks for frames before any item")
	}
	snap.Questions = []owner.Question{{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "q"}}
	h.send(snap)
	if !h.tt.FramePending() {
		t.Error("an arrival does not move the badge")
	}
	h.tt.Advance(kit.BadgeAttentionLength + time.Second)
	if h.tt.FramePending() {
		t.Error("the badge asks for frames after its effect")
	}
	snap.Questions = snap.Questions[:0]
	snap.Questions = append(snap.Questions, owner.Question{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "q"},
		owner.Question{ID: "D2", Project: "alpha", Coordinator: "lead", Question: "q2"})
	h.send(snap)
	if !h.tt.FramePending() {
		t.Error("a second arrival does not move the badge")
	}
	h.tt.Advance(kit.BadgeAttentionLength + time.Second)
	snap.Questions = snap.Questions[:1]
	h.send(snap)
	if h.tt.FramePending() {
		t.Error("a lower count moves the badge")
	}
}

func TestTheIconColumnShowsADotForItems(t *testing.T) {
	h := compactRows(t)
	snap := h.live.get()
	snap.Questions = []owner.Question{{ID: "D1", Project: "alpha", Coordinator: "lead", Question: "q"}}
	h.send(snap)
	if _, ok := h.tt.Find(kit.LabelBadgeDot); !ok {
		t.Error("no dot on the orchestrator icon")
	}
	if _, ok := h.tt.Find(kit.LabelBadge); ok {
		t.Error("the icon column shows the badge")
	}
	snap.Questions = nil
	h.send(snap)
	if _, ok := h.tt.Find(kit.LabelBadgeDot); ok {
		t.Error("the dot stays at 0")
	}
}
