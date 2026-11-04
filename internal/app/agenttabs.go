package app

// The model of the top bar tabs (#138, S1): one tab for each hc terminal of
// the open project. This file has no UI. The title bar reads it.

import (
	"path/filepath"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/state"
)

// AgentTab is one tab of the top bar.
type AgentTab struct {
	// Address is the hc terminal address. It is the Selection ID.
	Address string
	// Label is the text shown, without the orchestrator mark.
	Label        string
	Orchestrator bool
	// Plain is true for a plain terminal (a shell, not an agent).
	Plain bool
	Icon  icons.Status
	// Needs is the badge count. Only the first orchestrator tab has it.
	Needs    int
	Selected bool
}

// AgentTabs returns the tabs of project in bar order: the orchestrators
// first, then the workers, then the plain terminals, each in snapshot order.
// selected is the address of the selected tab, or "".
func AgentTabs(s agentstate.Snapshot, project, selected string) []AgentTab {
	var tabs []AgentTab
	add := func(w agents.Worker) {
		if w.Project != project {
			return
		}
		t := AgentTab{
			Address: w.Address, Label: tabLabel(w), Orchestrator: w.Orchestrator(), Plain: w.Plain(),
			Icon: workerIcon(w), Selected: selected != "" && w.Address == selected,
		}
		if t.Orchestrator && !hasOrchestratorTab(tabs) {
			t.Needs = tabBadge(s, project)
		}
		tabs = append(tabs, t)
	}
	for _, w := range s.Orchestrators {
		add(w)
	}
	for _, w := range s.Workers {
		add(w)
	}
	for _, w := range s.Terminals {
		add(w)
	}
	return tabs
}

func hasOrchestratorTab(tabs []AgentTab) bool {
	for _, t := range tabs {
		if t.Orchestrator {
			return true
		}
	}
	return false
}

// tabLabel is the text of a tab (owner question Q4, recommended answer): an
// orchestrator shows its name; a worker shows "<issue> · <worktree>", or its
// name when it has no issue or no worktree. A plain terminal shows its title
// (set by the shell with OSC), else "$ <folder>" (#246).
func tabLabel(w agents.Worker) string {
	if w.Plain() {
		if w.Title != "" {
			return w.Title
		}
		if w.Worktree != "" {
			return "$ " + filepath.Base(w.Worktree)
		}
		return w.Name
	}
	if w.Orchestrator() || w.Issue == "" || w.Worktree == "" {
		return w.Name
	}
	return w.Issue + " · " + filepath.Base(w.Worktree)
}

// tabBadge is the count on the orchestrator tab (owner question Q5,
// recommended answer): all the items of the needs-you row that are open.
func tabBadge(s agentstate.Snapshot, project string) int {
	return NeedsCount(s, project)
}

// NeedsCount is the number of open items of project that wait on the owner:
// blocked workers, questions and to-dos. Resolved items do not count.
func NeedsCount(s agentstate.Snapshot, project string) int {
	n := 0
	for _, t := range needItems(s, project) {
		if !t.Resolved {
			n++
		}
	}
	return n
}

// OpenProject returns the project the bar shows. A selected orchestrator or
// worker or plain terminal that is in the snapshot gives its project. Any other selection, or
// none, keeps keep. When keep is empty (at start) it is the project of the
// first orchestrator (owner question Q1, recommended answer), or "".
func OpenProject(s agentstate.Snapshot, sel state.Selected, ok bool, keep string) string {
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		for _, list := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
			for _, w := range list {
				if w.Address == sel.ID {
					return w.Project
				}
			}
		}
	}
	if keep == "" && len(s.Orchestrators) > 0 {
		return s.Orchestrators[0].Project
	}
	return keep
}
