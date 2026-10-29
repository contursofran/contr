package herdr

// TabItem is one tab in the strip.
type TabItem struct {
	ID string
	// Label is herdr's tab label, shown as it is.
	Label string
	// Status is herdr's aggregate status for the tab.
	Status AgentState
	// Active is true for the focused tab. At most one tab in a strip is active.
	Active bool
}

// TabStrip is what the title bar shows for one herdr tree: the tabs of the
// current workspace, the target of "+", and the target of split.
type TabStrip struct {
	// WorkspaceID is the target of "+". Empty when there is no tree or no
	// workspace.
	WorkspaceID string
	// Tabs holds every tab of the current workspace, in herdr's order.
	Tabs []TabItem
	// SplitPaneID is the target of split. Empty when there is no active tab or
	// it has no panes.
	SplitPaneID string
}

// TabStripFromTree returns the strip for tree. No tree (herdr not running)
// and no workspace both give the empty strip.
func TabStripFromTree(tree *HerdrTree) TabStrip {
	if tree == nil {
		return TabStrip{}
	}
	ws := tree.CurrentWorkspace()
	if ws == nil {
		return TabStrip{}
	}
	active := activeIndex(ws, tree.FocusedPaneID)
	var tabs []TabItem
	for i, tab := range ws.Tabs {
		tabs = append(tabs, TabItem{
			ID:     tab.ID,
			Label:  tab.Label,
			Status: tab.Status,
			Active: i == active,
		})
	}
	strip := TabStrip{WorkspaceID: ws.ID, Tabs: tabs}
	if active >= 0 {
		if pane := splitPane(&ws.Tabs[active], tree.FocusedPaneID); pane != nil {
			strip.SplitPaneID = pane.ID
		}
	}
	return strip
}

// CurrentWorkspace returns the workspace the title bar works in: the focused
// one, else the one that holds the focused pane, else the first. It returns
// nil without workspaces.
func (t HerdrTree) CurrentWorkspace() *Workspace {
	for i := range t.Workspaces {
		if t.Workspaces[i].Focused {
			return &t.Workspaces[i]
		}
	}
	if t.FocusedPaneID != nil {
		for i := range t.Workspaces {
			for j := range t.Workspaces[i].Tabs {
				if tabHolds(&t.Workspaces[i].Tabs[j], *t.FocusedPaneID) {
					return &t.Workspaces[i]
				}
			}
		}
	}
	if len(t.Workspaces) > 0 {
		return &t.Workspaces[0]
	}
	return nil
}

// activeIndex returns the index of the active tab in ws.Tabs: the first tab
// with Focused, else the tab holding the focused pane, else -1.
func activeIndex(ws *Workspace, focusedPane *string) int {
	for i := range ws.Tabs {
		if ws.Tabs[i].Focused {
			return i
		}
	}
	if focusedPane != nil {
		for i := range ws.Tabs {
			if tabHolds(&ws.Tabs[i], *focusedPane) {
				return i
			}
		}
	}
	return -1
}

// splitPane returns the pane a split targets in tab: the focused pane if it
// is in tab, else the first focused pane, else the first pane.
func splitPane(tab *Tab, focusedPane *string) *Pane {
	if focusedPane != nil {
		for i := range tab.Panes {
			if tab.Panes[i].ID == *focusedPane {
				return &tab.Panes[i]
			}
		}
	}
	for i := range tab.Panes {
		if tab.Panes[i].Focused {
			return &tab.Panes[i]
		}
	}
	if len(tab.Panes) > 0 {
		return &tab.Panes[0]
	}
	return nil
}

func tabHolds(tab *Tab, paneID string) bool {
	for i := range tab.Panes {
		if tab.Panes[i].ID == paneID {
			return true
		}
	}
	return false
}
