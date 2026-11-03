package app

// The needs-you row under the terminal: one line of tabs for what waits on
// the owner. Blocked workers come first, then questions (D7), then to-dos
// (T12). Resolved items stay, struck through, until "clear done". The row is
// hidden when nothing waits. A click on a tab picks it; it opens the
// answer card (#162, S5).

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the needs-you row.
const (
	LabelNeedsRow       = "needs row"
	LabelNeedsClearDone = "clear done"
)

// AgentSource is the part of agentstate.Model that the views read and act on.
type AgentSource interface {
	Snapshot() agentstate.Snapshot
	Answer(id, answer string) error
	Done(id string) error
	ClearDone()
	// Read, Interrupt and Stop act on one worker's terminal (its address).
	Read(name string, lines int) (string, error)
	// SendKeys presses raw keys in a worker's terminal, with no Enter.
	SendKeys(name string, keys []byte) error
	Interrupt(name string) error
	Stop(name string) error
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
	// Command and BlockKind (permission, question or auth) tell the card how
	// to read the worker's prompt.
	Command   string
	BlockKind string
}

// Label is the accessible name: "{key} {title}".
func (t NeedTab) Label() string { return t.Key + " " + t.Title }

// NeedsRow is the row's state. Build it with its fields set; Scope,
// ClearDone and Open may be nil.
type NeedsRow struct {
	// Snapshot reads the agent state; nil means nothing waits.
	Snapshot func() agentstate.Snapshot
	// ClearDone forgets the resolved items. The "clear done" control shows
	// only when it is set.
	ClearDone func()
	// Scope returns the project to show, or "" for all projects.
	Scope func() string
	// Open runs when a tab is clicked. The frame opens the answer card here.
	Open func(NeedTab)

	picked string
}

// Tabs returns the tabs for the scope: blocked workers, then questions, then
// to-dos.
func (n *NeedsRow) Tabs() []NeedTab {
	if n.Snapshot == nil {
		return nil
	}
	project := ""
	if n.Scope != nil {
		project = n.Scope()
	}
	return needItems(n.Snapshot(), project)
}

// needItems returns the items of the needs-you row for project, or for all
// projects when project is "": blocked workers, then questions, then to-dos.
func needItems(snap agentstate.Snapshot, project string) []NeedTab {
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
			tabs = append(tabs, NeedTab{Kind: NeedQuestion, Key: q.ID, Title: oneLine(title), Icon: icons.Decide, Project: q.Project, Resolved: done[q.ID]})
		}
	}
	for _, t := range snap.Todos {
		if in(t.Project) {
			icon := icons.Do
			if t.Kind == "know" {
				icon = icons.Fyi
			}
			tabs = append(tabs, NeedTab{Kind: NeedTodo, Key: t.ID, Title: oneLine(t.Text), Icon: icon, Project: t.Project, Resolved: done[t.ID]})
		}
	}
	return tabs
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// View builds the row, or nothing when no tab shows.
func (n *NeedsRow) View(c *ui.Context) {
	tabs := n.Tabs()
	if len(tabs) == 0 {
		return
	}
	anyDone := false
	ui.Row(c).Label(LabelNeedsRow).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(theme.PanelPadX).
		Background(theme.Color(theme.BgBars)).BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).
		Children(func() {
			for _, t := range tabs {
				anyDone = anyDone || t.Resolved
				n.tab(c, t)
			}
			ui.Box(c).Grow(1)
			if anyDone && n.ClearDone != nil {
				ui.ButtonBase(c).Label(LabelNeedsClearDone).Shrink(0).Cursor(ui.CursorPointer).
					OnClick(n.ClearDone).Children(func() {
					ui.Text(c, LabelNeedsClearDone).TextColor(theme.Color(theme.TextDim)).NoWrap()
				})
			}
		})
}

func (n *NeedsRow) tab(c *ui.Context, t NeedTab) {
	text := uint32(theme.Text)
	if t.Resolved {
		text = theme.TextDim
	}
	picked := n.picked == t.Key
	el := ui.ButtonBase(c).Label(t.Label()).Height(theme.BoardLineHeight - 6).Shrink(1).MinWidth(0).MaxWidth(220).
		AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(6).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).Selected(picked).OnClick(func() {
		n.picked = t.Key
		if n.Open != nil {
			n.Open(t)
		}
	})
	if picked {
		el = el.Background(theme.Color(theme.BgCard))
	}
	el.Children(func() {
		icons.Sized(c, t.Icon, theme.IconSizeSmall).Shrink(0)
		ui.Text(c, t.Key).Shrink(0).NoWrap().TextColor(theme.Color(text))
		title := ui.Text(c, t.Title).MinWidth(0).SingleLine().TextColor(theme.Color(text))
		if t.Resolved {
			title.Strikethrough()
		}
	})
}
