package app

import (
	"testing"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

// Port of the Rust test the_panel_opens_on_the_board_tab.
func TestThePanelOpensOnTheBoardTab(t *testing.T) {
	r := newPanelRig(t, nil, "", nil)
	if r.p.Tab() != TabBoard {
		t.Errorf("tab = %d", r.p.Tab())
	}
	if TabBoard != 0 {
		t.Error("TabBoard is not the zero tab")
	}
}

// Port of the Rust test report_target_follows_the_row_key.
func TestReportTargetFollowsTheRowKey(t *testing.T) {
	snap := panelFixture(t)
	if got := reportTarget(snap, nil); got.Kind != TargetNone {
		t.Errorf("no key: %+v", got)
	}
	gone := "owner/name#999"
	if got := reportTarget(snap, &gone); got.Kind != TargetGone {
		t.Errorf("gone key: %+v", got)
	}
	key := "owner/name#640"
	want := ReportTarget{Kind: TargetRow, Source: SourceFromRow(snap.Rows[0])}
	if got := reportTarget(snap, &key); got.Kind != TargetRow || got.Source.Key != key || got.Source != want.Source {
		t.Errorf("row key: %+v, want %+v", got, want)
	}
}

// Port of the Rust test empty_text_names_the_state.
func TestEmptyTextNamesTheState(t *testing.T) {
	cases := []struct {
		bridge  bool
		problem string
		want    string
	}{
		{false, "", "no coordinator board"},
		{false, "x", "no coordinator board"},
		{true, "", "loading board…"},
		{true, board.NoBoardFile, board.NoBoardFile},
	}
	for _, c := range cases {
		if got := EmptyText(c.bridge, c.problem); got != c.want {
			t.Errorf("EmptyText(%v, %q) = %q, want %q", c.bridge, c.problem, got, c.want)
		}
	}
}

// Port of the Rust test summary_text_adds_a_problem_after_the_summary.
func TestSummaryTextAddsAProblemAfterTheSummary(t *testing.T) {
	cases := []struct{ summary, problem, want string }{
		{"2 of 2 busy", "", "2 of 2 busy"},
		{"2 of 2 busy", "board v2 · this contr reads v1", "2 of 2 busy · board v2 · this contr reads v1"},
		{"", "no board file yet", "no board file yet"},
	}
	for _, c := range cases {
		if got := SummaryText(c.summary, c.problem); got != c.want {
			t.Errorf("SummaryText(%q, %q) = %q, want %q", c.summary, c.problem, got, c.want)
		}
	}
}

// Port of the Rust test wrap_text_follows_wrap_up.
func TestWrapTextFollowsWrapUp(t *testing.T) {
	if got := WrapText(false); got != "wrap up" {
		t.Errorf("WrapText(false) = %q", got)
	}
	if got := WrapText(true); got != "keep going" {
		t.Errorf("WrapText(true) = %q", got)
	}
}

// wrapFlip is the pure part of the wrap click, which a later slice wires.
func TestWrapFlipSendsTheOtherStateWithItsStatusLine(t *testing.T) {
	if a, line := wrapFlip(false); a != board.WrapUp(true) || line != "wrap up sent" {
		t.Errorf("flip from off: %+v %q", a, line)
	}
	if a, line := wrapFlip(true); a != board.WrapUp(false) || line != "keep going sent" {
		t.Errorf("flip from on: %+v %q", a, line)
	}
}

// Port of the Rust test no_bridge_shows_the_empty_line_and_no_summary.
func TestNoBridgeShowsTheEmptyLineAndNoSummary(t *testing.T) {
	r := newPanelRig(t, nil, "", nil)
	if !r.has(LabelBoardEmpty) {
		t.Error("no empty line")
	}
	if !r.tt.HasText("no coordinator board") {
		t.Errorf("texts %v", r.tt.Texts())
	}
	for _, label := range []string{LabelBoardSummary, LabelWorkSection, LabelBoardWrap} {
		if r.has(label) {
			t.Errorf("%q shows without a snapshot", label)
		}
	}
	if !r.has(LabelBoardHeader) {
		t.Error("no header")
	}
}

// Port of the Rust test a_problem_without_a_snapshot_is_the_empty_line.
func TestAProblemWithoutASnapshotIsTheEmptyLine(t *testing.T) {
	r := newPanelRig(t, nil, board.NoBoardFile, testBridge())
	if !r.has(LabelBoardEmpty) || !r.tt.HasText(board.NoBoardFile) {
		t.Errorf("texts %v", r.tt.Texts())
	}
	if r.has(LabelBoardSummary) {
		t.Error("summary shows without a snapshot")
	}
}

// Port of the Rust test a_snapshot_shows_the_header_summary_and_sections.
func TestASnapshotShowsTheHeaderSummaryAndSections(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	if r.has(LabelBoardEmpty) {
		t.Error("empty line with a snapshot")
	}
	for _, label := range []string{LabelBoardSummary, LabelWorkSection, LabelNeedsSection} {
		if !r.has(label) {
			t.Errorf("missing %q in %v", label, r.tt.Texts())
		}
	}
	root, header := r.rect(LabelBoardRoot), r.rect(LabelBoardHeader)
	if header.H != theme.PanelHeaderHeight {
		t.Errorf("header height %v", header.H)
	}
	if header.Y != root.Y {
		t.Errorf("the header is at %v, the panel at %v", header.Y, root.Y)
	}
	if r.rect(LabelTabBoard).X >= r.rect(LabelTabReport).X {
		t.Error("Board is not left of Report")
	}
}

// Port of the Rust test the_wrap_label_follows_wrap_up.
func TestTheWrapLabelFollowsWrapUp(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	if !r.has(LabelBoardWrap) || !r.tt.HasText("wrap up") || r.tt.HasText("keep going") {
		t.Errorf("texts %v", r.tt.Texts())
	}
	next := panelFixture(t)
	next.WrapUp = true
	r.set(next)
	if !r.tt.HasText("keep going") || r.tt.HasText("wrap up") {
		t.Errorf("texts %v", r.tt.Texts())
	}
}

// Port of the Rust test a_problem_keeps_the_summary_on_screen.
func TestAProblemKeepsTheSummaryOnScreen(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, board.NoBoardFile, testBridge())
	if !r.has(LabelBoardSummary) || r.has(LabelBoardEmpty) {
		t.Errorf("texts %v", r.tt.Texts())
	}
	if !r.tt.HasText(snap.Summary + " · " + board.NoBoardFile) {
		t.Errorf("the problem does not follow the summary: %v", r.tt.Texts())
	}
}

