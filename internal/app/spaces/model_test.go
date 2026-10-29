package spaces

import (
	"reflect"
	"testing"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/herdr"
)

const (
	common = "/work/app/.git"
	mainWT = "/work/app"
	w1Root = "/work/app-wt/w1"
	w2Root = "/work/app-wt/w2"
)

const (
	blocked = herdr.StateBlocked
	done    = herdr.StateDone
	idle    = herdr.StateIdle
	unknown = herdr.StateUnknown
	working = herdr.StateWorking
)

func sp(s string) *string { return &s }

// pane is a pane in cwd; state is its agent's state, nil for a plain shell.
func pane(id, cwd string, state *herdr.AgentState) herdr.Pane {
	p := herdr.Pane{ID: id, Cwd: sp(cwd), Tokens: map[string]string{}}
	if state != nil {
		p.Agent = &herdr.AgentInfo{Name: "claude", Status: *state}
	}
	return p
}

func st(s herdr.AgentState) *herdr.AgentState { return &s }

func tab(id, label string, panes ...herdr.Pane) herdr.Tab {
	return herdr.Tab{ID: id, Label: label, Status: idle, Panes: panes}
}

func tree(tabs ...herdr.Tab) herdr.HerdrTree {
	return herdr.HerdrTree{
		Version: "test",
		Workspaces: []herdr.Workspace{
			{ID: "w1", Label: "contr", Status: idle, Focused: true, Tabs: tabs},
		},
	}
}

func checkout(root, common string, isMain bool) git.Checkout {
	return git.Checkout{Root: root, GitDir: common, CommonDir: common, IsMain: isMain}
}

func entry(root, name, branch string, isMain bool) git.WorktreeEntry {
	return git.WorktreeEntry{Root: root, Name: name, Branch: branch, IsMain: isMain}
}

func repo(common, name string, worktrees ...git.WorktreeEntry) git.RepoInfo {
	r := git.RepoInfo{CommonDir: common, Name: name, MainBranch: "main", Worktrees: worktrees}
	if len(worktrees) > 0 {
		r.MainRoot = worktrees[0].Root
	}
	return r
}

// appScan is repo app: main checkout on main, linked w1 and w2.
func appScan() git.Scan {
	scan := git.Scan{Checkouts: map[string]git.Checkout{}, Repos: map[string]git.RepoInfo{}}
	scan.Checkouts[mainWT] = checkout(mainWT, common, true)
	scan.Checkouts[w1Root] = checkout(w1Root, common, false)
	scan.Checkouts[w2Root] = checkout(w2Root, common, false)
	scan.Repos[common] = repo(common, "app",
		entry(mainWT, "app", "main", true),
		entry(w1Root, "w1", "agent/6-x", false),
		entry(w2Root, "w2", "agent/6-y", false),
	)
	return scan
}

// withTool is appScan plus repo tool, which has only its main checkout.
func withTool() git.Scan {
	const toolCommon = "/work/tool/.git"
	scan := appScan()
	scan.Checkouts["/work/tool"] = checkout("/work/tool", toolCommon, true)
	scan.Repos[toolCommon] = repo(toolCommon, "tool", entry("/work/tool", "tool", "main", true))
	return scan
}

func built(tabs ...herdr.Tab) []RepoGroup { return Build(tree(tabs...), appScan()) }

func row(t *testing.T, groups []RepoGroup, name string) WorktreeRow {
	t.Helper()
	for _, g := range groups {
		for _, r := range g.Rows {
			if r.Name == name {
				return r
			}
		}
	}
	t.Fatalf("no row %q", name)
	return WorktreeRow{}
}

func names(g RepoGroup) []string {
	var out []string
	for _, r := range g.Rows {
		out = append(out, r.Name)
	}
	return out
}

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReposFollowTheirFirstPaneInTreeOrder(t *testing.T) {
	groups := Build(tree(
		tab("t1", "tool", pane("w1:p1", "/work/tool", st(idle))),
		tab("t2", "app", pane("w1:p2", w1Root, st(working))),
		tab("t3", "tool-2", pane("w1:p3", "/work/tool", nil)),
	), withTool())
	var order []string
	for _, g := range groups {
		order = append(order, g.Name)
	}
	eq(t, order, []string{"tool", "app"})
	eq(t, groups[0].Branch, "main")
	eq(t, groups[1].CommonDir, common)
}

func TestWorkingBuilderTabBeatsDonePlannerTab(t *testing.T) {
	groups := built(
		tab("t1", "p-6", pane("w1:p1", w1Root, st(done))),
		tab("t2", "b-3s2", pane("w1:p2", w1Root, st(working))),
	)
	w1 := row(t, groups, "w1")
	eq(t, w1.Status, icons.Working)
	eq(t, *w1.FocusPane, "w1:p2")
	eq(t, *w1.Hold, "b-3s2 working +1")
	eq(t, len(w1.Holders), 2)
}

func TestEqualStatesTakeTheNewestTab(t *testing.T) {
	groups := built(
		tab("t1", "old", pane("w1:p1", w1Root, st(working))),
		tab("t2", "new", pane("w1:p2", w1Root, st(working))),
	)
	w1 := row(t, groups, "w1")
	eq(t, *w1.FocusPane, "w1:p2")
	eq(t, *w1.Hold, "new working +1")
}

