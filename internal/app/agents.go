package app

// The agents section at the bottom of the sidebar: an "Agents" header with
// the running count, then one row per agent in herdr's order. A click selects
// the row and focuses its pane in herdr. Ported from the Rust contr module
// agents_list.
//
// MyGo rebuilds a view on every redraw, so the Rust "one cached view per
// row" becomes: the working icon draws its own turn (no view runs for it),
// every row is a cached layer (its paint is reused while it looks the same),
// and [AgentsList.Sync] tells what changed so that nothing redraws for a
// tree that shows the same rows.

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// focusTimeout is how long a focus call may wait for herdr's socket.
const focusTimeout = 2 * time.Second

// FocusFn sends `pane.focus` for a pane id. report shows a failure to the
// user. The app uses [FocusInBackground]; tests record the ids.
type FocusFn func(paneID string, report func(string))

// FocusInBackground focuses a pane through herdr's API socket on a
// goroutine, so the UI thread never waits for the socket. With no socket
// path nothing is sent. A failed call is logged and given to report, which
// runs on that goroutine and should only set the status message.
func FocusInBackground(paneID string, report func(string)) {
	go focusPane(herdr.APISocketPath, paneID, report)
}

func focusPane(socket func() (string, bool), paneID string, report func(string)) {
	path, ok := socket()
	if !ok {
		slog.Warn("no herdr socket path; pane not focused", "pane", paneID)
		return
	}
	if err := herdr.FocusPane(path, paneID, focusTimeout); err != nil {
		slog.Warn("focus pane failed", "pane", paneID, "err", err)
		if report != nil {
			report(failureLine(err))
		}
	}
}

// Element labels of the agents section.
const (
	LabelAgents       = "sidebar agents"
	LabelAgentsHeader = "agents header"
)

// AgentRowLabel is the row's label, which is also its accessible label:
// "{name}, {status}, {detail}", then ", {link}" when there is one, then
// ", selected" on the selected row.
func AgentRowLabel(row herdr.AgentRow, selected bool) string {
	label := row.Name + ", " + icons.FromAgentState(row.Status).Label() + ", " + row.Detail
	if row.Link != nil {
		label += ", " + *row.Link
	}
	if selected {
		label += ", selected"
	}
	return label
}

// AgentsChange tells what [AgentsList.Sync] changed.
type AgentsChange struct {
	// Rebuilt: the panes changed (added, removed, reordered, or the tree
	// came or went), so the whole list changes.
	Rebuilt bool
	// Rows are the indexes of the rows that show other data, when the panes
	// are the same.
	Rows []int
}

// Any tells whether the list needs a redraw.
func (c AgentsChange) Any() bool { return c.Rebuilt || len(c.Rows) > 0 }

// AgentsList is the agents section. It is empty, with no height, until herdr
// has a tree. Methods run on the UI thread.
type AgentsList struct {
	src       liveSource
	board     *Board // nil in views built without one
	selection *state.Selection
	status    *state.StatusMessage
	focus     FocusFn

	hasTree bool
	rows    []herdr.AgentRow
	// builds counts the builds of each row by pane id. Tests read it.
	builds map[string]int
}

// NewAgentsList returns the list. src gives the tree, b (may be nil) the
// agents' names, selection the card. status (may be nil) shows focus
// errors. A click focuses the pane with [FocusInBackground]. Call
// [AgentsList.Sync] when the tree or the board changed.
func NewAgentsList(src liveSource, b *Board, selection *state.Selection, status *state.StatusMessage) *AgentsList {
	l := &AgentsList{src: src, board: b, selection: selection, status: status, focus: FocusInBackground, builds: map[string]int{}}
	l.Sync()
	return l
}

// Rows returns the rows shown, in order. Empty without a tree.
func (l *AgentsList) Rows() []herdr.AgentRow { return l.rows }

// Sync brings the rows up to the tree and the board. A selected agent that
// the new tree no longer has leaves the selection; another kind of
// selection is kept, and so is the agent's while there is no tree.
func (l *AgentsList) Sync() AgentsChange {
	var tree *herdr.HerdrTree
	if l.src != nil {
		tree = l.src.Live().Tree
	}
	var rows []herdr.AgentRow
	if tree != nil {
		rows = herdr.AgentRows(*tree, l.metaFor())
		l.dropMissingSelection(rows)
	}
	change := AgentsChange{Rebuilt: l.hasTree != (tree != nil) || !samePanes(l.rows, rows)}
	if !change.Rebuilt {
		for i := range rows {
			if !sameRow(l.rows[i], rows[i]) {
				change.Rows = append(change.Rows, i)
			}
		}
	}
	l.hasTree, l.rows = tree != nil, rows
	return change
}

