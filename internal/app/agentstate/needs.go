package agentstate

// The items that wait on the owner: blocked workers first, then questions
// (D7), then to-dos (T12). The v0.2 needs-you row under the terminal is gone
// (D146); the questions notch shows these items now (D135). The disabled board
// lists them with [NeedItems].

import (
	"cmp"
	"strings"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/icons"
)

// AgentSource is the part of Model that the views read and act on.
type AgentSource interface {
	Snapshot() Snapshot
	Answer(id, answer string) error
	Done(id string) error
	ClearDone()
	// Read, Interrupt and Stop act on one worker's terminal (its address).
	Read(name string, lines int) (string, error)
	// Send types text into a worker's terminal and presses Enter.
	Send(name, text string) error
	// SendKeys presses raw keys in a worker's terminal, with no Enter.
	SendKeys(name string, keys []byte) error
	Interrupt(name string) error
	Stop(name string) error
	// Close stops a plain terminal and removes it, so its tab goes.
	Close(name string) error
	// SetVisible tells the source whether the window shows; a hidden window
	// has no retry timer (D9).
	SetVisible(visible bool)
	// Reconnect tries to connect to hc at once while it is offline.
	Reconnect()
}

// NeedKind tells what a [NeedTab] is.
type NeedKind uint8

// The kinds of tab, in the order of the row.
const (
	NeedBlocked NeedKind = iota + 1
	NeedQuestion
	NeedTodo
)

// NeedTab is one tab of the row. Key is the worker name, D7 or T12.
type NeedTab struct {
	Kind     NeedKind
	Key      string
	Title    string
	Icon     icons.Status
	Project  string
	Resolved bool
	// Address and Orchestrator name the worker of a blocked tab.
	Address      string
	Orchestrator bool
	// Coordinator is the hc terminal that asked a question or pinned a to-do;
	// empty when not known.
	Coordinator string
	// Command and BlockKind (permission, question or auth) tell the card how
	// to read the worker's prompt.
	Command   string
	BlockKind string
}

// Label is the accessible name: "{key} {title}".
func (t NeedTab) Label() string { return t.Key + " " + t.Title }

// NeedItems returns a function that lists the items for the scope: the project
// scope returns, or all projects when it is nil or returns "". A nil snap
// lists nothing.
func NeedItems(snap func() Snapshot, scope func() string) func() []NeedTab {
	return func() []NeedTab {
		if snap == nil {
			return nil
		}
		project := ""
		if scope != nil {
			project = scope()
		}
		return NeedItemsOf(snap(), project)
	}
}

// NeedItemsOf returns the items of the items for project, or for all
// projects when project is "": blocked workers, then questions, then to-dos.
func NeedItemsOf(snap Snapshot, project string) []NeedTab {
	in := func(p string) bool { return project == "" || p == project }
	done := make(map[string]bool, len(snap.Resolved))
	for _, id := range snap.Resolved {
		done[id] = true
	}
	var tabs []NeedTab
	for _, w := range append(append([]agents.Worker(nil), snap.Orchestrators...), snap.Workers...) {
		if w.Status == "blocked" && in(w.Project) {
			tabs = append(tabs, NeedTab{Kind: NeedBlocked, Key: w.Name, Title: w.Msg, Icon: icons.Blocked,
				Project: w.Project, Address: w.Address, Orchestrator: w.Orchestrator(),
				Command: cmp.Or(w.Command, w.Profile), BlockKind: w.Kind})
		}
	}
	for _, q := range snap.Questions {
		if in(q.Project) {
			title := q.Title
			if title == "" {
				title = q.Question
			}
			tabs = append(tabs, NeedTab{Kind: NeedQuestion, Key: q.ID, Title: oneLine(title), Icon: icons.Decide, Project: q.Project, Resolved: done[q.ID],
				Coordinator: q.Coordinator})
		}
	}
	for _, t := range snap.Todos {
		if in(t.Project) {
			icon := icons.Do
			if t.Kind == "know" {
				icon = icons.Fyi
			}
			tabs = append(tabs, NeedTab{Kind: NeedTodo, Key: t.ID, Title: oneLine(t.Text), Icon: icon, Project: t.Project, Resolved: done[t.ID],
				Coordinator: t.Coordinator})
		}
	}
	return tabs
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
