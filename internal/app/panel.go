// Ported from the Rust contr module board_panel.

package app

// The board panel: the header with the Board | Report switch and the wrap
// label, then the body of the shown tab. The Board tab draws the summary
// line, the work section, the needs-you section and the answer card. The
// Report tab draws the report of the row it follows, and nothing else.
//
// The panel is a plain struct that the frame syncs, not an entity with
// observers (decision D16): [BoardPanel.Sync] runs on the UI thread after the
// board changed. The report view and the outbox redraw through Invalidate.
// A report is read only while the Report tab is shown.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// Labels of the panel. Tests find parts by label (decision D14).
const (
	LabelBoardRoot    = "board root"
	LabelBoardHeader  = "board header"
	LabelBoardSwitch  = "board switch"
	LabelTabBoard     = "Board tab"
	LabelTabReport    = "Report tab"
	LabelBoardWrap    = "board wrap"
	LabelBoardSummary = "board summary"
	LabelBoardEmpty   = "board empty"
)

// Status lines of the sends, and the empty-board lines.
const (
	ClearSent       = "clear done sent"
	wrapUpSent      = "wrap up sent"
	keepGoingSent   = "keep going sent"
	emptyNoBoard    = "no coordinator board"
	emptyLoading    = "loading board…"
	wrapUpText      = "wrap up"
	keepGoingText   = "keep going"
	tabBoardText    = "Board"
	tabReportText   = "Report"
	summarySeparate = " · "
)

// OpenFn opens a URL (Open PR, Open link). The app passes nil: the panel
// then uses c.OpenURL. Tests record.
type OpenFn func(c *ui.Context, url string)

// PanelTab is a tab of the panel.
type PanelTab uint8

const (
	// TabBoard is the default tab.
	TabBoard PanelTab = iota
	// TabReport follows the row in reportRow.
	TabReport
)

// PanelDeps are the panel's inputs. A nil Outbox means no control sends.
type PanelDeps struct {
	Board  *Board
	Outbox *BoardOutbox // may be nil
	// Tree gives the herdr tree for the Focus label; may be nil.
	Tree func() *herdr.HerdrTree
	// Status shows a line in the status bar; may be nil.
	Status func(string)
	// Focus focuses a pane; nil means [FocusInBackground].
	Focus FocusFn
	// Open opens a URL; nil means c.OpenURL.
	Open OpenFn
	// OpenPath opens a file; nil means [OpenInSystem].
	OpenPath OpenPathFn
	// Invalidate asks for a redraw. The report reads and the outbox call it.
	Invalidate func()
}

// BoardPanel is the board panel. Its methods run on the UI thread.
type BoardPanel struct {
	deps PanelDeps
	tab  PanelTab
	work *WorkList
	// pick is the picked row or item; nil shows the first open item.
	pick *BoardPick
	// report is the Report tab's view.
	report *ReportView
	// reportRow is the key of the work row that the Report tab follows. It
	// stays when the tab is left.
	reportRow *string
}

// NewBoardPanel returns a panel on the Board tab.
func NewBoardPanel(d PanelDeps) *BoardPanel {
	p := &BoardPanel{deps: d, work: NewWorkList(nil)}
	p.report = NewReportView(d.Invalidate)
	p.report.Open = d.OpenPath
	p.report.OnBack = p.OpenBoardTab
	p.syncWork(d.Board.View().Snapshot)
	return p
}

// Tab returns the tab that the panel shows.
func (p *BoardPanel) Tab() PanelTab { return p.tab }

// CanAct tells whether the board takes actions now: a queue is held and the
// board is live. The panel's buttons send only when this is true.
func (p *BoardPanel) CanAct() bool { return p.deps.Outbox != nil && p.deps.Outbox.CanAct() }

// Sync brings the panel in line with the board. Call it on the UI thread
// after the board changed. On the Report tab it also syncs the report.
func (p *BoardPanel) Sync() {
	v := p.deps.Board.View()
	p.syncWork(v.Snapshot)
	if p.tab == TabReport {
		p.syncReport(v.Snapshot)
	}
	p.invalidate()
}

// syncWork gives the work section the rows of s (none without a snapshot).
func (p *BoardPanel) syncWork(s *board.Snapshot) {
	var rows []board.WorkRow
	if s != nil {
		rows = s.Rows
	}
	p.work.Sync(rows)
}

