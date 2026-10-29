package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/board"
)

// count is the number of elements in the last frame with label or text s.
func (r *panelRig) count(s string) int {
	n := 0
	for _, x := range r.tt.Texts() {
		if x == s {
			n++
		}
	}
	return n
}

// hasCardReport tells whether the card has a Report button. The Report tab
// of the header has the same text, so the button is the second one.
func (r *panelRig) hasCardReport() bool { return r.count(CardReport) > 1 }

// rowFace is the face that the card shows now, which must be a row's.
func (r *panelRig) rowFace() *RowFace {
	r.t.Helper()
	f, ok := r.p.currentFace(r.c).(*RowFace)
	if !ok {
		r.t.Fatalf("the card does not show a row: %v", r.tt.Texts())
	}
	return f
}

// pickedRow is the key of the row the work section draws as picked.
func (r *panelRig) pickedRow() string { return r.p.work.picked }

// pickedItem is the key of the item the needs section draws as picked.
func (r *panelRig) pickedItem() string {
	return r.p.pickedItemKey(r.b.View().Snapshot)
}

// rowLabel and itemLabel are the labels of the rows of the fixture.
func rowLabel(snap *board.Snapshot, i int) string  { return WorkRowLabel(snap.Rows[i]) }
func itemLabel(snap *board.Snapshot, i int) string { return NeedLabel(snap.Items[i]) }

// cardHead is true when the card draws the head line of a row or an item.
func (r *panelRig) cardHead(text string) bool { return r.has(text) }

const t12Head = "T12  Test the bag drag"

// Port of the Rust test card_report_shows_only_for_a_row_with_a_report.
func TestCardReportShowsOnlyForARowWithAReport(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	if r.hasCardReport() {
		t.Error("the item T12 is picked")
	}
	r.click(rowLabel(snap, 0))
	if !r.hasCardReport() {
		t.Error("#640 has a report")
	}
	r.click(rowLabel(snap, 1))
	if r.hasCardReport() {
		t.Error("#641 has no report")
	}
}

// Port of the Rust test card_report_opens_the_report_tab_for_that_row.
func TestCardReportOpensTheReportTabForThatRow(t *testing.T) {
	snap := snapshotWithReport(t, reportFile(t))
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 0))
	r.rowFace().ReportClick()
	r.settle()
	if r.p.Tab() != TabReport {
		t.Error("the Report tab is not shown")
	}
	if r.p.reportRow == nil || *r.p.reportRow != row640 {
		t.Errorf("report row %v", r.p.reportRow)
	}
	if !r.has("report-heading-0") {
		t.Errorf("the row's file is not drawn: %v", r.tt.Texts())
	}
	if got := r.reads(); got != 1 {
		t.Errorf("reads = %d", got)
	}
}

// Port of the Rust test back_keeps_the_pick.
func TestBackKeepsThePick(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 0))
	r.click(LabelTabReport)
	r.click(LabelReportBack)
	if r.p.Tab() != TabBoard {
		t.Error("the Board tab is not shown")
	}
	if r.pickedRow() != row640 {
		t.Errorf("picked row %q", r.pickedRow())
	}
	if !r.hasCardReport() {
		t.Error("the card does not show the row")
	}
	if r.p.pick == nil || *r.p.pick != RowPick(row640) {
		t.Errorf("pick %v", r.p.pick)
	}
}

// Port of the Rust test show_report_keeps_its_row_over_another_pick.
func TestShowReportKeepsItsRowOverAnotherPick(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 1))
	r.showReport(row640)
	if r.p.Tab() != TabReport {
		t.Error("the Report tab is not shown")
	}
	if r.p.reportRow == nil || *r.p.reportRow != row640 {
		t.Errorf("report row %v", r.p.reportRow)
	}
}

