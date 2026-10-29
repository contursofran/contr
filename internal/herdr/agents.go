// The agents list the sidebar shows: one row per herdr pane that runs an
// agent, in tree order. Pure: no socket, no UI. Name, model and role come
// from the board bridge (AgentMeta) when it has them. Without it, a row shows
// the herdr tab label and the agent kind.

package herdr

import (
	"strconv"
	"strings"
)

// AgentMeta is what the board bridge knows about one agent. Every field is
// optional: nil means absent.
type AgentMeta struct {
	Name  *string
	Model *string
	Role  *string
	Link  *string
}

// AgentRow is one row of the agents list.
type AgentRow struct {
	PaneID      string
	TabID       string
	WorkspaceID string
	// Name is the bridge's name, else the tab label. With more than one agent
	// in the tab: the tab label, then the pane's label or its number.
	Name string
	// Detail is "model · role" from the bridge, with the parts that are
	// present. Else the agent kind, as herdr reports it (claude).
	Detail string
	// Link is the bridge's link, as given. Nil when there is none.
	Link *string
	// Status is herdr's state, one to one.
	Status AgentState
	// Focused is true for the pane that herdr focuses.
	Focused bool
}

// AgentRows gives one row per pane that runs an agent, in tree order:
// workspace, tab, then pane. meta gives the bridge's data for a pane id, or
// nil when there is none. meta may be nil.
func AgentRows(tree HerdrTree, meta func(paneID string) *AgentMeta) []AgentRow {
	var rows []AgentRow
	for _, ws := range tree.Workspaces {
		for _, tab := range ws.Tabs {
			var agents []*Pane
			for i := range tab.Panes {
				if tab.Panes[i].Agent != nil {
					agents = append(agents, &tab.Panes[i])
				}
			}
			for index, pane := range agents {
				var bridge AgentMeta
				if meta != nil {
					if m := meta(pane.ID); m != nil {
						bridge = *m
					}
				}
				name, ok := agentsPresent(bridge.Name)
				if !ok {
					name = fallbackName(tab, pane, index, len(agents))
				}
				var link *string
				if l, ok := agentsPresent(bridge.Link); ok {
					link = &l
				}
				rows = append(rows, AgentRow{
					PaneID:      pane.ID,
					TabID:       tab.ID,
					WorkspaceID: ws.ID,
					Name:        name,
					Detail:      detailOf(bridge, pane.Agent.Name),
					Link:        link,
					Status:      pane.Agent.Status,
					Focused:     tree.FocusedPaneID != nil && *tree.FocusedPaneID == pane.ID,
				})
			}
		}
	}
	return rows
}

// RunningLine is the header count: "N running". Every listed agent counts,
// idle and done too.
func RunningLine(n int) string { return strconv.Itoa(n) + " running" }

// fallbackName is the tab label, or the agent kind when the label is blank.
// When the tab has more than one agent, the name is followed by the pane's
// label, or by the pane's 1-based number among the tab's agents.
func fallbackName(tab Tab, pane *Pane, index, agentsInTab int) string {
	base := pane.Agent.Name
	if !agentsBlank(tab.Label) {
		base = tab.Label
	}
	if agentsInTab < 2 {
		return base
	}
	which, ok := agentsPresent(pane.Label)
	if !ok {
		which = strconv.Itoa(index + 1)
	}
	return base + " · " + which
}

// detailOf is "model · role" from the parts of bridge that are present. The
// agent kind when neither is.
func detailOf(bridge AgentMeta, agent string) string {
	var parts []string
	for _, p := range []*string{bridge.Model, bridge.Role} {
		if s, ok := agentsPresent(p); ok {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return agent
	}
	return strings.Join(parts, " · ")
}

// agentsPresent gives the text when it has something besides white space. The text
// is kept as given.
func agentsPresent(text *string) (string, bool) {
	if text == nil || agentsBlank(*text) {
		return "", false
	}
	return *text, true
}

func agentsBlank(s string) bool { return strings.TrimSpace(s) == "" }