func (p *BoardPanel) invalidate() {
	if p.deps.Invalidate != nil {
		p.deps.Invalidate()
	}
}

// sendAction sends action through the queue. A sent action shows feedback,
// when there is one, in the status bar. It returns false, and shows nothing,
// when the queue sends nothing: no queue, or a board that takes no actions.
func (p *BoardPanel) sendAction(action board.Action, feedback string) bool {
	if p.deps.Outbox == nil || !p.deps.Outbox.Send(action) {
		return false
	}
	if feedback != "" && p.deps.Status != nil {
		p.deps.Status(feedback)
	}
	return true
}

// sendWrap sends the other wrap state: wrap up when the coordinator goes on,
// keep going when it wraps up. It reads the board as it is now.
func (p *BoardPanel) sendWrap() {
	s := p.deps.Board.View().Snapshot
	if s == nil {
		return
	}
	action, line := wrapFlip(s.WrapUp)
	p.sendAction(action, line)
}

// sendClear sends clear done for the work rows (rows) or the needs-you items.
func (p *BoardPanel) sendClear(rows bool) {
	action := board.ClearItems()
	if rows {
		action = board.ClearRows()
	}
	p.sendAction(action, ClearSent)
}

// wrapClick, clearRowsClick and clearItemsClick give the click of a control,
// or nil, so that the control is drawn inert, while the board takes no
// actions.
func (p *BoardPanel) wrapClick() ActionClick {
	if !p.CanAct() {
		return nil
	}
	return p.sendWrap
}

func (p *BoardPanel) clearClick(rows bool) ActionClick {
	if !p.CanAct() {
		return nil
	}
	return func() { p.sendClear(rows) }
}

// ShowReport shows the Report tab for the work row rowKey. The card's Report
// button calls it.
func (p *BoardPanel) ShowReport(rowKey string) {
	p.reportRow = &rowKey
	p.enterReportTab()
}

// SetPick picks pick. The pick is held here; the card and the sections that
// show it come in a later slice.
func (p *BoardPanel) SetPick(pick BoardPick) {
	p.pick = &pick
	p.invalidate()
}

// OpenReportTab is the Report tab's click. A picked work row becomes the row
// the tab follows. Otherwise the tab keeps the row it followed before.
func (p *BoardPanel) OpenReportTab() {
	if p.pick != nil && p.pick.Kind == PickRow {
		key := p.pick.Key
		p.reportRow = &key
	}
	p.enterReportTab()
}

func (p *BoardPanel) enterReportTab() {
	p.tab = TabReport
	p.syncReport(p.deps.Board.View().Snapshot)
	p.invalidate()
}

// OpenBoardTab is the Board tab's click, and `back` in the report. The report
// row stays.
func (p *BoardPanel) OpenBoardTab() {
	p.tab = TabBoard
	p.invalidate()
}

// syncReport gives the report view the row it follows, as the board is now.
// The view reads the file only when its path or the board revision is new.
// With no snapshot there is no report to show, so nothing is set.
func (p *BoardPanel) syncReport(s *board.Snapshot) {
	if s == nil {
		return
	}
	p.report.Show(reportTarget(s, p.reportRow), s.Rev)
}

// reportTarget is what the Report tab shows for the row key on s: nothing
// without a key, the row's fields while the row is on the board, else gone.
func reportTarget(s *board.Snapshot, key *string) ReportTarget {
	if key == nil {
		return ReportTarget{Kind: TargetNone}
	}
	for _, row := range s.Rows {
		if row.Key == *key {
			return ReportTarget{Kind: TargetRow, Source: SourceFromRow(row)}
		}
	}
	return ReportTarget{Kind: TargetGone}
}

// EmptyText is the line in place of the board while there is no snapshot: no
// bridge, a problem, or a read in progress.
func EmptyText(bridge bool, problem string) string {
	switch {
	case !bridge:
		return emptyNoBoard
	case problem != "":
		return problem
	default:
		return emptyLoading
	}
}

// SummaryText is the summary line. With a problem, the problem follows the
// summary after " · ". With an empty summary, the problem is the whole line.
func SummaryText(summary, problem string) string {
	switch {
	case problem == "":
		return summary
	case summary == "":
		return problem
	default:
		return summary + summarySeparate + problem
	}
}