// Port of the Rust test
// the_needs_section_shows_the_snapshot_and_ends_where_the_card_begins.
func TestTheNeedsSectionShowsTheSnapshotAndEndsWhereTheCardBegins(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	if !r.has(NeedsCountLabel(3)) {
		t.Errorf("no count in %v", r.tt.Texts())
	}
	if !r.has(NeedLabel(snap.Items[3])) {
		t.Errorf("no resolved row in %v", r.tt.Texts())
	}
	// The item card shares its name with the item's row, so the card's top is
	// found from the panel bottom: the card is a fixed height, one margin up.
	section, host := r.rect(LabelNeedsSection), r.rect("panel host")
	cardTop := host.Y + host.H - theme.AnswerMargin - theme.AnswerCardHeight
	if got := section.Y + section.H; got != cardTop {
		t.Errorf("the section ends at %v, the card begins at %v", got, cardTop)
	}
}

// Port of the Rust test the_report_tab_is_clickable_and_selected_when_open.
func TestTheReportTabIsClickableAndSelectedWhenOpen(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	if r.p.Tab() != TabBoard {
		t.Fatal("not on the Board tab")
	}
	r.click(LabelTabReport)
	if r.p.Tab() != TabReport {
		t.Errorf("tab = %d after the click", r.p.Tab())
	}
	if !r.has(LabelTabBoard) {
		t.Error("the Board tab is gone")
	}
}

// Port of the Rust test the_report_tab_without_a_row_shows_the_pick_line.
func TestTheReportTabWithoutARowShowsThePickLine(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.click(LabelTabReport)
	if !r.has(LabelReport) || !r.has(LabelReportEmpty) || !r.tt.HasText(ReportNoRow) {
		t.Errorf("texts %v", r.tt.Texts())
	}
	if r.has(LabelReportTitle) {
		t.Error("a title without a row")
	}
	if n := r.reads(); n != 0 {
		t.Errorf("no row, but %d reads", n)
	}
}

// Port of the Rust test board_tab_and_back_return_to_the_board.
func TestBoardTabAndBackReturnToTheBoard(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.click(LabelTabReport)
	if !r.has(LabelReport) {
		t.Fatal("no report")
	}

	r.click(LabelTabBoard)
	if r.has(LabelReport) || !r.has(LabelWorkSection) || r.p.Tab() != TabBoard {
		t.Errorf("the Board tab did not return: %v", r.tt.Texts())
	}

	r.click(LabelTabReport)
	r.click(LabelReportBack)
	if r.has(LabelReport) || !r.has(LabelNeedsSection) || r.p.Tab() != TabBoard {
		t.Errorf("back did not return: %v", r.tt.Texts())
	}
}

