package app

// The work section of the board: one row per worker of the project, with
// the issue, the title, "<role> <status>" and four stage columns. The stage
// of a worker comes from its role (D46): a planner is in plan, a builder in
// build, a reviewer in review. Earlier stages show done and later ones not
// started. The you column lights when the worker is blocked or an open
// question or to-do names it. A click on a row selects the worker (#162,
// S6).

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the work section.
const (
	LabelWorkSection = "work section"
	LabelWorkHeader  = "work header"
	LabelWorkRows    = "work rows"
	LabelWorkEmpty   = "No workers yet."
)

// LabelWorkPR is the label of a work row's issue button.
func LabelWorkPR(label string) string { return "pr " + label }

// StageHeads are the stage columns: plan, build, review, you.
var StageHeads = [4]string{"plan", "build", "review", "you"}

// The stage columns.
const (
	StagePlan = iota
	StageBuild
	StageReview
	StageYou
)

// WorkRow is one worker on the board.
type WorkRow struct {
	Address string
	// Dir and PR open the pull request pane from the label. PR is 0 when the worker has no
	// issue number or no worktree.
	Dir    string
	PR     int
	Label  string // "#162", or the name when the worker has no issue
	Title  string
	Role   string
	Status string // the word shown: working, blocked, done, ended …
	Stages [4]icons.Status
}

// RoleStage is the column of a role. A role the board does not know counts
// as build.
func RoleStage(role string) int {
	switch role {
	case "planner":
		return StagePlan
	case "reviewer":
		return StageReview
	}
	return StageBuild
}

// StatusWord is the status shown for w: its own word, "ended" once the
// program ended, "" when it never said.
func StatusWord(w agents.Worker) string {
	if !w.Live {
		return "ended"
	}
	return w.Status
}

// workIcon is the icon of the worker's own stage.
func workIcon(w agents.Worker) icons.Status {
	switch StatusWord(w) {
	case "working":
		return icons.Working
	case "blocked", "error":
		return icons.Blocked
	case "done", "ended":
		return icons.Done
	case "idle":
		return icons.Idle
	}
	return icons.Waiting
}

// WorkRows returns the rows for project ("" for all projects), in the order
// of the snapshot. Orchestrators are not rows.
func WorkRows(snap agentstate.Snapshot, project string) []WorkRow {
	var rows []WorkRow
	for _, w := range snap.Workers {
		if project != "" && w.Project != project {
			continue
		}
		row := WorkRow{Address: w.Address, Label: w.Name, Title: w.Name, Role: w.Role, Status: StatusWord(w)}
		if w.Issue != "" {
			row.Label = "#" + strings.TrimPrefix(w.Issue, "#")
			if n, err := strconv.Atoi(strings.TrimPrefix(w.Issue, "#")); err == nil && w.Worktree != "" {
				row.Dir, row.PR = w.Worktree, n
			}
		}
		own := RoleStage(w.Role)
		for i := range own {
			row.Stages[i] = icons.Done
		}
		row.Stages[own] = workIcon(w)
		for i := own + 1; i < StageYou; i++ {
			row.Stages[i] = icons.NotStarted
		}
		row.Stages[StageYou] = icons.NotStarted
		if w.Status == "blocked" || waitsOnOwner(snap, w) {
			row.Stages[StageYou] = icons.NeedsYou
		}
		rows = append(rows, row)
	}
	return rows
}

// waitsOnOwner tells whether an open question or to-do names the worker or
// its issue.
func waitsOnOwner(snap agentstate.Snapshot, w agents.Worker) bool {
	names := func(from string) bool {
		return from != "" && (from == w.Name || (w.Issue != "" && strings.TrimPrefix(from, "#") == strings.TrimPrefix(w.Issue, "#")))
	}
	for _, q := range snap.Questions {
		if names(q.From) {
			return true
		}
	}
	for _, t := range snap.Todos {
		if names(t.From) {
			return true
		}
	}
	return false
}