func TestBlockedBeatsWorking(t *testing.T) {
	groups := built(
		tab("t1", "a", pane("w1:p1", w1Root, st(working))),
		tab("t2", "b", pane("w1:p2", w1Root, st(blocked))),
	)
	w1 := row(t, groups, "w1")
	eq(t, w1.Status, icons.Blocked)
	eq(t, *w1.FocusPane, "w1:p2")
	eq(t, *w1.Hold, "b blocked +1")
}

func TestPaneWithoutAgentHoldsAsUnknownAndShowsIdle(t *testing.T) {
	groups := built(
		tab("t1", "shell", pane("w1:p1", w1Root, nil)),
		tab("t2", "agent", pane("w1:p2", w2Root, st(unknown))),
	)
	w1 := row(t, groups, "w1")
	eq(t, w1.Holders[0].Status, unknown)
	eq(t, w1.Status, icons.Idle)
	eq(t, *w1.Hold, "shell unknown")
	w2 := row(t, groups, "w2")
	eq(t, w2.Status, icons.Idle)
	eq(t, *w2.FocusPane, "w1:p2")
}

func TestATabKeepsItsBestPaneInEachWorktree(t *testing.T) {
	groups := built(tab("t1", "multi",
		pane("w1:p1", w1Root, st(idle)),
		pane("w1:p2", w1Root, st(working)),
		pane("w1:p3", w1Root, st(idle)),
	))
	w1 := row(t, groups, "w1")
	eq(t, len(w1.Holders), 1)
	eq(t, w1.Holders[0].PaneID, "w1:p2")
}

func TestOneTabHoldsEveryWorktreeItHasPanesIn(t *testing.T) {
	groups := built(tab("t1", "multi",
		pane("w1:p1", w1Root, st(working)),
		pane("w1:p2", w2Root, st(idle)),
	))
	w1 := row(t, groups, "w1")
	eq(t, w1.Holders[0].TabID, "t1")
	eq(t, *w1.FocusPane, "w1:p1")
	w2 := row(t, groups, "w2")
	eq(t, w2.Holders[0].TabID, "t1")
	eq(t, *w2.FocusPane, "w1:p2")
}

func TestMainCheckoutRowOnlyWhenATabHoldsIt(t *testing.T) {
	unheld := built(tab("t1", "a", pane("w1:p1", w1Root, st(idle))))
	eq(t, names(unheld[0]), []string{"w1", "w2"})
	held := built(tab("t1", "a", pane("w1:p1", mainWT, st(idle))))
	eq(t, names(held[0]), []string{"app", "w1", "w2"})
	if !held[0].Rows[0].IsMain {
		t.Error("first row is not main")
	}
}

func TestUnheldLinkedWorktreeIsIdleWithNoFocusOrHold(t *testing.T) {
	groups := built(tab("t1", "a", pane("w1:p1", w1Root, st(working))))
	w2 := row(t, groups, "w2")
	eq(t, w2.Status, icons.Idle)
	if w2.FocusPane != nil || w2.Hold != nil {
		t.Error("unheld row has focus or hold")
	}
	eq(t, len(w2.Holders), 0)
	eq(t, w2.Branch, "agent/6-y")
}

func TestPanesOutsideGitAndWithoutACwdAreLeftOut(t *testing.T) {
	noCwd := pane("w1:p2", w1Root, st(working))
	noCwd.Cwd = nil
	groups := built(tab("t1", "scratch", pane("w1:p1", "/elsewhere", st(working)), noCwd))
	eq(t, len(groups), 0)
}

func TestACheckoutTheRepoDoesNotListGivesNoGroup(t *testing.T) {
	scan := appScan()
	const ghost = "/work/ghost/.git"
	scan.Checkouts["/work/ghost"] = checkout("/work/ghost", ghost, true)
	scan.Repos[ghost] = repo(ghost, "ghost")
	groups := Build(tree(tab("t1", "a", pane("w1:p1", "/work/ghost", st(working)))), scan)
	eq(t, len(groups), 0)
}

func TestACheckoutWhoseRepoIsMissingGivesNoGroup(t *testing.T) {
	scan := appScan()
	scan.Repos = map[string]git.RepoInfo{}
	groups := Build(tree(tab("t1", "a", pane("w1:p1", w1Root, st(working)))), scan)
	eq(t, len(groups), 0)
}

func TestRankPutsBlockedFirstAndUnknownLast(t *testing.T) {
	states := []herdr.AgentState{blocked, working, done, idle, unknown}
	for i := 1; i < len(states); i++ {
		if Rank(states[i-1]) <= Rank(states[i]) {
			t.Errorf("rank(%d) <= rank(%d)", states[i-1], states[i])
		}
	}
}

func TestRowIDIsItsRootPath(t *testing.T) {
	groups := built(tab("t1", "a", pane("w1:p1", w1Root, st(idle))))
	eq(t, row(t, groups, "w1").ID(), w1Root)
}