// Port of the Rust test show_report_opens_the_rows_report.
func TestShowReportOpensTheRowsReport(t *testing.T) {
	raw := reportFile(t)
	r := newPanelRig(t, snapshotWithReport(t, raw), "", testBridge())
	r.showReport(row640)

	if r.p.Tab() != TabReport {
		t.Errorf("tab = %d", r.p.Tab())
	}
	if !r.has(LabelReportTitle) || !r.has("report-heading-0") {
		t.Errorf("texts %v", r.tt.Texts())
	}
	if n := r.reads(); n != 1 {
		t.Errorf("reads = %d, want 1", n)
	}
}

// Port of the Rust test a_new_board_rev_rereads_the_shown_report.
func TestANewBoardRevRereadsTheShownReport(t *testing.T) {
	raw := reportFile(t)
	r := newPanelRig(t, snapshotWithReport(t, raw), "", testBridge())
	r.showReport(row640)
	if n := r.reads(); n != 1 {
		t.Fatalf("reads = %d, want 1", n)
	}

	next := snapshotWithReport(t, raw)
	next.Rev = 43
	r.set(next)
	if n := r.reads(); n != 2 {
		t.Errorf("a new revision reads %d times, want 2", n)
	}
}

// Port of the Rust test the_board_tab_reads_no_report.
func TestTheBoardTabReadsNoReport(t *testing.T) {
	raw := reportFile(t)
	r := newPanelRig(t, snapshotWithReport(t, raw), "", testBridge())
	next := snapshotWithReport(t, raw)
	next.Rev = 43
	r.set(next)
	if n := r.reads(); n != 0 {
		t.Errorf("the Board tab read %d reports", n)
	}

	r.showReport(row640)
	r.click(LabelTabBoard)
	next = snapshotWithReport(t, raw)
	next.Rev = 44
	r.set(next)
	if n := r.reads(); n != 1 {
		t.Errorf("after the Board tab, reads = %d, want 1", n)
	}
}

// Port of the Rust test a_row_that_leaves_the_board_shows_the_gone_line.
func TestARowThatLeavesTheBoardShowsTheGoneLine(t *testing.T) {
	raw := reportFile(t)
	r := newPanelRig(t, snapshotWithReport(t, raw), "", testBridge())
	r.showReport(row640)

	gone := snapshotWithReport(t, raw)
	gone.Rows = nil
	gone.Rev = 43
	r.set(gone)

	if !r.has(LabelReportEmpty) || !r.tt.HasText(ReportRowGone) {
		t.Errorf("no gone line in %v", r.tt.Texts())
	}
	if r.has(LabelReportTitle) {
		t.Error("a title for a row that left")
	}
	if n := r.reads(); n != 1 {
		t.Errorf("a gone row read: reads = %d, want 1", n)
	}
}

// Port of the Rust test the_report_tab_hides_summary_and_sections.
func TestTheReportTabHidesSummaryAndSections(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.click(LabelTabReport)
	for _, label := range []string{LabelBoardSummary, LabelWorkSection, LabelNeedsSection, LabelCard} {
		if r.has(label) {
			t.Errorf("%q shows on the Report tab", label)
		}
	}
	if !r.has(LabelBoardHeader) {
		t.Error("the header is gone")
	}
	root, report := r.rect(LabelBoardRoot), r.rect(LabelReport)
	if got, want := report.Y+report.H, root.Y+root.H; got != want {
		t.Errorf("the report ends at %v, the panel at %v", got, want)
	}
}

// Port of the Rust test no_snapshot_shows_board_empty_on_the_report_tab.
func TestNoSnapshotShowsBoardEmptyOnTheReportTab(t *testing.T) {
	r := newPanelRig(t, nil, "", nil)
	r.click(LabelTabReport)
	if r.p.Tab() != TabReport || !r.has(LabelBoardEmpty) || r.has(LabelReport) {
		t.Errorf("texts %v", r.tt.Texts())
	}
	if n := r.reads(); n != 0 {
		t.Errorf("reads = %d", n)
	}
}

