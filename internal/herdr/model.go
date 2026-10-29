package herdr

import (
	"fmt"
	"maps"
	"reflect"
)

// AgentState is the state of one agent: herdr's five values, one to one.
// The zero value is [StateUnknown].
type AgentState uint8

// The agent states.
const (
	StateUnknown AgentState = iota
	StateIdle
	StateWorking
	StateBlocked
	StateDone
)

// Label returns the lower-case word herdr uses for the state.
func (s AgentState) Label() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateWorking:
		return "working"
	case StateBlocked:
		return "blocked"
	case StateDone:
		return "done"
	}
	return "unknown"
}

func stateFromWire(s WireStatus) AgentState {
	switch s {
	case StatusIdle:
		return StateIdle
	case StatusWorking:
		return StateWorking
	case StatusBlocked:
		return StateBlocked
	case StatusDone:
		return StateDone
	}
	return StateUnknown
}

// HerdrTree is the herdr tree at one snapshot.
type HerdrTree struct {
	Version       string
	FocusedPaneID *string
	Workspaces    []Workspace
}

// Workspace is one workspace of the tree.
type Workspace struct {
	ID      string
	Label   string
	Status  AgentState
	Focused bool
	Tabs    []Tab
}

// Tab is one tab of a workspace.
type Tab struct {
	ID      string
	Label   string
	Status  AgentState
	Focused bool
	Panes   []Pane
}

// Pane is one pane of a tab.
type Pane struct {
	ID      string
	Label   *string
	Cwd     *string
	Focused bool
	// Agent is nil when no agent runs in this pane.
	Agent *AgentInfo
	// Tokens are the pane tokens as herdr sent them. Never nil; empty when
	// the pane has none.
	Tokens map[string]string
}

// AgentInfo is the agent in a pane.
type AgentInfo struct {
	// Name is `display_agent` when herdr sends it, else `agent`.
	Name   string
	Status AgentState
}

// StatusChange is an agent state that changed in one pane between two trees.
type StatusChange struct {
	PaneID string
	// Agent is the agent name from the new tree, else from the old one.
	Agent *string
	// From is nil when the pane had no agent before.
	From *AgentState
	// To is nil when the pane has no agent now.
	To *AgentState
}