// WorkRowLabel is the accessible name of a row.
func WorkRowLabel(row WorkRow) string {
	stages := make([]string, len(StageHeads))
	for i, head := range StageHeads {
		stages[i] = head + " " + row.Stages[i].Label()
	}
	return row.Label + " " + row.Title + ", " + row.Role + " " + row.Status + ", " + strings.Join(stages, ", ")
}

// WorkSection adds the section: the head with the stage heads, then the rows
// or the empty line. onPick gets the address of a clicked row.
func WorkSection(c *ui.Context, rows []WorkRow, onPick func(address string), onPR func(dir string, n int)) {
	ui.Column(c).Label(LabelWorkSection).FillWidth().Shrink(0).MaxHeightPercent(60).Children(func() {
		ui.Row(c).Label(LabelWorkHeader).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
			AlignItems(ui.Center).PaddingX(theme.PanelPadX).Children(func() {
			ui.Text(c, "Work").Grow(1).MinWidth(0).TextColor(theme.Color(theme.TextDim))
			for _, head := range StageHeads {
				ui.Row(c).Width(theme.StageColWidth).Shrink(0).Justify(ui.Center).Children(func() {
					ui.Text(c, head).TextColor(theme.Color(theme.TextDim))
				})
			}
		})
		if len(rows) == 0 {
			TextLine(c, LabelWorkEmpty, LabelWorkEmpty, theme.TextDim)
			return
		}
		ui.Scroll(c).Label(LabelWorkRows).FillWidth().MinHeight(0).Children(func() {
			for _, row := range rows {
				workRow(c, row, onPick, onPR)
			}
		})
	})
}

func workRow(c *ui.Context, row WorkRow, onPick func(address string), onPR func(dir string, n int)) {
	pick := func() {
		if onPick != nil {
			onPick(row.Address)
		}
	}
	label := func() {
		ui.Text(c, row.Label).SingleLine().Ellipsis("…").TextColor(theme.Color(theme.TextDim))
	}
	rest := func() {
		ui.Column(c).Grow(1).MinWidth(0).Justify(ui.Center).Children(func() {
			ui.Text(c, row.Title).Height(theme.LineHeight).SingleLine().Ellipsis("…").MinWidth(0).
				TextColor(theme.Color(theme.Text))
			ui.Text(c, strings.TrimSpace(row.Role+" "+row.Status)).Height(theme.LineHeight).SingleLine().
				Ellipsis("…").MinWidth(0).TextColor(theme.Color(theme.Text2))
		})
		for _, st := range row.Stages {
			ui.Row(c).Width(theme.StageColWidth).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				icons.Sized(c, st, theme.IconSizeSmall)
			})
		}
	}
	if row.PR > 0 && onPR != nil {
		// The number opens the pull request pane (D59). Its button sits beside the row's
		// button, not inside it, so both are separate elements.
		ui.Row(c).FillWidth().Height(theme.WorkRowHeight).Shrink(0).PaddingStart(theme.PanelPadX).Children(func() {
			ui.ButtonBase(c).Label(LabelWorkPR(row.Label)).Width(theme.WorkLabelWidth).FillHeight().Shrink(0).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).OnClick(func() { onPR(row.Dir, row.PR) }).Children(label)
			ui.ButtonBase(c).Label(WorkRowLabel(row)).Grow(1).MinWidth(0).FillHeight().
				AlignItems(ui.Center).PaddingEnd(theme.PanelPadX).Radius(theme.CardRadius).
				Cursor(ui.CursorPointer).OnClick(pick).Children(rest)
		})
		return
	}
	ui.ButtonBase(c).Label(WorkRowLabel(row)).FillWidth().Height(theme.WorkRowHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.PanelPadX).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).OnClick(pick).Children(func() {
		ui.Text(c, row.Label).Width(theme.WorkLabelWidth).Shrink(0).SingleLine().Ellipsis("…").
			TextColor(theme.Color(theme.TextDim))
		rest()
	})
}
