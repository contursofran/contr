package app

// The agents sections at the bottom of the sidebar, on the agent state: an
// "Orchestrators" section on top, whatever the state of its agents, then
// "Agents N running". A row has the status icon and the name, and a second
// line "<command> · <age>". A click selects the row. The age is the time since
// the last output as of the last build: it moves on a state change or a
// repaint, never on a timer (decision D40).

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

// Element labels of the agents sections.
const (
	LabelAgents              = "sidebar agents"
	LabelAgentsHeader        = "agents header"
	LabelOrchestratorsHeader = "orchestrators header"
)

// AgentsList is the agents sections. It has no height while there is no
// agent. Methods run on the UI thread.
type AgentsList struct {
	snap      func() agentstate.Snapshot
	now       func() time.Time // the clock; tests replace it
	selection *state.Selection
}

// NewAgentsList returns the list on snap, the state's snapshot (may be nil:
// then there is nothing to show). A click sets selection (may be nil).
func NewAgentsList(snap func() agentstate.Snapshot, selection *state.Selection) *AgentsList {
	return &AgentsList{snap: snap, now: time.Now, selection: selection}
}

func (l *AgentsList) snapshot() agentstate.Snapshot {
	if l.snap == nil {
		return agentstate.Snapshot{}
	}
	return l.snap()
}

// Sync follows a change of the state: a selected agent that is gone leaves
// the selection. Other kinds of selection stay.
func (l *AgentsList) Sync() {
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

// Activate is a click on a row: it becomes the selection.
func (l *AgentsList) Activate(kind state.SelectedKind, address string) {
	if l.selection != nil {
		l.selection.Select(state.Selected{Kind: kind, ID: address})
	}
}

// AgeText is the time since t as "5s", "3m" or "2h"; days go on as hours.
// A time in the future, or the zero time, gives "".
func AgeText(now, t time.Time) string {
	if t.IsZero() || t.After(now) {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Hour)) + "h"
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

// workerSub is the second line: the role and state from the hc labels, how
// it ended when it ended, and the age of the last output. The command is the
// terminal's foreground name, which Claude Code sets to its version, so it
// shows only for a worker with no role (#212).
func workerSub(w agents.Worker, now time.Time) string {
	var parts []string
	if w.Role != "" {
		parts = append(parts, w.Role)
	} else if w.Command != "" {
		parts = append(parts, w.Command)
	}
	if s := StatusWord(w); s != "" && s != "ended" {
		parts = append(parts, s)
	}
	if !w.Live && w.ExitCode != nil {
		parts = append(parts, fmt.Sprintf("exit %d", *w.ExitCode))
	}
	if age := AgeText(now, w.LastOutput); age != "" {
		parts = append(parts, age)
	}
	return strings.Join(parts, " · ")
}

func (l *AgentsList) opts(w agents.Worker, kind state.SelectedKind, now time.Time, c *ui.Context) kit.RowOpts {
	sel, ok := state.Selected{}, false
	if l.selection != nil {
		sel, ok = l.selection.Current()
	}
	sub := workerSub(w, now)
	icon := workerIcon(w)
	label := w.Name + ", " + icon.Label()
	if sub != "" {
		label += ", " + sub
	}
	return kit.RowOpts{
		Label: label, Text: w.Name, Sub: sub,
		Selected: ok && sel.Kind == kind && sel.ID == w.Address,
		OnClick:  func() { l.Activate(kind, w.Address) },
		Lead:     func() { icons.Icon(c, icon).Shrink(0) },
	}
}

// View builds the sections: nothing while there is no agent.
func (l *AgentsList) View(c *ui.Context) {
	s := l.snapshot()
	if len(s.Orchestrators) == 0 && len(s.Workers) == 0 {
		return
	}
	now := l.now()
	section := func(ws []agents.Worker, kind state.SelectedKind) (rows []kit.RowOpts, height float32) {
		for _, w := range ws {
			r := l.opts(w, kind, now, c)
			rows, height = append(rows, r), height+kit.HeightOf(r)
		}
		if len(ws) > 0 {
			height += theme.AgentsHeaderHeight
		}
		return rows, height
	}
	orch, h1 := section(s.Orchestrators, state.KindOrchestrator)
	work, h2 := section(s.Workers, state.KindWorker)
	ui.Scroll(c).Label(LabelAgents).FillWidth().Height(h1+h2+1).MinHeight(0).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		if len(orch) > 0 {
			header(c, LabelOrchestratorsHeader, "Orchestrators", "")
			for _, r := range orch {
				agentRow(c, r)
			}
		}
		if len(work) > 0 {
			header(c, LabelAgentsHeader, "Agents", strconv.Itoa(running(s.Workers))+" running")
			for _, r := range work {
				agentRow(c, r)
			}
		}
	})
}

// running counts the workers whose program still runs.
func running(ws []agents.Worker) int {
	n := 0
	for _, w := range ws {
		if w.Live {
			n++
		}
	}
	return n
}

func header(c *ui.Context, label, title, right string) {
	ui.Row(c).Label(label).FillWidth().Height(theme.AgentsHeaderHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.SidebarPadX).TextColor(theme.Color(theme.TextDim)).Children(func() {
		ui.Text(c, title)
		ui.Spacer(c)
		if right != "" {
			ui.Text(c, right)
		}
	})
}

// agentRow wraps the row in a full-width column, so that the row's side
// margins count in its width.
func agentRow(c *ui.Context, r kit.RowOpts) {
	ui.Column(c.Key(r.Label)).FillWidth().Shrink(0).Children(func() { kit.Row(c, r) })
}
