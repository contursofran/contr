package app

// The agent rows of the sidebar (docs/spec/AgentRow.md): under each project,
// its orchestrator first, then its workers on the rail. They sit in the same
// ui.List as the project rows. The ages move through the AgeClock (D137).

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
)

// Labels of the context menu of an agent row.
const (
	MenuInterrupt = "Interrupt"
	MenuStop      = "Stop"
	MenuOpenPR    = "Open PR"
)

// AgentActions is what the context menu of an agent row calls: the agent
// source (internal/agents behind agentstate). Both take the hc address.
type AgentActions interface {
	Interrupt(address string) error
	Stop(address string) error
}

// agentLine is one agent row of a project.
type agentLine struct {
	w            agents.Worker
	orchestrator bool
}

// sideItem is one row of the sidebar list: a project (agent is nil) or one of
// its agents.
type sideItem struct {
	group int
	agent *agentLine
}

// workerIcon is the icon for a worker. A program that ended shows done.
func workerIcon(w agents.Worker) icons.Status {
	switch {
	case !w.Live:
		return icons.Done
	case w.Status == "working":
		return icons.Working
	case w.Status == "blocked" || w.Status == "error":
		return icons.Blocked
	case w.Status == "done":
		return icons.Done
	}
	return icons.Idle
}

// AgeText is the time since t as "now", "3m" or "2h"; days go on as hours.
// Ages move once a minute (D137), so there is no seconds step. A time in the
// future, or the zero time, gives "".
func AgeText(now, t time.Time) string {
	if t.IsZero() || t.After(now) {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Hour)) + "h"
}

// agentLines are the agents of the project g: the orchestrators first, each
// followed by its workers; the workers of no orchestrator of the project go
// under the first one, or alone when there is none. An ended worker is left
// out (D112), and plain terminals are not agents.
func agentLines(g spaces.RepoGroup, s agentstate.Snapshot) []agentLine {
	in := map[string]bool{}
	for _, r := range g.Rows {
		for _, h := range r.Holders {
			in[h.Address] = true
		}
	}
	var orchs, loose []agents.Worker
	byParent := map[string][]agents.Worker{}
	for _, o := range s.Orchestrators {
		if in[o.Address] {
			orchs = append(orchs, o)
		}
	}
	isOrch := map[string]bool{}
	for _, o := range orchs {
		isOrch[o.Address] = true
	}
	for _, w := range s.Workers {
		if !in[w.Address] || !w.Live {
			continue
		}
		if isOrch[w.Parent] {
			byParent[w.Parent] = append(byParent[w.Parent], w)
		} else {
			loose = append(loose, w)
		}
	}
	var out []agentLine
	for i, o := range orchs {
		out = append(out, agentLine{w: o, orchestrator: true})
		kids := byParent[o.Address]
		if i == 0 {
			kids = append(kids, loose...)
		}
		for _, w := range kids {
			out = append(out, agentLine{w: w})
		}
	}
	if len(orchs) == 0 {
		for _, w := range loose {
			out = append(out, agentLine{w: w})
		}
	}
	return out
}

// items are the rows of the list: each project, then its agents unless the
// project is folded.
func (l *SpacesList) items() []sideItem {
	s := l.snapshot()
	var out []sideItem
	for i, g := range l.groups {
		out = append(out, sideItem{group: i})
		if l.folded[g.CommonDir] {
			continue
		}
		for _, a := range agentLines(g, s) {
			out = append(out, sideItem{group: i, agent: &a})
		}
	}
	return out
}

// agentAge is the age of an agent as of now.
func agentAge(w agents.Worker, now time.Time) string { return AgeText(now, w.LastOutput) }

func (l *SpacesList) clock() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *SpacesList) modelOf(w agents.Worker) string {
	if l.ModelOf == nil {
		return ""
	}
	return l.ModelOf(w.Profile)
}

