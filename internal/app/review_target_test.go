package app

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/state"
)

func TestReviewTargetOfHcTabs(t *testing.T) {
	o := apptest.OrchW("o", "lead", "p")
	o.Worktree = "/wt/orch"
	w := apptest.WorkW("w", "b1", "p")
	w.Worktree = "/wt/work"
	term := agents.Worker{Address: "t", Name: "sh", Project: "p", Role: "terminal", Live: true, Worktree: "/wt/term"}
	snap := agentstate.Snapshot{
		Orchestrators: []agents.Worker{o},
		Workers:       []agents.Worker{w},
		Terminals:     []agents.Worker{term},
	}
	f := &Frame{Selection: state.NewSelection(nil)}
	f.agentSnap = func() agentstate.Snapshot { return snap }
	f.takeSnapshot()

	cases := []struct {
		name string
		sel  state.Selected
		want string
	}{
		{"orchestrator", state.Selected{Kind: state.KindOrchestrator, ID: "o"}, "/wt/orch"},
		{"worker", state.Selected{Kind: state.KindWorker, ID: "w"}, "/wt/work"},
		{"terminal", state.Selected{Kind: state.KindTerminal, ID: "t"}, "/wt/term"},
		{"unknown address", state.Selected{Kind: state.KindWorker, ID: "nope"}, ""},
	}
	for _, c := range cases {
		f.Selection.Select(c.sel)
		if got := f.reviewTarget(); got != c.want {
			t.Errorf("%s: reviewTarget = %q, want %q", c.name, got, c.want)
		}
	}

	f.agentSnap = nil
	f.takeSnapshot()
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w"})
	if got := f.reviewTarget(); got != "" {
		t.Errorf("nil agentSnap: reviewTarget = %q, want empty", got)
	}
}

// contextLabels gives the Stop hook's context of the selected terminal, and
// nothing for a worktree, an unknown address or no agents (D205).
func TestContextLabelsOfTheSelectedTerminal(t *testing.T) {
	o := apptest.OrchW("o", "lead", "p")
	o.ContextTokens, o.ContextLimit = 90000, 160000
	w := apptest.WorkW("w", "b1", "p")
	snap := agentstate.Snapshot{Orchestrators: []agents.Worker{o}, Workers: []agents.Worker{w}}
	f := &Frame{Selection: state.NewSelection(nil)}
	f.agentSnap = func() agentstate.Snapshot { return snap }
	f.takeSnapshot()

	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	if got := f.contextLabels(); got.Tokens != 90000 || got.Size != 160000 {
		t.Errorf("orchestrator: %+v", got)
	}
	for _, sel := range []state.Selected{
		{Kind: state.KindWorker, ID: "w"},
		{Kind: state.KindWorker, ID: "nope"},
		{Kind: state.KindWorktree, ID: "o"},
	} {
		f.Selection.Select(sel)
		if got := f.contextLabels(); got.Size != 0 {
			t.Errorf("%+v: %+v, want none", sel, got)
		}
	}
	f.agentSnap = nil
	f.takeSnapshot()
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "o"})
	if got := f.contextLabels(); got.Size != 0 {
		t.Errorf("nil agentSnap: %+v, want none", got)
	}
}
