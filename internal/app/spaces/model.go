// Package spaces is the spaces model: the repo groups and worktree rows the
// sidebar shows, built from the hc terminals and the git scan. It is pure: no
// file reads and no UI, so tests use literal values.
//
// Ported from the Rust contr spaces_model module.
package spaces

import (
	"fmt"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/git"
)

// Term is an hc terminal as the sidebar sees it: an agent, an orchestrator
// or a plain terminal, with its folder (the worktree label) and its icon.
type Term struct {
	Name    string
	Address string
	Dir     string
	Status  icons.Status
}

// Holder is a terminal whose folder is in a worktree.
type Holder struct {
	Name    string
	Address string
	Status  icons.Status
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
	// Holders has one entry per terminal whose folder is in this checkout,
	// in the order of the terminals.
	Holders []Holder
	// Status is the top holder's icon. Idle without a holder.
	Status icons.Status
	// Hold is the second line: the top holder's name and state. Nil without
	// a holder.
	Hold *string
	// HoldYou is true when the second line is the owner's turn.
	HoldYou bool
	// Badge is the row's chip. Nil without one.
	Badge *Badge
	// Diff is the changed lines against the default branch. Nil when unknown.
	Diff *git.DiffStat
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
	// Path is the folder of a group that no hc terminal sits in (D116). It
	// is empty for a group that has rows.
	Path string
}

// Build groups the terminals that sit in a checkout of scan into repos and
// worktree rows. Repos come in the order of their first terminal. Terminals
// outside git are left out.
func Build(terms []Term, scan git.Scan) []RepoGroup {
	holders := map[string][]Holder{}
	var repoOrder []string
	seen := map[string]bool{}
	for _, t := range terms {
		checkout, ok := scan.Checkouts[t.Dir]
		if t.Dir == "" || !ok {
			continue
		}
		if !seen[checkout.CommonDir] {
			seen[checkout.CommonDir] = true
			repoOrder = append(repoOrder, checkout.CommonDir)
		}
		holders[checkout.Root] = append(holders[checkout.Root], Holder{Name: t.Name, Address: t.Address, Status: t.Status})
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

// Dirs gives the folder of every terminal, in order, each once. Terminals
// without a folder are skipped. These are the folders the git scan looks at.
func Dirs(terms []Term) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range terms {
		if t.Dir == "" || seen[t.Dir] {
			continue
		}
		seen[t.Dir] = true
		out = append(out, t.Dir)
	}
	return out
}

// Rank is the order of icons for choosing a row's top holder: blocked first,
// then working, done and idle.
func Rank(s icons.Status) int {
	switch s {
	case icons.Blocked:
		return 4
	case icons.Working:
		return 3
	case icons.Done:
		return 2
	case icons.Idle:
		return 1
	}
	return 0
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
// shows, dimmed and idle when no terminal holds it. The main checkout shows
// only when a terminal holds it, because the repo row already names it.
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
		row.Status = top.Status
		hold := holdText(top, len(held))
		row.Hold = &hold
	}
	return row
}

// topHolder gives the holder that speaks for the row: the highest rank. On a
// tie the last one wins.
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

// holdText is the second line (D49): "{name} {state}", plus " +N" for the
// N other terminals that hold the worktree (for example "b-3 working +1").
func holdText(top Holder, holderCount int) string {
	return withMore(top.Name+" "+top.Status.Label(), holderCount)
}

// withMore adds " +N" to line when holderCount terminals hold the worktree,
// N being the others (D49).
func withMore(line string, holderCount int) string {
	if more := holderCount - 1; more > 0 {
		return fmt.Sprintf("%s +%d", line, more)
	}
	return line
}

// ApplyStats sets the diff stat of each row whose root has one in stats.
func ApplyStats(groups []RepoGroup, stats map[string]git.DiffStat) {
	for g := range groups {
		for r := range groups[g].Rows {
			if s, ok := stats[groups[g].Rows[r].Root]; ok {
				groups[g].Rows[r].Diff = &s
			}
		}
	}
}
