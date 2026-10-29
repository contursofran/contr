// Package spaces is the spaces model: the repo groups and worktree rows the
// sidebar shows, built from the herdr tree and the git scan. It is pure: no
// file reads and no UI, so tests use literal values.
//
// Ported from the Rust contr spaces_model module.
package spaces

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/herdr"
)

// Holder is a tab that holds a worktree. Its state is the best of its panes
// in that worktree.
type Holder struct {
	TabID    string
	TabLabel string
	PaneID   string
	Status   herdr.AgentState
}

// BadgeKind says what a [Badge] is.
type BadgeKind int

// The badge kinds.
const (
	// BadgePR is a pull request number.
	BadgePR BadgeKind = iota
	// BadgeYou is a value that waits on the owner.
	BadgeYou
)

// Badge is a mark on a worktree row.
type Badge struct {
	Kind BadgeKind
	// PR is the pull request number, for [BadgePR].
	PR uint32
	// You is the text, for [BadgeYou].
	You string
}

// WorktreeRow is one worktree row: a linked worktree, or the main checkout
// when a tab holds it.
type WorktreeRow struct {
	Root string
	Name string
	// Branch is empty when unknown.
	Branch string
	IsMain bool
	// Holders has one entry per tab with a pane whose cwd is in this
	// checkout, in tree order.
	Holders []Holder
	// Status is the top holder's state as an icon. Idle without a holder.
	Status icons.Status
	// FocusPane is the top holder's pane, or nil. `pane.focus` on it also
	// moves the tab and the workspace.
	FocusPane *string
	// Hold is the second line: the top holder's tab and state. Nil without a
	// holder.
	Hold *string
	// HoldYou is true when the second line is the owner's turn.
	HoldYou bool
	// Badge comes from the board bridge. Nil without it.
	Badge *Badge
}

// ID is the id that the selection carries: the canonical root path.
func (r WorktreeRow) ID() string { return r.Root }

// RepoGroup is one repo in the sidebar, with its worktree rows.
type RepoGroup struct {
	CommonDir string
	Name      string
	// Branch is the main branch of the repo.
	Branch string
	Rows   []WorktreeRow
}

// Build groups the panes that sit in a checkout of scan into repos and
// worktree rows. Repos come in the order of their first pane in tree order.
// Panes outside git are left out.
func Build(tree herdr.HerdrTree, scan git.Scan) []RepoGroup {
	holders := map[string][]Holder{}
	var repoOrder []string
	seen := map[string]bool{}
	for _, p := range panesInTreeOrder(tree) {
		checkout, ok := checkoutOf(p.pane, scan)
		if !ok {
			continue
		}
		if !seen[checkout.CommonDir] {
			seen[checkout.CommonDir] = true
			repoOrder = append(repoOrder, checkout.CommonDir)
		}
		holders[checkout.Root] = addHolder(holders[checkout.Root], p.tab, p.pane)
	}
	var groups []RepoGroup
	for _, commonDir := range repoOrder {
		repo, ok := scan.Repos[commonDir]
		if !ok {
			continue
		}
		if g, ok := repoGroup(repo, holders); ok {
			groups = append(groups, g)
		}
	}
	return groups
}

// ApplyMarks applies the board's marks (from [board.PaneMarks]) to the rows.
// A row takes the mark of its holder's pane: the top holder first, then the
// other holders in tree order. Its second line becomes the mark's who with
// " +N" (D49), unless the who text is blank; the row keeps the herdr line
// then. A row without a mark keeps what [Build] made of it.
func ApplyMarks(groups []RepoGroup, marks map[string]board.PaneMark) {
	for g := range groups {
		for i := range groups[g].Rows {
			row := &groups[g].Rows[i]
			mark, ok := markFor(row, marks)
			if !ok {
				continue
			}
			if strings.TrimSpace(mark.Who.Text) != "" {
				line := withMore(mark.Who.Text, len(row.Holders))
				row.Hold = &line
				row.HoldYou = mark.Who.Kind == board.WhoYou
			}
			row.Badge = badgeOf(mark)
		}
	}
}

// PaneCwds gives the cwd of every pane in the tree, in tree order, each once.
// Panes without a cwd are skipped. These are the directories the git scan
// looks at.
func PaneCwds(tree herdr.HerdrTree) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range panesInTreeOrder(tree) {
		if p.pane.Cwd == nil || seen[*p.pane.Cwd] {
			continue
		}
		seen[*p.pane.Cwd] = true
		out = append(out, *p.pane.Cwd)
	}
	return out
}

// Rank is the order of agent states for choosing a row's top holder: blocked
// first, unknown last.
func Rank(state herdr.AgentState) int {
	switch state {
	case herdr.StateBlocked:
		return 4
	case herdr.StateWorking:
		return 3
	case herdr.StateDone:
		return 2
	case herdr.StateIdle:
		return 1
	}
	return 0
}

type treePane struct {
	tab  *herdr.Tab
	pane *herdr.Pane
}

