package agentstate

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
)

func TestSnapshotSetsTheTaskOfAWorkerFromItsTitle(t *testing.T) {
	m := &Model{workers: []agents.Worker{
		{Name: "b-1", Address: "w1", Role: "builder", Live: true, Title: "✳ Agent rows on ui.List"},
		{Name: "b-2", Address: "w2", Role: "builder", Live: true, Title: "~/work/tree"},
		{Name: "b-3", Address: "w3", Role: "builder", Live: true},
	}}
	got := map[string]string{}
	for _, w := range m.Snapshot().Workers {
		got[w.Address] = w.Task
	}
	if got["w1"] != "Agent rows on ui.List" || got["w2"] != "" || got["w3"] != "" {
		t.Errorf("tasks %q", got)
	}
}