// Port of the Rust test a_panel_without_a_queue_sends_nothing.
func TestAPanelWithoutAQueueSendsNothing(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	if r.p.sendAction(board.ClearRows(), ClearSent) {
		t.Error("no queue, but the action was sent")
	}
	if r.p.CanAct() {
		t.Error("CanAct without a queue")
	}
	if got := r.status(); len(got) != 0 {
		t.Errorf("status lines %v", got)
	}
}

// The next tests have no Rust twin: they cover the branches that the Go
// panel has of its own.

func TestAPanelShowsTheRowsOfTheBoardItHoldsAtOnce(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	if got := len(r.p.work.Rows()); got != len(snap.Rows) {
		t.Errorf("work rows = %d, want %d", got, len(snap.Rows))
	}
	if !r.has(WorkRowLabel(snap.Rows[0])) {
		t.Errorf("no first row in %v", r.tt.Texts())
	}
}

func TestSyncFollowsTheBoardRows(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	next := panelFixture(t)
	next.Rows = next.Rows[:1]
	r.set(next)
	if got := len(r.p.work.Rows()); got != 1 {
		t.Errorf("work rows = %d, want 1", got)
	}
	r.set(nil)
	if got := len(r.p.work.Rows()); got != 0 {
		t.Errorf("work rows = %d without a snapshot, want 0", got)
	}
}

func TestSyncOnTheReportTabWithoutASnapshotShowsNothingNew(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.click(LabelTabReport)
	r.set(nil)
	if !r.has(LabelBoardEmpty) || r.has(LabelReport) {
		t.Errorf("texts %v", r.tt.Texts())
	}
}

func TestTheReportTabFollowsAPickedRow(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.p.SetPick(RowPick(row640))
	r.click(LabelTabReport)
	if r.p.reportRow == nil || *r.p.reportRow != row640 {
		t.Errorf("report row %v", r.p.reportRow)
	}
	if !r.has(LabelReportTitle) {
		t.Errorf("texts %v", r.tt.Texts())
	}
}

func TestTheReportTabKeepsItsRowWhenAnItemPickIsSet(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.showReport(row640)
	r.p.SetPick(ItemPick("D7"))
	r.click(LabelTabBoard)
	r.click(LabelTabReport)
	if r.p.reportRow == nil || *r.p.reportRow != row640 {
		t.Errorf("report row %v", r.p.reportRow)
	}
}

func TestTheSwitchMarksTheShownTabSelected(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	// The Selected state sets accent colours that the design does not use;
	// the segments must stay greyscale on both tabs.
	for _, open := range []func(){r.p.OpenBoardTab, r.p.OpenReportTab} {
		open()
		r.settle()
		img := r.tt.Image()
		for _, label := range []string{LabelTabBoard, LabelTabReport} {
			rc := r.rect(label)
			for _, off := range [][2]float32{{1, 1}, {rc.W / 2, 2}, {rc.W - 2, rc.H - 2}} {
				px := img.RGBAAt(int(rc.X+off[0]), int(rc.Y+off[1]))
				if px.R != px.G || px.G != px.B {
					t.Errorf("%s has a colour %v on the %d tab", label, px, r.p.Tab())
				}
			}
		}
	}
}

func TestAnActionIsSentAndShowsItsFeedback(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	if !r.p.CanAct() {
		t.Fatal("the queued panel cannot act")
	}
	if !r.p.sendAction(board.ClearRows(), ClearSent) {
		t.Fatal("not sent")
	}
	r.settle()
	if got := r.status(); len(got) != 1 || got[0] != ClearSent {
		t.Errorf("status lines %v", got)
	}
}

func TestAnActionWithoutFeedbackShowsNoStatusLine(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	if !r.p.sendAction(board.ClearRows(), "") {
		t.Fatal("not sent")
	}
	r.settle()
	if got := r.status(); len(got) != 0 {
		t.Errorf("status lines %v", got)
	}
}

func TestABoardThatTakesNoActionsRefusesTheSendWithoutFeedback(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	inert := panelFixture(t)
	inert.Actions = 0
	r.set(inert)
	if r.p.CanAct() {
		t.Fatal("an inert board can act")
	}
	if r.p.sendAction(board.ClearRows(), ClearSent) {
		t.Error("an inert board took the action")
	}
	if got := r.status(); len(got) != 0 {
		t.Errorf("status lines %v", got)
	}
}

