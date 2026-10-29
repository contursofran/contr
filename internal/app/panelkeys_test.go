package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/board"
)

// answerD7 is the answer to D7 with option index and label.
func answerD7(index uint64, label string) board.Action { return board.Answer("D7", index, label) }

// wantKeysQueued checks the actions file: no arguments mean no file at all,
// otherwise the actions in order with their seqs.
func (r *panelRig) wantKeysQueued(want ...board.Action) {
	r.t.Helper()
	if len(want) == 0 {
		want = nil
	}
	r.wantQueued(want, "keys")
}

// pickedNeed is the key of the item that the needs section draws as picked.
func (r *panelRig) pickedNeed() string { return r.pickedItem() }

// Probe (decision D23). A click on a need row, a ButtonBase, leaves the focus
// where the button takes it, but the key still reaches the root: the panel's
// shortcuts run for a focus inside the root. A click on the summary, which is
// not focusable, focuses the root. Both must give Enter to the panel.
func TestProbeKeysReachThePanelAfterAClickOnARowOrTheSummary(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 1))
	r.key(ui.KeyEnter)
	r.advance(board.UndoMS)
	r.wantKeysQueued(answerD7(0, "one line per row"))

	r2 := newQueuedPanelRig(t, snap)
	r2.click(LabelBoardSummary)
	if !r2.tt.Focused(LabelBoardRoot) {
		t.Fatal("a click on the summary does not focus the panel")
	}
	r2.key(ui.KeyEnter)
	r2.advance(board.UndoMS)
	r2.wantKeysQueued(board.Done("T12"))
}

// Port of the Rust test the_keys_do_nothing_until_the_panel_has_focus.
func TestTheKeysDoNothingUntilThePanelHasFocus(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	for _, k := range []ui.Key{ui.KeyDown, ui.KeyEnter, ui.Key1} {
		r.key(k)
	}
	if r.p.pick != nil {
		t.Errorf("the pick moved to %v", *r.p.pick)
	}
	r.advance(board.UndoMS)
	r.wantKeysQueued()
}

// The keys do not reach the panel while another view, such as the terminal,
// holds the focus.
func TestTheKeysDoNotReachThePanelWhileAnotherViewHasFocus(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	keys := 0
	r.extra = func(c *ui.Context) {
		ui.Box(c).Label("terminal view").Grow(1).FillHeight().Focusable().FocusRing(false).
			HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown {
					keys++
				}
				return true
			}).Children(func() { ui.Text(c, "terminal") })
	}
	r.tt.Frame()
	r.click("terminal view")
	r.key(ui.KeyDown)
	r.key(ui.KeyEnter)
	r.advance(board.UndoMS)
	if r.p.pick != nil {
		t.Errorf("the pick moved to %v", *r.p.pick)
	}
	r.wantKeysQueued()
	if keys == 0 {
		t.Error("the terminal view took no key")
	}
}

// Port of the Rust test the_arrow_keys_move_the_pick_through_the_items.
func TestTheArrowKeysMoveThePickThroughTheItems(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(LabelBoardSummary)
	steps := []struct {
		keys []ui.Key
		want string
		note string
	}{
		{[]ui.Key{ui.KeyUp}, "T12", "up from the first item stays"},
		{[]ui.Key{ui.KeyDown}, "D7", "down moves to D7"},
		{[]ui.Key{ui.KeyDown, ui.KeyDown}, "D6", "down reaches D6"},
		{[]ui.Key{ui.KeyDown}, "D6", "down stops at D6"},
		{[]ui.Key{ui.KeyUp}, "N4", "up moves to N4"},
	}
	for _, s := range steps {
		for _, k := range s.keys {
			r.key(k)
		}
		if got := r.pickedNeed(); got != s.want {
			t.Errorf("%s: picked %q, want %q", s.note, got, s.want)
		}
	}
	if r.p.pick == nil || *r.p.pick != ItemPick("N4") {
		t.Errorf("pick = %v", r.p.pick)
	}
}

// Port of the Rust test down_from_a_picked_row_goes_to_the_first_item.
func TestDownFromAPickedRowGoesToTheFirstItem(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(rowLabel(snap, 0))
	r.key(ui.KeyUp)
	if r.p.work.picked != snap.Rows[0].Key || r.pickedNeed() != "" {
		t.Error("up keeps the row")
	}
	r.key(ui.KeyDown)
	if r.pickedNeed() != "T12" || r.p.work.picked != "" {
		t.Errorf("down picks T12: item %q, row %q", r.pickedNeed(), r.p.work.picked)
	}
	if r.p.pick == nil || *r.p.pick != ItemPick("T12") {
		t.Errorf("pick = %v", r.p.pick)
	}
}

