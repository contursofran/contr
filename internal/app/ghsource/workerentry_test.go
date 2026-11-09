package ghsource

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
)

func TestItemOfFindsTheWorkersIssue(t *testing.T) {
	ws := []agents.Worker{apptest.EntryWorker, {Name: "x", Worktree: "/wt/b"}}
	if got := ItemOf(ws, "/wt/a"); got != "#268" {
		t.Errorf("item %q", got)
	}
	if got := ItemOf(ws, "/wt/b"); got != "" {
		t.Errorf("item %q for a worker with no issue", got)
	}
	if got := ItemOf(ws, "/other"); got != "" {
		t.Errorf("item %q for an unknown folder", got)
	}
	if got := ItemOf(ws, ""); got != "" {
		t.Errorf("item %q for no folder", got)
	}
}