// Port of the Rust test wrap_flip_sends_the_other_state_with_its_status.
func TestWrapFlipSendsTheOtherStateWithItsStatus(t *testing.T) {
	for _, tc := range []struct {
		wrapUp bool
		want   board.Action
		line   string
	}{
		{false, board.WrapUp(true), "wrap up sent"},
		{true, board.WrapUp(false), "keep going sent"},
	} {
		if a, line := wrapFlip(tc.wrapUp); a != tc.want || line != tc.line {
			t.Errorf("wrapFlip(%v) = %v, %q", tc.wrapUp, a, line)
		}
	}
}

// Port of the Rust test wrap_up_click_queues_wrap_up_and_shows_the_status.
func TestWrapUpClickQueuesWrapUpAndShowsTheStatus(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	r.click(LabelBoardWrap)
	got, ok := r.queued()
	if !ok || len(got) != 1 || got[0].Action != board.WrapUp(true) {
		t.Fatalf("queued = %v, %v", got, ok)
	}
	if st := r.status(); len(st) != 1 || st[0] != "wrap up sent" {
		t.Errorf("status = %v", st)
	}
}

// Port of the Rust test keep_going_click_queues_wrap_up_off.
func TestKeepGoingClickQueuesWrapUpOff(t *testing.T) {
	snap := panelFixture(t)
	snap.WrapUp = true
	r := newQueuedPanelRig(t, snap)
	r.click(LabelBoardWrap)
	got, ok := r.queued()
	if !ok || len(got) != 1 || got[0].Action != board.WrapUp(false) {
		t.Fatalf("queued = %v, %v", got, ok)
	}
	if st := r.status(); len(st) != 1 || st[0] != "keep going sent" {
		t.Errorf("status = %v", st)
	}
}

// Port of the Rust test clear_done_on_the_work_section_queues_clear_rows.
func TestClearDoneOnTheWorkSectionQueuesClearRows(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	r.click(LabelClearWork)
	got, ok := r.queued()
	if !ok || len(got) != 1 || got[0].Action != board.ClearRows() {
		t.Fatalf("queued = %v, %v", got, ok)
	}
	if st := r.status(); len(st) != 1 || st[0] != ClearSent {
		t.Errorf("status = %v", st)
	}
}

// Port of the Rust test clear_done_on_the_needs_section_queues_clear_items.
func TestClearDoneOnTheNeedsSectionQueuesClearItems(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	r.click(LabelClearNeed)
	got, ok := r.queued()
	if !ok || len(got) != 1 || got[0].Action != board.ClearItems() {
		t.Fatalf("queued = %v, %v", got, ok)
	}
	if st := r.status(); len(st) != 1 || st[0] != ClearSent {
		t.Errorf("status = %v", st)
	}
}

// Port of the Rust test a_board_that_takes_no_actions_queues_nothing. The
// controls are inert, and a direct send is refused and shows nothing.
func TestABoardThatTakesNoActionsQueuesNothing(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	next := panelFixture(t)
	next.Actions = 0
	r.set(next)
	if r.p.CanAct() {
		t.Fatal("CanAct on a board that takes no actions")
	}
	for _, label := range []string{LabelBoardWrap, LabelClearWork, LabelClearNeed} {
		r.click(label)
	}
	r.p.sendWrap()
	r.p.sendClear(true)
	r.p.sendClear(false)
	r.settle()
	if got, ok := r.queued(); ok && len(got) != 0 {
		t.Errorf("queued = %v", got)
	}
	if st := r.status(); len(st) != 0 {
		t.Errorf("status = %v", st)
	}
}

// Port of the Rust test the_wrap_label_follows_the_next_snapshot. After the
// board changes, a click sends the flip of the new state.
func TestTheWrapLabelFollowsTheNextSnapshot(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	next := panelFixture(t)
	next.WrapUp = true
	r.set(next)
	if !r.tt.HasText("keep going") || r.tt.HasText("wrap up") {
		t.Fatalf("texts %v", r.tt.Texts())
	}
	r.click(LabelBoardWrap)
	got, ok := r.queued()
	if !ok || len(got) != 1 || got[0].Action != board.WrapUp(false) {
		t.Fatalf("queued = %v, %v", got, ok)
	}
}

func TestWrapLabelColourFollowsClickAndHover(t *testing.T) {
	if wrapColour(false, true) != theme.TextDim || wrapColour(false, false) != theme.TextDim {
		t.Error("an inert wrap label must be dim")
	}
	if wrapColour(true, false) != theme.Text2 {
		t.Error("a clickable wrap label must be Text2")
	}
	if wrapColour(true, true) != theme.Text {
		t.Error("a hovered wrap label must be Text")
	}
}
