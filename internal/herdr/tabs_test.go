package herdr

import (
	"reflect"
	"testing"
)

func tabsPane(id string, focused bool) Pane { return Pane{ID: id, Focused: focused} }

// tabsTab returns an idle tab whose label is its id.
func tabsTab(id string, focused bool, panes ...Pane) Tab {
	return Tab{ID: id, Label: id, Status: StateIdle, Focused: focused, Panes: panes}
}

func tabsWorkspace(id string, focused bool, tabs ...Tab) Workspace {
	return Workspace{ID: id, Label: id, Status: StateIdle, Focused: focused, Tabs: tabs}
}

func tabsTree(focusedPane string, workspaces ...Workspace) HerdrTree {
	t := HerdrTree{Version: "0.9.3", Workspaces: workspaces}
	if focusedPane != "" {
		t.FocusedPaneID = &focusedPane
	}
	return t
}

func tabsStripOf(t HerdrTree) TabStrip { return TabStripFromTree(&t) }

func tabsCurrentID(t HerdrTree) string {
	if ws := t.CurrentWorkspace(); ws != nil {
		return ws.ID
	}
	return "<none>"
}

// tabsActiveIDs lists the active tabs, so a test can see that there is at
// most one.
func tabsActiveIDs(s TabStrip) []string {
	var ids []string
	for _, item := range s.Tabs {
		if item.Active {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func tabsIDs(s TabStrip) []string {
	var ids []string
	for _, item := range s.Tabs {
		ids = append(ids, item.ID)
	}
	return ids
}

func tabsEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestNoTreeGivesTheEmptyStrip(t *testing.T) {
	tabsEqual(t, TabStripFromTree(nil), TabStrip{})
}

func TestNoWorkspaceGivesTheEmptyStrip(t *testing.T) {
	tabsEqual(t, tabsStripOf(tabsTree("w1:p1")), TabStrip{})
}

func TestShowsOnlyTheFocusedWorkspaceTabsInHerdrOrder(t *testing.T) {
	tr := tabsTree("",
		tabsWorkspace("w1", false, tabsTab("w1:t1", false)),
		tabsWorkspace("w2", true, tabsTab("w2:t2", false), tabsTab("w2:t1", false)))
	s := tabsStripOf(tr)
	tabsEqual(t, s.WorkspaceID, "w2")
	tabsEqual(t, tabsIDs(s), []string{"w2:t2", "w2:t1"})
}

func TestCurrentWorkspacePrefersTheFocusedFlagOverTheFocusedPane(t *testing.T) {
	tr := tabsTree("w1:p1",
		tabsWorkspace("w1", false, tabsTab("w1:t1", false, tabsPane("w1:p1", true))),
		tabsWorkspace("w2", true))
	tabsEqual(t, tabsCurrentID(tr), "w2")
}

func TestCurrentWorkspaceIsTheOneHoldingTheFocusedPaneWhenNoneIsFocused(t *testing.T) {
	tr := tabsTree("w2:p3",
		tabsWorkspace("w1", false, tabsTab("w1:t1", false, tabsPane("w1:p1", false))),
		tabsWorkspace("w2", false, tabsTab("w2:t1", false, tabsPane("w2:p3", false))))
	tabsEqual(t, tabsCurrentID(tr), "w2")
}

func TestCurrentWorkspaceIsTheFirstWhenNothingPointsAtOne(t *testing.T) {
	noPane := tabsTree("", tabsWorkspace("w1", false), tabsWorkspace("w2", false))
	unknownPane := tabsTree("gone:p1", tabsWorkspace("w1", false), tabsWorkspace("w2", false))
	tabsEqual(t, tabsCurrentID(noPane), "w1")
	tabsEqual(t, tabsCurrentID(unknownPane), "w1")
}

func TestCurrentWorkspaceIsNoneWithoutWorkspaces(t *testing.T) {
	tr := tabsTree("w1:p1")
	if tr.CurrentWorkspace() != nil {
		t.Error("want nil workspace")
	}
}

func TestTheFocusedTabIsTheActiveOne(t *testing.T) {
	tr := tabsTree("", tabsWorkspace("w1", true,
		tabsTab("w1:t1", false), tabsTab("w1:t2", true), tabsTab("w1:t3", false)))
	tabsEqual(t, tabsActiveIDs(tabsStripOf(tr)), []string{"w1:t2"})
}

func TestTheTabHoldingTheFocusedPaneIsActiveWhenNoTabIsFocused(t *testing.T) {
	tr := tabsTree("w1:p3", tabsWorkspace("w1", true,
		tabsTab("w1:t1", false, tabsPane("w1:p1", false)),
		tabsTab("w1:t2", false, tabsPane("w1:p3", false))))
	s := tabsStripOf(tr)
	tabsEqual(t, tabsActiveIDs(s), []string{"w1:t2"})
	tabsEqual(t, s.SplitPaneID, "w1:p3")
}

func TestNoTabIsActiveWhenTheFocusedPaneIsInAnotherWorkspace(t *testing.T) {
	tr := tabsTree("w2:p1",
		tabsWorkspace("w1", true, tabsTab("w1:t1", false, tabsPane("w1:p1", false))),
		tabsWorkspace("w2", false, tabsTab("w2:t1", false, tabsPane("w2:p1", true))))
	s := tabsStripOf(tr)
	tabsEqual(t, s.WorkspaceID, "w1")
	tabsEqual(t, len(tabsActiveIDs(s)), 0)
	tabsEqual(t, s.SplitPaneID, "")
}

func TestAtMostOneTabIsActiveEvenIfTwoSayFocused(t *testing.T) {
	tr := tabsTree("", tabsWorkspace("w1", true, tabsTab("w1:t1", true), tabsTab("w1:t2", true)))
	tabsEqual(t, tabsActiveIDs(tabsStripOf(tr)), []string{"w1:t1"})
}

func TestAWorkspaceWithoutTabsKeepsItsIDForThePlusButton(t *testing.T) {
	s := tabsStripOf(tabsTree("", tabsWorkspace("w1", true)))
	tabsEqual(t, s.WorkspaceID, "w1")
	tabsEqual(t, len(s.Tabs), 0)
	tabsEqual(t, s.SplitPaneID, "")
}

func TestStatusAndLabelAreCopiedFromEachTab(t *testing.T) {
	tr := tabsTree("", tabsWorkspace("w1", true,
		Tab{ID: "w1:t1", Label: "api", Status: StateBlocked, Focused: true},
		Tab{ID: "w1:t2", Label: "docs", Status: StateWorking},
		Tab{ID: "w1:t3", Label: "3", Status: StateUnknown}))
	tabsEqual(t, tabsStripOf(tr).Tabs, []TabItem{
		{ID: "w1:t1", Label: "api", Status: StateBlocked, Active: true},
		{ID: "w1:t2", Label: "docs", Status: StateWorking},
		{ID: "w1:t3", Label: "3", Status: StateUnknown},
	})
}

func TestSplitTargetsTheFocusedPaneOfTheActiveTab(t *testing.T) {
	tr := tabsTree("w1:p2", tabsWorkspace("w1", true,
		tabsTab("w1:t1", true, tabsPane("w1:p1", true), tabsPane("w1:p2", false))))
	tabsEqual(t, tabsStripOf(tr).SplitPaneID, "w1:p2")
}

func TestSplitFallsBackToTheFirstFocusedPaneOfTheActiveTab(t *testing.T) {
	tr := tabsTree("w1:p9", tabsWorkspace("w1", true,
		tabsTab("w1:t1", true, tabsPane("w1:p1", false), tabsPane("w1:p2", true), tabsPane("w1:p3", true)),
		tabsTab("w1:t2", false, tabsPane("w1:p9", true))))
	tabsEqual(t, tabsStripOf(tr).SplitPaneID, "w1:p2")
}

func TestSplitFallsBackToTheFirstPaneWhenNoneIsFocused(t *testing.T) {
	tr := tabsTree("", tabsWorkspace("w1", true,
		tabsTab("w1:t1", true, tabsPane("w1:p1", false), tabsPane("w1:p2", false))))
	tabsEqual(t, tabsStripOf(tr).SplitPaneID, "w1:p1")
}

func TestSplitIsNoneForAnActiveTabWithoutPanes(t *testing.T) {
	s := tabsStripOf(tabsTree("", tabsWorkspace("w1", true, tabsTab("w1:t1", true))))
	tabsEqual(t, tabsActiveIDs(s), []string{"w1:t1"})
	tabsEqual(t, s.SplitPaneID, "")
}

func TestSplitIsNoneWithoutAnActiveTab(t *testing.T) {
	s := tabsStripOf(tabsTree("", tabsWorkspace("w1", true,
		tabsTab("w1:t1", false, tabsPane("w1:p1", true)))))
	tabsEqual(t, len(tabsActiveIDs(s)), 0)
	tabsEqual(t, s.SplitPaneID, "")
}