// Port of the Rust test the_report_tab_follows_the_picked_row, through clicks
// on the rows.
func TestTheReportTabFollowsThePickedRow(t *testing.T) {
	snap := snapshotWithReport(t, reportFile(t))
	other := filepath.Join(t.TempDir(), "OTHER.md")
	if err := os.WriteFile(other, []byte("Other row, plain text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap.Rows[1].Report = &other
	r := newPanelRig(t, snap, "", testBridge())

	r.click(rowLabel(snap, 1))
	r.click(LabelTabReport)
	if *r.p.reportRow != "owner/name#641" || !r.has("report-text-0") {
		t.Errorf("row %v, texts %v", *r.p.reportRow, r.tt.Texts())
	}
	r.click(LabelTabBoard)
	r.click(rowLabel(snap, 0))
	r.click(LabelTabReport)
	if *r.p.reportRow != row640 || !r.has("report-heading-0") {
		t.Errorf("row %v, texts %v", *r.p.reportRow, r.tt.Texts())
	}
	if got := r.reads(); got != 2 {
		t.Errorf("each pick reads its own report once, reads = %d", got)
	}
}

// Port of the Rust test the_report_tab_keeps_its_row_when_an_item_is_picked,
// through a click on the item row.
func TestTheReportTabKeepsItsRowWhenAnItemIsPicked(t *testing.T) {
	snap := snapshotWithReport(t, reportFile(t))
	r := newPanelRig(t, snap, "", testBridge())
	r.showReport(row640)
	r.click(LabelTabBoard)
	r.click(itemLabel(snap, 1))
	r.click(LabelTabReport)
	if *r.p.reportRow != row640 || !r.has("report-heading-0") {
		t.Errorf("row %v, texts %v", *r.p.reportRow, r.tt.Texts())
	}
}

// Port of the Rust test the_default_pick_shows_the_first_open_item_and_no_row_buttons.
func TestTheDefaultPickShowsTheFirstOpenItemAndNoRowButtons(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	if !r.cardHead(t12Head) {
		t.Errorf("T12 is not in the head: %v", r.tt.Texts())
	}
	if r.pickedItem() != "T12" {
		t.Errorf("picked item %q", r.pickedItem())
	}
	if r.has(FocusLabel("builder")) || r.hasCardReport() || r.has(CardOpenPR) {
		t.Errorf("row buttons: %v", r.tt.Texts())
	}
}

// Port of the Rust test the_card_is_on_the_board_tab_only.
func TestTheCardIsOnTheBoardTabOnly(t *testing.T) {
	r := newPanelRig(t, panelFixture(t), "", testBridge())
	r.click(LabelTabReport)
	if r.has(LabelCard) || r.has(t12Head) || r.p.currentFace(r.c) != nil {
		t.Errorf("the card is drawn: %v", r.tt.Texts())
	}
}

// Port of the Rust test a_click_on_a_work_row_shows_its_buttons.
func TestAClickOnAWorkRowShowsItsButtons(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 0))
	if r.pickedRow() != row640 {
		t.Errorf("the row is not picked: %q", r.pickedRow())
	}
	if r.pickedItem() != "" {
		t.Errorf("T12 is picked: %q", r.pickedItem())
	}
	if !r.has("#640  sort bags") {
		t.Errorf("no head: %v", r.tt.Texts())
	}
	if !r.has(FocusLabel("builder")) || !r.hasCardReport() || !r.has(CardOpenPR) {
		t.Errorf("buttons: %v", r.tt.Texts())
	}
}

// Port of the Rust test focus_records_the_pane_and_open_pr_the_url.
func TestFocusRecordsThePaneAndOpenPrTheUrl(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 0))
	r.click(FocusLabel("builder"))
	if !slices.Equal(r.focused, []string{"w1:p4"}) {
		t.Errorf("focused %v", r.focused)
	}
	r.click(CardOpenPR)
	if !slices.Equal(r.opened, []string{"https://example.com/pull/40"}) {
		t.Errorf("opened %v", r.opened)
	}
}

