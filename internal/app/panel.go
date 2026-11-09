package app

// The board panel: the work section over the needs-you section, for the
// project of the selected orchestrator (all projects when none is selected).
// It reads the agent state on every build and keeps nothing of its own, so
// it needs no sync (#162, S6). There is no Wrap up or Keep going control
// (owner, 2026-11-03): the owner types "wrap up" in the orchestrator terminal.

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelBoardRoot and LabelBoardEmpty name the panel and its empty line.
const (
	LabelBoardRoot    = "board root"
	LabelBoardEmpty   = "board empty"
	LabelBoardProblem = "board problem"
	emptyNoAgents     = "no agents"
)

// BoardPanel is the board. Build it with its fields set; all may be nil.
type BoardPanel struct {
	// Snapshot reads the agent state; nil shows "no agents".
	Snapshot func() agentstate.Snapshot
	// Scope returns the project to show, or "" for all projects.
	Scope func() string
	// Needs returns the tabs of the needs-you section, the same as the row
	// under the terminal.
	Needs func() []NeedTab
	// Select runs when a work row is clicked, with the worker's address.
	Select func(address string)
	// Open runs when a needs-you line is clicked.
	Open func(NeedTab)
	// OpenPR runs when a work row's number is clicked, with the worktree and the number.
	OpenPR func(dir string, n int)
	// OpenIssue runs when the number of a work row with no pull request is clicked, with the
	// worktree and the issue number (D74).
	OpenIssue func(dir string, n int)
	// PRs gives each worker's pull request number (D104). Nil: no worker has one.
	PRs *ghsource.WorkerPRs
}

// Rows returns the work rows now.
func (p *BoardPanel) Rows() []WorkRow {
	if p.Snapshot == nil {
		return nil
	}
	project := ""
	if p.Scope != nil {
		project = p.Scope()
	}
	rows := WorkRows(p.Snapshot(), project)
	for i := range rows {
		if n := p.PRs.PR(rows[i].Dir); n > 0 {
			rows[i].PR, rows[i].Label = n, "#"+strconv.Itoa(n)
		}
	}
	return rows
}

// View builds the panel.
func (p *BoardPanel) View(c *ui.Context) {
	ui.Column(c).Label(LabelBoardRoot).Fill().Children(func() {
		if p.Snapshot == nil {
			TextLine(c, LabelBoardEmpty, emptyNoAgents, theme.Current().Ink50)
			return
		}
		WorkSection(c, p.Rows(), p.Select, p.OpenPR, p.OpenIssue)
		if prob := p.Snapshot().Problem; prob != "" {
			TextLine(c, LabelBoardProblem, prob, theme.Current().Ink50)
		}
		var tabs []NeedTab
		if p.Needs != nil {
			tabs = p.Needs()
		}
		NeedsSection(c, tabs, p.Open)
	})
}
