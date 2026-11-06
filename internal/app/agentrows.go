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

// modelFor is the model of line 2: the profile's, else the --model of the
// program's own command line (an orchestrator contr did not start, D169).
func (l *SpacesList) modelFor(w agents.Worker) string {
	if m := l.modelOf(w); m != "" {
		return m
	}
	if w.Model != "" {
		return w.Model
	}
	if l.StatusModel != nil {
		return l.StatusModel(w.Address)
	}
	return ""
}

// issueTitle is the title of the issue of a worker row, "" when none is known (D174).
func (l *SpacesList) issueTitle(a agentLine) string {
	if a.orchestrator {
		return ""
	}
	return l.Titles.Title(a.w.Worktree, a.w.Issue)
}

// titleOf is line 1 of an agent row. A worker shows the title of its issue
// (D174); with none known, its task (the terminal title); with none usable,
// its name. An orchestrator shows its name.
func titleOf(a agentLine, issueTitle string) string {
	if a.orchestrator {
		return a.w.DisplayName()
	}
	if issueTitle != "" {
		return issueTitle
	}
	if a.w.Task != "" {
		return a.w.Task
	}
	return a.w.DisplayName()
}

// nameOf is the name on line 2 of a worker row: "b-241" in "b-241  sonnet".
// It is left out when line 1 already shows it, and for an orchestrator.
func nameOf(a agentLine, issueTitle string) string {
	if a.orchestrator || titleOf(a, issueTitle) == a.w.DisplayName() {
		return ""
	}
	return a.w.DisplayName()
}

