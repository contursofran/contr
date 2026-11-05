package app

// The model of the title bar tabs (docs/spec/Tab.md, TitleBar.md): the selected agent's own
// hc terminal, then the plain terminals of its project. This file has no UI.

import (
	"path/filepath"
	"slices"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
)

// Tab is one tab of the title bar.
type Tab struct {
	// Address is the hc terminal address. It is the Selection ID.
	Address string
	// Title is the text shown.
	Title        string
	Orchestrator bool
	// Plain is true for a plain terminal (a shell, not an agent). Only a plain tab closes (D85).
	Plain bool
	// State is the glyph state of an agent tab. A plain tab shows the terminal icon.
	State    kit.GlyphState
	Selected bool
}

// Closable tells whether the tab has a close button: a plain terminal, not an agent (D85).
// Middle-click and the menu close only a closable tab too.
func (t Tab) Closable() bool { return t.Plain }

// Movable tells whether the tab can be dragged to another place: a plain terminal. The agent's
// own tab stays first.
func (t Tab) Movable() bool { return t.Plain }

// TitleTabs returns the tabs for agent, the address of the agent whose tabs show: its own tab
// first, then the plain terminals of project. selected is the address of the selected tab, or "".
// An agent that is not in the snapshot, or an ended worker, has no tab (D112).
func TitleTabs(s agentstate.Snapshot, project, key, agent, selected string) []Tab {
	var tabs []Tab
	if w, ok := findAgent(s, agent); ok {
		tabs = append(tabs, Tab{Address: w.Address, Title: tabTitle(w), Orchestrator: w.Orchestrator(),
			State: tabState(w), Selected: selected != "" && w.Address == selected})
	}
	for _, w := range s.Terminals {
		if agentstate.SameProject(w, project, key) {
			tabs = append(tabs, Tab{Address: w.Address, Title: tabTitle(w), Plain: true,
				Selected: selected != "" && w.Address == selected})
		}
	}
	return tabs
}

// findAgent returns the orchestrator or live worker with an address.
func findAgent(s agentstate.Snapshot, addr string) (agents.Worker, bool) {
	if addr == "" {
		return agents.Worker{}, false
	}
	for _, w := range s.Orchestrators {
		if w.Address == addr {
			return w, true
		}
	}
	for _, w := range s.Workers {
		if w.Address == addr && w.Live {
			return w, true
		}
	}
	return agents.Worker{}, false
}

// tabState is the glyph state of an agent: a program that ended is idle.
func tabState(w agents.Worker) kit.GlyphState {
	switch {
	case !w.Live:
		return kit.GlyphIdle
	case w.Status == "blocked" || w.Status == "error":
		return kit.GlyphNeedsYou
	case w.Status == "working":
		return kit.GlyphWorking
	}
	return kit.GlyphIdle
}

// tabTitle is the text of a tab: a plain terminal shows its title (set by the shell with OSC),
// else "$ <folder>"; an orchestrator shows its name; a worker shows "<issue> · <worktree>", or
// its name when it has no issue or no worktree.
func tabTitle(w agents.Worker) string {
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

// TabAgent returns the address of the agent whose tabs show. A selected orchestrator or live
// worker is it. Any other selection (a plain terminal, a worktree, none) keeps keep while it is
// still an agent of project; else it is the first orchestrator of project, then its first live
// worker, then "".
func TabAgent(s agentstate.Snapshot, sel state.Selected, ok bool, project, key, keep string) string {
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker) {
		if _, found := findAgent(s, sel.ID); found {
			return sel.ID
		}
	}
	if w, found := findAgent(s, keep); found && agentstate.SameProject(w, project, key) {
		return keep
	}
	for _, w := range s.Orchestrators {
		if agentstate.SameProject(w, project, key) {
			return w.Address
		}
	}
	for _, w := range s.Workers {
		if agentstate.SameProject(w, project, key) && w.Live {
			return w.Address
		}
	}
	return ""
}

// OpenProject returns the project the bar shows. A selected orchestrator or worker or plain
// terminal that is in the snapshot gives its project. Any other selection, or none, keeps keep.
// When keep is empty (at start) it is the project of the first orchestrator, or "".
func OpenProject(s agentstate.Snapshot, sel state.Selected, ok bool, keep string) string {
	name, _ := OpenProjectKey(s, sel, ok, keep, "")
	return name
}

// OpenProjectKey is OpenProject with the project key (the git common folder) that goes with the
// name. keepKey is the last key; it is kept when the name is kept.
func OpenProjectKey(s agentstate.Snapshot, sel state.Selected, ok bool, keep, keepKey string) (string, string) {
	if ok && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker || sel.Kind == state.KindTerminal) {
		for _, list := range [][]agents.Worker{s.Orchestrators, s.Workers, s.Terminals} {
			for _, w := range list {
				if w.Address == sel.ID {
					return w.Project, w.ProjectKey
				}
			}
		}
	}
	if keep == "" && len(s.Orchestrators) > 0 {
		return s.Orchestrators[0].Project, s.Orchestrators[0].ProjectKey
	}
	return keep, keepKey
}

// OrderTabs puts the plain tabs in the order of order, a list of addresses. The agent's own tab
// stays first. A plain tab that order does not name goes after the ones it names, in the order it
// came in. The result is a new slice.
func OrderTabs(tabs []Tab, order []string) []Tab {
	out := make([]Tab, 0, len(tabs))
	var plain []Tab
	for _, t := range tabs {
		if t.Plain {
			plain = append(plain, t)
		} else {
			out = append(out, t)
		}
	}
	rank := func(t Tab) int {
		if i := slices.Index(order, t.Address); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(plain, func(a, b Tab) int { return rank(a) - rank(b) })
	return append(out, plain...)
}

// MoveTab returns the addresses of the plain tabs after the plain tab from takes the place of the
// plain tab to: a tab that moves right goes after to, one that moves left goes before it. The
// agent's own tab is not in the list and never moves. ok is false, and nothing moves, when from
// or to is not a plain tab of tabs, or they are the same tab.
func MoveTab(tabs []Tab, from, to string) (order []string, ok bool) {
	var plain []string
	for _, t := range tabs {
		if t.Plain {
			plain = append(plain, t.Address)
		}
	}
	i, j := slices.Index(plain, from), slices.Index(plain, to)
	if i < 0 || j < 0 || i == j {
		return nil, false
	}
	plain = slices.Delete(plain, i, i+1)
	plain = slices.Insert(plain, j, from)
	return plain, true
}