func TestPaneCwdsAreListedOnceInTreeOrderAndSkipMissingCwds(t *testing.T) {
	noCwd := pane("w1:p4", w2Root, st(idle))
	noCwd.Cwd = nil
	cwds := PaneCwds(tree(
		tab("t1", "a", pane("w1:p1", w1Root, nil), pane("w1:p2", mainWT, nil)),
		tab("t2", "b", pane("w1:p3", w1Root, nil), noCwd),
	))
	eq(t, cwds, []string{w1Root, mainWT})
	eq(t, len(PaneCwds(tree())), 0)
}

// mark is a mark with the given label and who, no open item, not blocked.
func mark(label, text string, kind board.WhoKind) board.PaneMark {
	return board.PaneMark{Label: label, Who: board.Who{Text: text, Kind: kind}}
}

func marksOn(paneID string, m board.PaneMark) map[string]board.PaneMark {
	return map[string]board.PaneMark{paneID: m}
}

func TestNoMarkKeepsTheHerdrFallback(t *testing.T) {
	groups := built(tab("t1", "b-3s2", pane("w1:p2", w1Root, st(working))))
	ApplyMarks(groups, map[string]board.PaneMark{})
	w1 := row(t, groups, "w1")
	eq(t, *w1.Hold, "b-3s2 working")
	if w1.Badge != nil || w1.HoldYou {
		t.Error("badge or hold-you set without a mark")
	}
}

func TestAMarkReplacesTheSecondLineAndKeepsPlusN(t *testing.T) {
	groups := built(
		tab("t1", "p-6", pane("w1:p1", w1Root, st(done))),
		tab("t2", "b-3s2", pane("w1:p2", w1Root, st(working))),
	)
	ApplyMarks(groups, marksOn("w1:p2", mark("#640", "b-1151 5m", board.WhoText)))
	w1 := row(t, groups, "w1")
	eq(t, *w1.Hold, "b-1151 5m +1")
	eq(t, w1.HoldYou, false)
	eq(t, *w1.Badge, Badge{Kind: BadgePR, PR: 640})
}

func TestAYouWhoSetsHoldYou(t *testing.T) {
	groups := built(tab("t1", "b-3s2", pane("w1:p2", w1Root, st(working))))
	ApplyMarks(groups, marksOn("w1:p2", mark("#640", "your test", board.WhoYou)))
	w1 := row(t, groups, "w1")
	eq(t, *w1.Hold, "your test")
	eq(t, w1.HoldYou, true)
}

func TestABlankWhoKeepsTheFallbackLine(t *testing.T) {
	for _, text := range []string{"", "   "} {
		groups := built(tab("t1", "b-3s2", pane("w1:p2", w1Root, st(working))))
		ApplyMarks(groups, marksOn("w1:p2", mark("#640", text, board.WhoYou)))
		w1 := row(t, groups, "w1")
		eq(t, *w1.Hold, "b-3s2 working")
		eq(t, w1.HoldYou, false)
		eq(t, *w1.Badge, Badge{Kind: BadgePR, PR: 640})
	}
}

func TestBadgeOrderItemThenBlockedThenPR(t *testing.T) {
	with := func(item *string, blocked bool, label string) board.PaneMark {
		return board.PaneMark{Label: label, OpenItem: item, Blocked: blocked}
	}
	eq(t, *badgeOf(with(sp("D7"), true, "#640")), Badge{Kind: BadgeYou, You: "D7"})
	eq(t, *badgeOf(with(nil, true, "#640")), Badge{Kind: BadgeYou, You: "blocked"})
	eq(t, *badgeOf(with(nil, false, "#640")), Badge{Kind: BadgePR, PR: 640})
	for _, label := range []string{"fix", "#", "#12a", "#+5", "640", "#99999999999"} {
		if b := badgeOf(with(nil, false, label)); b != nil {
			t.Errorf("label %q gave badge %v", label, *b)
		}
	}
}

func TestAMarkOnALowerHolderIsUsedWhenTheTopHasNone(t *testing.T) {
	groups := built(
		tab("t1", "p-6", pane("w1:p1", w1Root, st(done))),
		tab("t2", "b-3s2", pane("w1:p2", w1Root, st(working))),
	)
	// The top holder is w1:p2, which has no mark. The mark is on w1:p1.
	ApplyMarks(groups, marksOn("w1:p1", mark("#641", "r-1152 2m", board.WhoText)))
	w1 := row(t, groups, "w1")
	eq(t, *w1.FocusPane, "w1:p2")
	eq(t, *w1.Hold, "r-1152 2m +1")
	eq(t, *w1.Badge, Badge{Kind: BadgePR, PR: 641})
}

func TestAnUnheldRowGetsNoMark(t *testing.T) {
	groups := built(tab("t1", "a", pane("w1:p1", w1Root, st(working))))
	ApplyMarks(groups, marksOn("w1:p1", mark("#640", "b-1151 5m", board.WhoYou)))
	w2 := row(t, groups, "w2")
	if w2.Hold != nil || w2.Badge != nil || w2.HoldYou {
		t.Error("unheld row got a mark")
	}
}