// panesInTreeOrder lists every pane with its tab, workspace by workspace, tab
// by tab.
func panesInTreeOrder(tree herdr.HerdrTree) []treePane {
	var out []treePane
	for w := range tree.Workspaces {
		tabs := tree.Workspaces[w].Tabs
		for t := range tabs {
			for p := range tabs[t].Panes {
				out = append(out, treePane{&tabs[t], &tabs[t].Panes[p]})
			}
		}
	}
	return out
}

// checkoutOf gives the checkout that the pane's cwd is in, if the scan has
// one.
func checkoutOf(pane *herdr.Pane, scan git.Scan) (git.Checkout, bool) {
	if pane.Cwd == nil {
		return git.Checkout{}, false
	}
	c, ok := scan.Checkouts[*pane.Cwd]
	return c, ok
}

// addHolder records the pane for its tab in this checkout. A pane without an
// agent counts as Unknown. A tab keeps its highest-ranked pane. On a tie the
// first pane in tree order stays: only a strictly higher rank replaces it.
func addHolder(holders []Holder, tab *herdr.Tab, pane *herdr.Pane) []Holder {
	status := herdr.StateUnknown
	if pane.Agent != nil {
		status = pane.Agent.Status
	}
	for i := range holders {
		if holders[i].TabID != tab.ID {
			continue
		}
		if Rank(status) > Rank(holders[i].Status) {
			holders[i].PaneID = pane.ID
			holders[i].Status = status
		}
		return holders
	}
	return append(holders, Holder{TabID: tab.ID, TabLabel: tab.Label, PaneID: pane.ID, Status: status})
}

// repoGroup gives the repo's group, or false when it has no rows (its
// checkouts are not in its worktree list).
func repoGroup(repo git.RepoInfo, holders map[string][]Holder) (RepoGroup, bool) {
	var rows []WorktreeRow
	for _, entry := range repo.Worktrees {
		held := holders[entry.Root]
		if len(held) > 0 || !entry.IsMain {
			rows = append(rows, worktreeRow(entry, held))
		}
	}
	if len(rows) == 0 {
		return RepoGroup{}, false
	}
	return RepoGroup{CommonDir: repo.CommonDir, Name: repo.Name, Branch: repo.MainBranch, Rows: rows}, true
}

// worktreeRow builds a row. Settled (D48): every non-prunable linked worktree
// shows, dimmed and idle when no tab holds it. The main checkout shows only
// when a tab holds it, because the repo row already names it.
func worktreeRow(entry git.WorktreeEntry, held []Holder) WorktreeRow {
	row := WorktreeRow{
		Root:    entry.Root,
		Name:    entry.Name,
		Branch:  entry.Branch,
		IsMain:  entry.IsMain,
		Holders: append([]Holder(nil), held...),
		Status:  icons.Idle,
	}
	if top, ok := topHolder(held); ok {
		row.Status = icons.FromAgentState(top.Status)
		pane := top.PaneID
		hold := holdText(top, len(held))
		row.FocusPane = &pane
		row.Hold = &hold
	}
	return row
}

// topHolder gives the holder that speaks for the row: the highest rank. On a
// tie the last one wins, which is the newest tab in tree order.
func topHolder(holders []Holder) (Holder, bool) {
	if len(holders) == 0 {
		return Holder{}, false
	}
	top := holders[0]
	for _, h := range holders[1:] {
		if Rank(h.Status) >= Rank(top.Status) {
			top = h
		}
	}
	return top, true
}

// holdText is the herdr fallback for the second line (D49): "{tab label}
// {state}", plus " +N" for the N other tabs that hold the worktree (for
// example "b-3s2 working +1").
func holdText(top Holder, holderCount int) string {
	return withMore(top.TabLabel+" "+top.Status.Label(), holderCount)
}

// withMore adds " +N" to line when holderCount tabs hold the worktree, N
// being the others (D49).
func withMore(line string, holderCount int) string {
	if more := holderCount - 1; more > 0 {
		return fmt.Sprintf("%s +%d", line, more)
	}
	return line
}

// markFor gives the mark of the row's holder: the top holder's pane first,
// then the other holders in tree order. The first pane that has a mark gives
// it.
func markFor(row *WorktreeRow, marks map[string]board.PaneMark) (board.PaneMark, bool) {
	if row.FocusPane != nil {
		if m, ok := marks[*row.FocusPane]; ok {
			return m, true
		}
	}
	for _, h := range row.Holders {
		if m, ok := marks[h.PaneID]; ok {
			return m, true
		}
	}
	return board.PaneMark{}, false
}

// badgeOf gives the row's badge for a mark, in this order: the open
// needs-you item's key (T12, D8), then "blocked", then the work row's "#<n>"
// label. Any other label gives no badge.
func badgeOf(mark board.PaneMark) *Badge {
	if mark.OpenItem != nil {
		return &Badge{Kind: BadgeYou, You: *mark.OpenItem}
	}
	if mark.Blocked {
		return &Badge{Kind: BadgeYou, You: "blocked"}
	}
	digits, ok := strings.CutPrefix(mark.Label, "#")
	if !ok || digits == "" {
		return nil
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return nil
		}
	}
	n, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return nil
	}
	return &Badge{Kind: BadgePR, PR: uint32(n)}
}