func (l *AgentsList) metaFor() func(string) *herdr.AgentMeta {
	if l.board == nil {
		return nil
	}
	snap := l.board.View().Snapshot
	if snap == nil {
		return nil
	}
	meta := board.AgentMeta(snap)
	return func(paneID string) *herdr.AgentMeta {
		if m, ok := meta[paneID]; ok {
			return &m
		}
		return nil
	}
}

func (l *AgentsList) dropMissingSelection(rows []herdr.AgentRow) {
	pane, ok := l.selectedPane()
	if !ok {
		return
	}
	for _, r := range rows {
		if r.PaneID == pane {
			return
		}
	}
	l.selection.Clear()
}

// selectedPane is the pane of the selected agent, if an agent is selected.
func (l *AgentsList) selectedPane() (string, bool) {
	if l.selection == nil {
		return "", false
	}
	if sel, ok := l.selection.Current(); ok && sel.Kind == state.KindAgent {
		return sel.ID, true
	}
	return "", false
}

func samePanes(a, b []herdr.AgentRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].PaneID != b[i].PaneID {
			return false
		}
	}
	return true
}

func sameRow(a, b herdr.AgentRow) bool {
	if (a.Link == nil) != (b.Link == nil) || (a.Link != nil && *a.Link != *b.Link) {
		return false
	}
	a.Link, b.Link = nil, nil
	return a == b
}

// Activate is a click on a row: it becomes the selection, and herdr focuses
// its pane. A second click on the same row focuses it again.
func (l *AgentsList) Activate(paneID string) {
	if l.selection != nil {
		l.selection.Select(state.Selected{Kind: state.KindAgent, ID: paneID})
	}
	if l.focus != nil {
		var report func(string)
		if l.status != nil {
			report = l.status.Show
		}
		l.focus(paneID, report)
	}
}

// View builds the section: nothing without a tree.
func (l *AgentsList) View(c *ui.Context) {
	if !l.hasTree {
		return
	}
	selected, _ := l.selectedPane()
	ui.Column(c).Label(LabelAgents).FillWidth().MinHeight(0).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		l.header(c)
		ui.Scroll(c).FillWidth().Height(float32(len(l.rows) * theme.AgentRowHeight)).MinHeight(0).PaddingX(theme.CardInsetX).Children(func() {
			for _, row := range l.rows {
				l.row(c, row, row.PaneID == selected)
			}
		})
	})
}

func (l *AgentsList) header(c *ui.Context) {
	ui.Row(c).Label(LabelAgentsHeader).FillWidth().Height(theme.AgentsHeaderHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.SidebarPadX).TextColor(theme.Color(theme.TextDim)).Children(func() {
		ui.Text(c, "Agents")
		ui.Spacer(c)
		ui.Text(c, herdr.RunningLine(len(l.rows)))
	})
}

// row builds one agent row: the status icon, the name, the detail and, when
// there is one, the link. The selected row shows the card.
func (l *AgentsList) row(c *ui.Context, row herdr.AgentRow, selected bool) {
	l.builds[row.PaneID]++
	card := ui.ButtonBase(c).Label(AgentRowLabel(row, selected)).FillWidth().Height(theme.AgentRowHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.AgentGap).PaddingX(theme.AgentRowPadX).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).Cache().OnClick(func() { l.Activate(row.PaneID) })
	if selected {
		card = card.Background(theme.Color(theme.BgCard)).Border(1, theme.Color(theme.CardBorder))
	}
	card.Children(func() {
		icons.Icon(c, icons.FromAgentState(row.Status)).Shrink(0)
		ui.Text(c, row.Name).TextColor(theme.Color(theme.Text)).NoWrap().Ellipsis("…").MinWidth(0)
		ui.Text(c, row.Detail).TextColor(theme.Color(theme.TextDim2)).NoWrap().Ellipsis("…").MinWidth(0).Grow(1)
		if row.Link != nil {
			ui.Text(c, *row.Link).TextColor(theme.Color(theme.Text2)).NoWrap().Shrink(0)
		}
	})
}