// WrapText is the wrap label: "keep going" while the coordinator wraps up,
// else "wrap up".
func WrapText(wrapUp bool) string {
	if wrapUp {
		return keepGoingText
	}
	return wrapUpText
}

// wrapFlip is the action that the wrap label sends, with its status line:
// the other wrap state.
func wrapFlip(wrapUp bool) (board.Action, string) {
	if wrapUp {
		return board.WrapUp(false), keepGoingSent
	}
	return board.WrapUp(true), wrapUpSent
}

// View builds the panel.
func (p *BoardPanel) View(c *ui.Context) {
	v := p.deps.Board.View()
	ui.Column(c).Label(LabelBoardRoot).Fill().Children(func() {
		p.header(c, v.Snapshot)
		switch {
		case v.Snapshot == nil:
			TextLine(c, LabelBoardEmpty, EmptyText(v.Bridge != nil, v.Problem), theme.TextDim)
		case p.tab == TabReport:
			ui.Column(c).FillWidth().Grow(1).MinHeight(0).Children(func() { p.report.View(c) })
		default:
			p.boardTab(c, v.Snapshot, v.Problem)
		}
	})
}

// boardTab adds the summary, the work section, the needs-you section and the
// answer card. The card is inert in this slice.
func (p *BoardPanel) boardTab(c *ui.Context, s *board.Snapshot, problem string) {
	colour := uint32(theme.Text2)
	if problem != "" {
		colour = theme.TextDim
	}
	TextLine(c, LabelBoardSummary, SummaryText(s.Summary, problem), colour)
	p.work.View(c, s.ClearRows, p.clearClick(true))
	NeedsSection(c, s.Items, s.NeedCount, s.ClearItems, "", nil, p.clearClick(false))
	AnswerCard(c, nil, nowMS())
}

// header adds the Board | Report switch on the left and, with a snapshot, the
// wrap label on the right.
func (p *BoardPanel) header(c *ui.Context, s *board.Snapshot) {
	ui.Row(c).Label(LabelBoardHeader).FillWidth().Height(theme.PanelHeaderHeight).Shrink(0).
		AlignItems(ui.Center).Justify(ui.SpaceBetween).PaddingX(theme.PanelPadX).Children(func() {
		ui.Row(c).Label(LabelBoardSwitch).AlignItems(ui.Center).Children(func() {
			switchSegment(c, LabelTabBoard, tabBoardText, p.tab == TabBoard, p.OpenBoardTab)
			switchSegment(c, LabelTabReport, tabReportText, p.tab == TabReport, p.OpenReportTab)
		})
		if s != nil {
			wrapLabel(c, s.WrapUp, p.wrapClick())
		}
	})
}

// switchSegment adds one segment of the switch. The selected one is a card with a
// 1 px border. The border is always there (transparent when not selected), so
// the labels do not move when the selection does.
func switchSegment(c *ui.Context, label, text string, on bool, click func()) {
	// Selected comes first: it sets accent colours that the design does not
	// use, and the colours below replace them.
	el := ui.ButtonBase(c).Label(label).Selected(on).Cursor(ui.CursorPointer).
		PaddingX(theme.SegmentPadX).PaddingY(theme.SegmentPadY).Radius(theme.SegmentRadius).
		OnClick(click)
	if on {
		el = el.Border(1, theme.Color(theme.CardBorder)).
			Background(theme.Color(theme.BgCard)).TextColor(theme.Color(theme.Text))
	} else {
		el = el.Border(1, ui.Transparent).TextColor(theme.Color(theme.TextDim))
	}
	el.Children(func() { ui.Text(c, text) })
}

// wrapLabel adds the wrap label on the right of the header. It is dim. It
// is a button when click is set, else inert.
func wrapLabel(c *ui.Context, wrapUp bool, click ActionClick) {
	text := func() { ui.Text(c, WrapText(wrapUp)).TextColor(theme.Color(theme.TextDim)) }
	if click == nil {
		ui.Row(c).Label(LabelBoardWrap).Children(text)
		return
	}
	ui.ButtonBase(c).Label(LabelBoardWrap).Cursor(ui.CursorPointer).OnClick(click).Children(text)
}