// Port of the Rust test enter_on_an_open_decision_queues_its_recommended_answer.
func TestEnterOnAnOpenDecisionQueuesItsRecommendedAnswer(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 1))
	r.key(ui.KeyEnter)
	if !r.has(CardUndo) {
		t.Error("the answer can be undone")
	}
	r.wantKeysQueued()
	r.advance(board.UndoMS)
	r.wantKeysQueued(answerD7(0, "one line per row"))
}

// Port of the Rust test enter_on_a_to_do_sends_done_and_enter_on_news_dismisses.
func TestEnterOnAToDoSendsDoneAndEnterOnNewsDismisses(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(LabelBoardSummary)
	r.key(ui.KeyEnter)
	r.advance(board.UndoMS)
	r.wantKeysQueued(board.Done("T12"))

	r.click(itemLabel(snap, 2))
	r.key(ui.KeyEnter)
	r.advance(board.UndoMS)
	r.wantKeysQueued(board.Done("T12"), board.Dismiss("N4"))
}

// Port of the Rust test
// enter_does_nothing_on_a_resolved_item_or_while_an_answer_is_held.
func TestEnterDoesNothingOnAResolvedItemOrWhileAnAnswerIsHeld(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 3))
	r.key(ui.KeyEnter)
	r.wantKeysQueued()

	r.click(itemLabel(snap, 1))
	r.key(ui.KeyEnter)
	r.key(ui.KeyEnter)
	r.advance(board.UndoMS)
	r.wantKeysQueued(answerD7(0, "one line per row"))
}

// Port of the Rust test digit_1_sends_the_first_option_in_card_order.
func TestDigit1SendsTheFirstOptionInCardOrder(t *testing.T) {
	snap := panelFixture(t)
	one := uint(1)
	snap.Items[1].Recommended = &one
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 1))
	r.key(ui.Key1)
	r.advance(board.UndoMS)
	r.wantKeysQueued(answerD7(1, "two lines per row"))
}

// Port of the Rust test digit_2_sends_the_second_option_in_card_order.
func TestDigit2SendsTheSecondOptionInCardOrder(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 1))
	r.key(ui.Key2)
	r.advance(board.UndoMS)
	r.wantKeysQueued(answerD7(1, "two lines per row"))
}

// Port of the Rust test digits_send_nothing_past_the_last_option_or_on_a_to_do.
func TestDigitsSendNothingPastTheLastOptionOrOnAToDo(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(LabelBoardSummary)
	r.key(ui.Key1)
	if !r.has(CardDone) {
		t.Error("T12 is still open")
	}
	r.wantKeysQueued()

	r.click(itemLabel(snap, 1))
	r.key(ui.Key4)
	r.advance(board.UndoMS)
	r.wantKeysQueued()
	if r.has(CardUndo) {
		t.Error("nothing is held")
	}
}

// Port of the Rust test the_report_tab_ignores_the_arrow_keys_enter_and_digits.
func TestTheReportTabIgnoresTheArrowKeysEnterAndDigits(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	press := func(keys ...ui.Key) {
		for _, k := range keys {
			r.key(k)
		}
	}
	r.click(rowLabel(snap, 0))
	r.click(LabelTabReport)
	press(ui.KeyDown, ui.KeyUp, ui.KeyEnter, ui.Key1)
	if r.p.pick == nil || *r.p.pick != RowPick(snap.Rows[0].Key) {
		t.Errorf("the row stays picked: %v", r.p.pick)
	}

	r.click(LabelTabBoard)
	r.click(itemLabel(snap, 1))
	r.click(LabelTabReport)
	press(ui.KeyDown, ui.KeyUp, ui.KeyEnter, ui.Key1, ui.Key2)
	if r.p.pick == nil || *r.p.pick != ItemPick("D7") {
		t.Errorf("the item stays picked: %v", r.p.pick)
	}
	r.advance(board.UndoMS)
	r.wantKeysQueued()

	r.click(LabelTabBoard)
	if !r.has("one line per row") || r.has(CardUndo) {
		t.Error("D7 is still open and nothing is held")
	}
}

// Port of the Rust test an_inert_board_moves_the_pick_and_sends_nothing.
func TestAnInertBoardMovesThePickAndSendsNothing(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	inert := panelFixture(t)
	inert.Actions = 0
	r.set(inert)
	if r.p.CanAct() {
		t.Fatal("the board takes actions")
	}
	r.click(LabelBoardSummary)
	r.key(ui.KeyDown)
	if r.pickedNeed() != "D7" {
		t.Errorf("the pick moves: %q", r.pickedNeed())
	}
	r.key(ui.KeyEnter)
	r.key(ui.Key1)
	r.advance(board.UndoMS)
	r.wantKeysQueued()
	if st := r.status(); len(st) != 0 {
		t.Errorf("status = %v", st)
	}
	if !r.has("one line per row") {
		t.Error("the options stay")
	}
}
