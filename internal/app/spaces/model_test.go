package spaces

import (
	"reflect"
	"testing"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/git"
)

const (
	common = "/work/app/.git"
	mainWT = "/work/app"
	w1Root = "/work/app-wt/w1"
	w2Root = "/work/app-wt/w2"
)

// term is a terminal named name, in dir, with icon s.
func term(name, dir string, s icons.Status) Term {
	return Term{Name: name, Address: "addr-" + name, Dir: dir, Status: s}
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

func built(terms ...Term) []RepoGroup { return Build(terms, appScan()) }

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

func TestReposFollowTheirFirstTerminalInOrder(t *testing.T) {
	groups := Build([]Term{
		term("t1", "/work/tool", icons.Idle),
		term("t2", w1Root, icons.Working),
		term("t3", "/work/tool", icons.Idle),
	}, withTool())
	var order []string
	for _, g := range groups {
		order = append(order, g.Name)
	}
	eq(t, order, []string{"tool", "app"})
	eq(t, groups[0].Branch, "main")
	eq(t, groups[1].CommonDir, common)
}

func TestWorkingTerminalBeatsDoneTerminal(t *testing.T) {
	groups := built(term("p-6", w1Root, icons.Done), term("b-3s2", w1Root, icons.Working))
	w1 := row(t, groups, "w1")
	eq(t, w1.Status, icons.Working)
	eq(t, *w1.Hold, "b-3s2 working +1")
	eq(t, len(w1.Holders), 2)
	eq(t, w1.Holders[1].Address, "addr-b-3s2")
}

func TestEqualStatesTakeTheNewestTerminal(t *testing.T) {
	groups := built(term("old", w1Root, icons.Working), term("new", w1Root, icons.Working))
	eq(t, *row(t, groups, "w1").Hold, "new working +1")
}

func TestBlockedBeatsWorking(t *testing.T) {
	groups := built(term("a", w1Root, icons.Working), term("b", w1Root, icons.Blocked))
	w1 := row(t, groups, "w1")
	eq(t, w1.Status, icons.Blocked)
	eq(t, *w1.Hold, "b blocked +1")
}

func TestOneHolderHasNoMoreCount(t *testing.T) {
	groups := built(term("shell", w1Root, icons.Idle))
	eq(t, *row(t, groups, "w1").Hold, "shell idle")
}

func TestTerminalsHoldTheirOwnWorktrees(t *testing.T) {
	groups := built(term("a", w1Root, icons.Working), term("b", w2Root, icons.Idle))
	eq(t, row(t, groups, "w1").Holders[0].Name, "a")
	eq(t, row(t, groups, "w2").Holders[0].Name, "b")
}

func TestMainCheckoutRowOnlyWhenATerminalHoldsIt(t *testing.T) {
	unheld := built(term("a", w1Root, icons.Idle))
	eq(t, names(unheld[0]), []string{"w1", "w2"})
	held := built(term("a", mainWT, icons.Idle))
	eq(t, names(held[0]), []string{"app", "w1", "w2"})
	if !held[0].Rows[0].IsMain {
		t.Error("first row is not main")
	}
}

func TestUnheldLinkedWorktreeIsIdleWithNoHold(t *testing.T) {
	groups := built(term("a", w1Root, icons.Working))
	w2 := row(t, groups, "w2")
	eq(t, w2.Status, icons.Idle)
	if w2.Hold != nil {
		t.Error("unheld row has a hold line")
	}
	eq(t, len(w2.Holders), 0)
	eq(t, w2.Branch, "agent/6-y")
}

func TestTerminalsOutsideGitAndWithoutADirAreLeftOut(t *testing.T) {
	groups := built(term("scratch", "/elsewhere", icons.Working), term("nodir", "", icons.Working))
	eq(t, len(groups), 0)
}

func TestACheckoutTheRepoDoesNotListGivesNoGroup(t *testing.T) {
	scan := appScan()
	const ghost = "/work/ghost/.git"
	scan.Checkouts["/work/ghost"] = checkout("/work/ghost", ghost, true)
	scan.Repos[ghost] = repo(ghost, "ghost")
	groups := Build([]Term{term("a", "/work/ghost", icons.Working)}, scan)
	eq(t, len(groups), 0)
}

func TestACheckoutWhoseRepoIsMissingGivesNoGroup(t *testing.T) {
	scan := appScan()
	scan.Repos = map[string]git.RepoInfo{}
	groups := Build([]Term{term("a", w1Root, icons.Working)}, scan)
	eq(t, len(groups), 0)
}

func TestRankPutsBlockedFirstAndNotStartedLast(t *testing.T) {
	order := []icons.Status{icons.Blocked, icons.Working, icons.Done, icons.Idle, icons.NotStarted}
	for i := 1; i < len(order); i++ {
		if Rank(order[i-1]) <= Rank(order[i]) {
			t.Errorf("rank(%v) <= rank(%v)", order[i-1], order[i])
		}
	}
}

func TestRowIDIsItsRootPath(t *testing.T) {
	groups := built(term("a", w1Root, icons.Idle))
	eq(t, row(t, groups, "w1").ID(), w1Root)
}

func TestDirsAreListedOnceInOrderAndSkipEmptyOnes(t *testing.T) {
	dirs := Dirs([]Term{
		term("a", w1Root, icons.Idle), term("b", mainWT, icons.Idle),
		term("c", w1Root, icons.Idle), term("d", "", icons.Idle),
	})
	eq(t, dirs, []string{w1Root, mainWT})
	eq(t, len(Dirs(nil)), 0)
}