// TreeFromSnapshot groups the flat snapshot into the tree, in the snapshot's
// order. Tabs whose workspace is missing and panes whose tab is missing are
// dropped. A pane is placed by its tab id alone: its workspace id is not
// checked against the tab's.
func TreeFromSnapshot(s *WireSnapshot) HerdrTree {
	tabsByWorkspace := make(map[string][]*WireTab)
	for i := range s.Tabs {
		tab := &s.Tabs[i]
		tabsByWorkspace[tab.WorkspaceID] = append(tabsByWorkspace[tab.WorkspaceID], tab)
	}
	panesByTab := make(map[string][]*WirePane)
	for i := range s.Panes {
		pane := &s.Panes[i]
		panesByTab[pane.TabID] = append(panesByTab[pane.TabID], pane)
	}

	workspaces := make([]Workspace, 0, len(s.Workspaces))
	for _, ws := range s.Workspaces {
		var tabs []Tab
		for _, tab := range tabsByWorkspace[ws.WorkspaceID] {
			var panes []Pane
			for _, pane := range panesByTab[tab.TabID] {
				panes = append(panes, Pane{
					ID:      pane.PaneID,
					Label:   pane.Label,
					Cwd:     pane.Cwd,
					Focused: pane.Focused,
					Agent:   agentInfo(pane),
					Tokens:  maps.Clone(nonNil(pane.Tokens)),
				})
			}
			tabs = append(tabs, Tab{
				ID:      tab.TabID,
				Label:   tab.Label,
				Status:  stateFromWire(tab.AgentStatus),
				Focused: tab.Focused,
				Panes:   panes,
			})
		}
		workspaces = append(workspaces, Workspace{
			ID:      ws.WorkspaceID,
			Label:   ws.Label,
			Status:  stateFromWire(ws.AgentStatus),
			Focused: ws.Focused,
			Tabs:    tabs,
		})
	}
	return HerdrTree{Version: s.Version, FocusedPaneID: s.FocusedPaneID, Workspaces: workspaces}
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// agentInfo returns the agent in a pane: `display_agent` if herdr sends it,
// else `agent`. A pane with neither has no agent, whatever its status says.
func agentInfo(pane *WirePane) *AgentInfo {
	name := pane.DisplayAgent
	if name == nil {
		name = pane.Agent
	}
	if name == nil {
		return nil
	}
	return &AgentInfo{Name: *name, Status: stateFromWire(pane.AgentStatus)}
}

// TabCount returns the number of tabs in the tree.
func (t HerdrTree) TabCount() int {
	n := 0
	for _, ws := range t.Workspaces {
		n += len(ws.Tabs)
	}
	return n
}

// PaneCount returns the number of panes in the tree.
func (t HerdrTree) PaneCount() int { return len(t.panes()) }

// PaneIDs returns the pane ids in tree order: the ids to subscribe to for
// status events.
func (t HerdrTree) PaneIDs() []string {
	panes := t.panes()
	ids := make([]string, len(panes))
	for i, pane := range panes {
		ids[i] = pane.ID
	}
	return ids
}

// Equal reports whether both trees hold the same values, tokens included.
func (t HerdrTree) Equal(other HerdrTree) bool { return reflect.DeepEqual(t, other) }

// SameShape reports whether both trees have the same workspaces, tabs and
// panes, with the same ids and labels in the same order. Status and focus are
// ignored.
func (t HerdrTree) SameShape(other HerdrTree) bool {
	a, b := t.shape(), other.shape()
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// StatusChanges returns every pane present in both trees whose agent state
// differs, in next's order. A pane that is new or gone is not reported; see
// [HerdrTree.SameShape].
func (t HerdrTree) StatusChanges(next HerdrTree) []StatusChange {
	before := make(map[string]*Pane)
	for _, pane := range t.panes() {
		before[pane.ID] = pane
	}
	var changes []StatusChange
	for _, pane := range next.panes() {
		prev, ok := before[pane.ID]
		if !ok {
			continue
		}
		from, to := agentState(prev.Agent), agentState(pane.Agent)
		if sameState(from, to) {
			continue
		}
		named := pane.Agent
		if named == nil {
			named = prev.Agent
		}
		var name *string
		if named != nil {
			name = &named.Name
		}
		changes = append(changes, StatusChange{PaneID: pane.ID, Agent: name, From: from, To: to})
	}
	return changes
}

func agentState(a *AgentInfo) *AgentState {
	if a == nil {
		return nil
	}
	s := a.Status
	return &s
}

func sameState(a, b *AgentState) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// LogLines returns one indented line per workspace, tab and pane, for the
// log. The header line with the counts is the caller's.
func (t HerdrTree) LogLines() []string {
	var lines []string
	for _, ws := range t.Workspaces {
		lines = append(lines, fmt.Sprintf("  ws %s %q %s", ws.ID, ws.Label, ws.Status.Label()))
		for _, tab := range ws.Tabs {
			lines = append(lines, fmt.Sprintf("    tab %s %q %s", tab.ID, tab.Label, tab.Status.Label()))
			for i := range tab.Panes {
				lines = append(lines, paneLogLine(&tab.Panes[i]))
			}
		}
	}
	return lines
}

func (t HerdrTree) panes() []*Pane {
	var panes []*Pane
	for wi := range t.Workspaces {
		for ti := range t.Workspaces[wi].Tabs {
			tab := &t.Workspaces[wi].Tabs[ti]
			for pi := range tab.Panes {
				panes = append(panes, &tab.Panes[pi])
			}
		}
	}
	return panes
}

type shapeNode struct {
	depth    uint8
	id       string
	label    string
	hasLabel bool
}

// shape lists the id, label and depth of every node in tree order. Depth
// keeps the nesting, so two trees with equal shapes have the same parents too.
func (t HerdrTree) shape() []shapeNode {
	var shape []shapeNode
	for _, ws := range t.Workspaces {
		shape = append(shape, shapeNode{0, ws.ID, ws.Label, true})
		for _, tab := range ws.Tabs {
			shape = append(shape, shapeNode{1, tab.ID, tab.Label, true})
			for _, pane := range tab.Panes {
				node := shapeNode{depth: 2, id: pane.ID}
				if pane.Label != nil {
					node.label, node.hasLabel = *pane.Label, true
				}
				shape = append(shape, node)
			}
		}
	}
	return shape
}

// LogLine returns `herdr status pane=<id> agent=<name> <from> -> <to>`, with
// `-` for no agent or no state.
func (c StatusChange) LogLine() string {
	name := "-"
	if c.Agent != nil {
		name = *c.Agent
	}
	return fmt.Sprintf("herdr status pane=%s agent=%s %s -> %s", c.PaneID, name, stateWord(c.From), stateWord(c.To))
}

func stateWord(s *AgentState) string {
	if s == nil {
		return "-"
	}
	return s.Label()
}

func paneLogLine(pane *Pane) string {
	name, status := "-", "-"
	if pane.Agent != nil {
		name, status = pane.Agent.Name, pane.Agent.Status.Label()
	}
	focus := ""
	if pane.Focused {
		focus = " focused"
	}
	return fmt.Sprintf("      pane %s %s %s%s", pane.ID, name, status, focus)
}
