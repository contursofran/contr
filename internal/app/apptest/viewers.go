package apptest

import (
	"context"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/owner"
)

// PRRunner is a fake gh runner: it answers pr view with a canned reply per number.
type PRRunner struct {
	Reply func(n string) ([]byte, error)
	Dirs  []string
}

// Run records dir and gives the reply of the number in args.
func (r *PRRunner) Run(_ context.Context, dir string, args ...string) ([]byte, error) {
	r.Dirs = append(r.Dirs, dir)
	return r.Reply(args[2])
}

// PRJSON is the reply of `gh pr view 642`: an open, approved PR with green checks.
const PRJSON = `{"number":642,"title":"Fix bag drag target","url":"https://example.test/o/client/pull/642",
"state":"OPEN","body":"## Why\n\nDrops land under the cursor.\n- one\n- two","author":{"login":"b-1140"},
"headRefName":"fix/bag-drag","baseRefName":"main","headRefOid":"abc","additions":86,"deletions":12,"changedFiles":3,
"isDraft":false,"mergeable":"MERGEABLE","mergedAt":null,"closedAt":null,
"comments":[{"author":{"login":"alice"},"body":"Thanks\nsecond line","createdAt":"2026-11-03T11:50:00Z"}],
"reviews":[{"author":{"login":"r-1139"},"body":"","state":"APPROVED","submittedAt":"2026-11-03T11:46:00Z"}],
"commits":[],"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"}]}`

// BoardSnap is a snapshot of project a for the board: an orchestrator, three workers on two
// issues, a worker of another project, a question and a to-do.
func BoardSnap() agentstate.Snapshot {
	return agentstate.Snapshot{
		Orchestrators: []agents.Worker{{Name: "lead", Address: "o1", Role: "orchestrator", Project: "a", Live: true}},
		Workers: []agents.Worker{
			{Name: "p-1", Address: "t1", Role: "planner", Issue: "162", Title: "Plan the board", Project: "a", Live: true, Status: "done"},
			{Name: "b-1", Address: "t2", Role: "builder", Issue: "162", Project: "a", Live: true, Status: "working"},
			{Name: "r-1", Address: "t3", Role: "reviewer", Issue: "#170", Project: "a", Live: true, Status: "blocked", Msg: "ok?"},
			{Name: "b-9", Address: "t4", Role: "builder", Project: "other", Live: true, Status: "idle"},
		},
		Questions: []owner.Question{{ID: "D7", Project: "a", From: "b-1", Question: "Which?"}},
		Todos:     []owner.Todo{{ID: "T3", Project: "a", Kind: "do", Text: "Run it"}},
	}
}