// AgentLabel is the label of an agent row: "<title>, <state>, <issue>, <model>".
func AgentLabel(w agents.Worker, issue, model string) string {
	parts := []string{w.DisplayName(), tabState(w).Label()}
	for _, p := range []string{issue, model} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// rowTip is the tooltip of an agent row: the name. A terminal contr did not start also shows its
// hc address, which is no longer its name (D156).
func rowTip(w agents.Worker) string {
	n := w.DisplayName()
	if w.Foreign && w.Address != "" && n != w.Address {
		return n + " · " + w.Address
	}
	return n
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
	issue, model, it := issueText(a), l.modelFor(a.w), l.issueTitle(a)
	ci := !a.orchestrator && a.w.Live && a.w.Status != "working" && l.Entry.PRs.Waiting(a.w.Worktree)
	label := AgentLabel(a.w, issue, model)
	if ci {
		label += ", waiting on CI"
	}
	o := kit.AgentRowOpts{
		CI: ci, Label: label, Title: titleOf(a, it), Name: nameOf(a, it), Model: model,
		Age: agentAge(a.w, l.clock()), State: tabState(a.w), Orchestrator: a.orchestrator,
		OnClick: func() { l.SelectAgent(a) },
		Menu:    func(m *ui.Menu) { l.agentMenu(m, a) },
		Compact: l.compact, Tip: rowTip(a.w),
	}
	if l.selection != nil {
		if sel, ok := l.selection.Current(); ok {
			o.Selected = sel.ID == a.w.Address && sel.Kind != state.KindWorktree
		}
	}
	kit.AgentRow(c, o)
}

// SelectAgent is a click on an agent row: the agent becomes the selection,
// and the tabs, the terminal and the files panel follow it. The terminal then
// takes the focus (OnPick), also when the row was already selected.
func (l *SpacesList) SelectAgent(a agentLine) {
	if l.selection == nil {
		return
	}
	kind := state.KindWorker
	if a.orchestrator {
		kind = state.KindOrchestrator
	}
	l.selection.Select(state.Selected{Kind: kind, ID: a.w.Address})
	if l.OnPick != nil {
		l.OnPick()
	}
}

// agentMenu fills the context menu of an agent row: Interrupt, Stop, Open PR.
// The actions run off the UI thread. Open PR is off for an agent with no pull
// request. Open PR opens the browser, not the PR pane (D127).
func (l *SpacesList) agentMenu(m *ui.Menu, a agentLine) {
	if m.Item(MenuInterrupt).Disabled(l.Source == nil).Chosen() {
		l.act(a.w, "interrupted", l.Source.Interrupt)
	}
	if m.Item(MenuStop).Disabled(l.Source == nil).Chosen() {
		l.act(a.w, "stopped", l.Source.Stop)
	}
	m.Separator()
	var open func()
	if n := l.Entry.PRs.PR(a.w.Worktree); n > 0 && a.w.Worktree != "" && l.Entry.OpenPRWeb != nil {
		open = func() { l.Entry.OpenPRWeb(a.w.Worktree, n) }
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

// itemSelected tells whether the selection sel shows on the row it. An open
// project shows the selection on the agent row; the project row shows it for
// the project itself and while the project is folded.
func (l *SpacesList) itemSelected(it sideItem, sel state.Selected) bool {
	if a := it.agent; a != nil {
		// A plain terminal that now runs Claude Code keeps its terminal
		// selection (D190), so the kind is not checked.
		return sel.ID == a.w.Address && sel.Kind != state.KindWorktree
	}
	g := l.groups[it.group]
	open := !l.Folded(g.CommonDir)
	return l.holds(g, sel) && (sel.Kind == state.KindWorktree || !open)
}

// selectedIndex is the row that shows the selection, or -1.
func (l *SpacesList) selectedIndex(items []sideItem) int {
	if l.selection == nil {
		return -1
	}
	sel, ok := l.selection.Current()
	if !ok {
		return -1
	}
	for i, it := range items {
		if l.itemSelected(it, sel) {
			return i
		}
	}
	return -1
}

// target is the selection that a row stands for: an agent, a project's
// orchestrator, else the project folder.
func (l *SpacesList) target(it sideItem) state.Selected {
	if a := it.agent; a != nil {
		kind := state.KindWorker
		if a.orchestrator {
			kind = state.KindOrchestrator
		}
		return state.Selected{Kind: kind, ID: a.w.Address}
	}
	g := l.groups[it.group]
	for _, o := range l.snapshot().Orchestrators {
		if l.holdsAddress(g, o.Address) {
			return state.Selected{Kind: state.KindOrchestrator, ID: o.Address}
		}
	}
	return state.Selected{Kind: state.KindWorktree, ID: projectRoot(g)}
}

// syncCursor keeps the key cursor on its row while that row still stands for
// the selection (a project row stands for its orchestrator, which an open
// project shows on the agent row); else it moves to the row that shows the
// selection.
func (l *SpacesList) syncCursor(items []sideItem) {
	if l.selection != nil && l.cursor >= 0 && l.cursor < len(items) {
		if sel, ok := l.selection.Current(); ok && l.target(items[l.cursor]) == sel {
			return
		}
	}
	l.cursor = l.selectedIndex(items)
}

// choose makes a row the selection, as the arrow keys do. A project row does
// not fold: that stays a click on the selected project.
func (l *SpacesList) choose(it sideItem) {
	if l.selection == nil {
		return
	}
	if l.ByKey != nil {
		l.ByKey(func() { l.selection.Select(l.target(it)) })
		return
	}
	l.selection.Select(l.target(it))
}

func (l *SpacesList) groupIndex(g spaces.RepoGroup) int {
	for i := range l.groups {
		if l.groups[i].CommonDir == g.CommonDir {
			return i
		}
	}
	return 0
}

// move is Up (-1) or Down (+1) in the list: the row next to the cursor becomes
// the selection. The first and last rows stay where they are.
func (l *SpacesList) move(items []sideItem, d int) {
	if len(items) == 0 {
		return
	}
	next := l.cursor + d
	if l.cursor < 0 {
		next = 0 // nothing selected: the first key chooses the first row
	}
	if next < 0 || next >= len(items) {
		return
	}
	l.cursor = next
	l.choose(items[next])
}

// open is Enter in the list: the row under the cursor becomes the selection
// and the terminal takes the focus.
func (l *SpacesList) open(items []sideItem) {
	if l.cursor < 0 || l.cursor >= len(items) {
		return
	}
	if it := items[l.cursor]; it.agent == nil {
		// An empty project has no terminal to focus, and Enter starts nothing: its menu
		// (menu key, right-click) offers the new orchestrator (#443, D174).
		if l.groups[it.group].Path != "" {
			return
		}
	}
	l.choose(items[l.cursor])
	if l.OnOpen != nil {
		l.OnOpen()
	}
}
