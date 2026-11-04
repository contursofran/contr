package app

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
)

func TestReviewTargetOfHcTabs(t *testing.T) {
	o := orchW("o", "lead", "p")
	o.Worktree = "/wt/orch"
	w := workW("w", "b1", "p")
	w.Worktree = "/wt/work"
	term := agents.Worker{Address: "t", Name: "sh", Project: "p", Role: "terminal", Live: true, Worktree: "/wt/term"}
	snap := agentstate.Snapshot{
		Orchestrators: []agents.Worker{o},
		Workers:       []agents.Worker{w},
		Terminals:     []agents.Worker{term},
	}
	f := &Frame{Selection: state.NewSelection(nil)}
	f.agentSnap = func() agentstate.Snapshot { return snap }

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
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w"})
	if got := f.reviewTarget(); got != "" {
		t.Errorf("nil agentSnap: reviewTarget = %q, want empty", got)
	}
}