// AgentLabel is the label of an agent row: "<title>, <state>, <issue>, <model>".
func AgentLabel(w agents.Worker, issue, model string) string {
	parts := []string{w.Name, tabState(w).Label()}
	for _, p := range []string{issue, model} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// issueText is the issue id of line 2: the label of the worker, as set. An
// orchestrator has none.
func issueText(a agentLine) string {
	if a.orchestrator {
		return ""
	}
	return a.w.Issue
}

// agentRow is one AgentRow.
func (l *SpacesList) agentRow(c *ui.Context, a agentLine) {
	issue, model := issueText(a), l.modelOf(a.w)
	o := kit.AgentRowOpts{
		Label: AgentLabel(a.w, issue, model), Title: a.w.Name, Issue: issue, Model: model,
		Age: agentAge(a.w, l.clock()), State: tabState(a.w), Orchestrator: a.orchestrator,
		OnClick: func() { l.SelectAgent(a) },
		Menu:    func(m *ui.Menu) { l.agentMenu(m, a) },
	}
	if l.selection != nil {
		if sel, ok := l.selection.Current(); ok {
			o.Selected = sel.ID == a.w.Address && (sel.Kind == state.KindOrchestrator || sel.Kind == state.KindWorker)
		}
	}
	kit.AgentRow(c, o)
}

// SelectAgent is a click on an agent row: the agent becomes the selection,
// and the tabs, the terminal and the files panel follow it.
func (l *SpacesList) SelectAgent(a agentLine) {
	if l.selection == nil {
		return
	}
	kind := state.KindWorker
	if a.orchestrator {
		kind = state.KindOrchestrator
	}
	l.selection.Select(state.Selected{Kind: kind, ID: a.w.Address})
}

// agentMenu fills the context menu of an agent row: Interrupt, Stop, Open PR.
// The actions run off the UI thread. Open PR is off for an agent with no pull
// request.
func (l *SpacesList) agentMenu(m *ui.Menu, a agentLine) {
	if m.Item(MenuInterrupt).Disabled(l.Source == nil).Chosen() {
		l.act(a.w, "interrupted", l.Source.Interrupt)
	}
	if m.Item(MenuStop).Disabled(l.Source == nil).Chosen() {
		l.act(a.w, "stopped", l.Source.Stop)
	}
	m.Separator()
	var open func()
	if n := l.Entry.PRs.PR(a.w.Worktree); n > 0 && a.w.Worktree != "" && l.Entry.OpenPR != nil {
		open = func() { l.Entry.OpenPR(a.w.Worktree, n) }
	}
	if m.Item(MenuOpenPR).Disabled(open == nil).Chosen() && open != nil {
		open()
	}
}

func (l *SpacesList) act(w agents.Worker, done string, do func(string) error) {
	l.Run.Do(func() func() {
		err := do(w.Address)
		return func() {
			if l.Status == nil {
				return
			}
			if err != nil {
				l.Status(err.Error())
				return
			}
			l.Status(w.Name + " " + done)
		}
	})
}

// syncAges tells the age clock whether an age is on screen: an agent row of
// an open project has one. It runs when the rows change.
func (l *SpacesList) syncAges() {
	if l.Ages == nil {
		return
	}
	now := l.clock()
	for _, it := range l.items() {
		if it.agent != nil && agentAge(it.agent.w, now) != "" {
			l.Ages.SetShown(true)
			return
		}
	}
	l.Ages.SetShown(false)
}

// dropGoneSelection follows a change of the state: a selected agent that is
// gone leaves the selection. Other kinds of selection stay.
func (l *SpacesList) dropGoneSelection() {
	if l.selection == nil {
		return
	}
	sel, ok := l.selection.Current()
	if !ok || (sel.Kind != state.KindOrchestrator && sel.Kind != state.KindWorker) {
		return
	}
	s := l.snapshot()
	list := s.Workers
	if sel.Kind == state.KindOrchestrator {
		list = s.Orchestrators
	}
	for _, w := range list {
		if w.Address == sel.ID {
			return
		}
	}
	l.selection.Clear()
}