// Port of the Rust test report_shows_the_report_tab_for_the_row.
func TestReportShowsTheReportTabForTheRow(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 0))
	r.rowFace().ReportClick()
	r.settle()
	if r.p.Tab() != TabReport || r.p.reportRow == nil || *r.p.reportRow != row640 {
		t.Errorf("tab %d, row %v", r.p.Tab(), r.p.reportRow)
	}
}

// Port of the Rust test a_pane_missing_from_the_tree_has_no_focus_button.
func TestAPaneMissingFromTheTreeHasNoFocusButton(t *testing.T) {
	snap := panelFixture(t)
	// #641 has pane w1:p2 and a URL, but the tree holds only w1:p4.
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 1))
	if r.has(FocusLabel("builder")) {
		t.Error("a focus button")
	}
	if !r.has(CardOpenPR) || r.hasCardReport() {
		t.Errorf("buttons: %v", r.tt.Texts())
	}
}

// Port of the Rust test without_a_herdr_tree_a_row_has_no_focus_button.
func TestWithoutAHerdrTreeARowHasNoFocusButton(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 0))
	if r.rowFace().FocusClick != nil || !r.has(CardOpenPR) {
		t.Errorf("buttons: %v", r.tt.Texts())
	}
}

// Port of the Rust test a_click_on_an_item_row_picks_the_item.
func TestAClickOnAnItemRowPicksTheItem(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 0))
	r.click(itemLabel(snap, 1))
	if r.pickedItem() != "D7" || r.pickedRow() != "" {
		t.Errorf("item %q, row %q", r.pickedItem(), r.pickedRow())
	}
	if !r.has("D7  Row layout") || r.has(FocusLabel("builder")) {
		t.Errorf("card: %v", r.tt.Texts())
	}
}

// Port of the Rust test the_row_pick_survives_a_new_snapshot_with_the_same_keys.
func TestTheRowPickSurvivesANewSnapshotWithTheSameKeys(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 0))
	next := panelFixture(t)
	next.Rev = 43
	r.set(next)
	if r.pickedRow() != row640 || !r.has(FocusLabel("builder")) {
		t.Errorf("row %q, texts %v", r.pickedRow(), r.tt.Texts())
	}
}

// Port of the Rust test a_picked_row_that_leaves_the_board_falls_back_to_the_first_open_item.
func TestAPickedRowThatLeavesTheBoardFallsBackToTheFirstOpenItem(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.click(rowLabel(snap, 0))
	gone := panelFixture(t)
	gone.Rows = nil
	gone.Rev = 43
	r.set(gone)
	if r.pickedItem() != "T12" || !r.cardHead(t12Head) {
		t.Errorf("item %q, texts %v", r.pickedItem(), r.tt.Texts())
	}
	if r.p.pick != nil {
		t.Errorf("the gone pick is kept: %v", *r.p.pick)
	}
}

// Port of the Rust test the_row_pick_is_kept_across_the_tabs.
func TestTheRowPickIsKeptAcrossTheTabs(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge(), withTree(treeWith("builder", "w1:p4")))
	r.click(rowLabel(snap, 0))
	r.click(LabelTabReport)
	r.click(LabelTabBoard)
	if r.pickedRow() != row640 || !r.has(FocusLabel("builder")) {
		t.Errorf("row %q, texts %v", r.pickedRow(), r.tt.Texts())
	}
	if r.p.pick == nil || *r.p.pick != RowPick(row640) {
		t.Errorf("pick %v", r.p.pick)
	}
}

// With Open nil, Open PR goes to the browser through the context.
func TestOpenPrWithoutAnOpenCallOpensTheUrlInTheBrowser(t *testing.T) {
	snap := panelFixture(t)
	r := newPanelRig(t, snap, "", testBridge())
	r.p.deps.Open = nil
	r.click(rowLabel(snap, 0))
	r.click(CardOpenPR)
	if got := r.tt.OpenedURLs(); !slices.Equal(got, []string{"https://example.com/pull/40"}) {
		t.Errorf("opened %v", got)
	}
}
