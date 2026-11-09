package app

// The agent state that the frame keeps between changes (#565). The source is
// read once per change, on the UI thread; views and frame helpers read the
// stored copy, so a draw reads no source and changes no frame state.

import (
	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
)

// storedSource is an [AgentSource] whose Snapshot is the frame's stored snapshot. The actions go
// to the source.
type storedSource struct {
	AgentSource
	snap func() agentstate.Snapshot
}

// Snapshot returns the stored snapshot.
func (s storedSource) Snapshot() agentstate.Snapshot { return s.snap() }

// snapshot is the agent state of the last change. UI thread.
func (f *Frame) snapshot() agentstate.Snapshot { return f.snap }

// takeSnapshot reads the source once, indexes the agents by address and computes the open
// project and the tabs again. syncAgents calls it after each change. UI thread.
func (f *Frame) takeSnapshot() {
	f.snap, f.byAddr = agentstate.Snapshot{}, nil
	if f.agentSnap != nil {
		f.snap = f.agentSnap()
		f.byAddr = make(map[string]agents.Worker)
		for _, ws := range [][]agents.Worker{f.snap.Orchestrators, f.snap.Workers, f.snap.Terminals} {
			for _, w := range ws {
				if _, dup := f.byAddr[w.Address]; !dup { // the first one wins, as in a scan
					f.byAddr[w.Address] = w
				}
			}
		}
	}
	f.syncOpen()
}

// syncOpen computes the open project, its key, the tab agent and the title bar tabs from the
// stored snapshot and the selection. It runs after an agent change, a selection change and a tab
// move. The open project follows the selected orchestrator, worker or plain terminal, and keeps
// its last value for any other selection; the agent keeps its last value while a plain terminal
// or a worktree is selected. Without agents nothing changes and there are no tabs. UI thread.
func (f *Frame) syncOpen() {
	if f.agentSnap == nil {
		f.tabs = nil
		return
	}
	var sel state.Selected
	var ok bool
	if f.Selection != nil {
		sel, ok = f.Selection.Current()
	}
	f.openProject, f.openKey = OpenProjectKey(f.snap, sel, ok, f.openProject, f.openKey)
	f.tabAgent = TabAgent(f.snap, sel, ok, f.openProject, f.openKey, f.tabAgent)
	selected := ""
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		selected = sel.ID
	}
	f.tabs = OrderTabs(TitleTabs(f.snap, f.openProject, f.openKey, f.tabAgent, selected), f.tabOrder)
}

// titleTabs returns the stored tabs of the selected agent; see [Frame.syncOpen].
func (f *Frame) titleTabs() []Tab { return f.tabs }

// agentAt is the orchestrator, worker or terminal at hc address id, from the stored snapshot.
func (f *Frame) agentAt(id string) (agents.Worker, bool) {
	if id == "" {
		return agents.Worker{}, false
	}
	w, ok := f.byAddr[id]
	return w, ok
}
