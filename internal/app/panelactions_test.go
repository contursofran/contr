package app

// The item actions of the card, through the outbox: the Go form of the Rust
// card tests that use panel_with_queue_on and recording_panel.

import (
	"reflect"
	"slices"
	"testing"

	"github.com/contursofran/contr/internal/board"
)

const d7Answer = "two lines per row"

// advance moves the clock by ms and lets the outbox and the frame settle.
func (r *panelRig) advance(ms uint64) {
	r.t.Helper()
	r.clock.advance(ms)
	r.settle()
}

// itemIndex is the index of the item with key in snap.
func itemIndex(t *testing.T, snap *board.Snapshot, key string) int {
	t.Helper()
	for i, it := range snap.Items {
		if it.Key == key {
			return i
		}
	}
	t.Fatalf("no item %q", key)
	return -1
}

// recommendingSecond is the fixture whose D7 recommends its second option.
func recommendingSecond(t *testing.T) *board.Snapshot {
	snap := panelFixture(t)
	snap.Items[itemIndex(t, snap, "D7")].Recommended = uptr(1)
	return snap
}

// wantQueued checks the actions file: the actions in order with their seqs
// (seq is the 1-based position). nil want means no file.
func (r *panelRig) wantQueued(want []board.Action, why string) {
	r.t.Helper()
	got, ok := r.queued()
	if want == nil {
		if ok {
			r.t.Errorf("%s: an actions file exists: %v", why, got)
		}
		return
	}
	if !ok {
		r.t.Fatalf("%s: no actions file", why)
	}
	if len(got) != len(want) {
		r.t.Fatalf("%s: queued %v, want %v", why, got, want)
	}
	for i, e := range got {
		if e.Seq != uint64(i+1) || !reflect.DeepEqual(e.Action, want[i]) {
			r.t.Errorf("%s: queued %v, want %v", why, got, want)
		}
	}
}

// Port of the Rust test an_option_click_queues_the_answer_and_undo_takes_it_back.
func TestAnOptionClickQueuesTheAnswerAndUndoTakesItBack(t *testing.T) {
	snap := recommendingSecond(t)
	r := newQueuedPanelRig(t, snap)
	d7 := snap.Items[itemIndex(t, snap, "D7")]
	r.click(itemLabel(snap, 1))
	r.click(d7Answer)

	if !r.has(cardAnswerMark + d7Answer) {
		t.Errorf("the answer does not show: %v", r.tt.Texts())
	}
	if !r.has(CardUndo) {
		t.Error("the answer can not be undone")
	}
	resolved := d7
	resolved.Open = false
	if !r.has(NeedLabel(resolved)) {
		t.Errorf("D7 does not read as resolved: %v", r.tt.Texts())
	}
	if r.has("one line per row") {
		t.Error("the options are not gone")
	}
	if r.pickedItem() != "D7" {
		t.Errorf("the pick left D7: %q", r.pickedItem())
	}
	r.wantQueued(nil, "a held answer is not written yet")

	r.click(CardUndo)
	if !r.has("one line per row") || !r.has(d7Answer) {
		t.Errorf("the options are not back: %v", r.tt.Texts())
	}
	if r.has(cardAnswerMark + d7Answer) {
		t.Error("the answer stays")
	}
	r.advance(board.UndoMS)
	r.wantQueued(nil, "the undone answer is never written")
}

// Port of the Rust test an_answer_is_written_after_the_undo_window_and_leaves_once_acted.
func TestAnAnswerIsWrittenAfterTheUndoWindowAndLeavesOnceActed(t *testing.T) {
	r := newQueuedPanelRig(t, recommendingSecond(t))
	snap := panelFixture(t)
	r.click(itemLabel(snap, 1))
	r.click(d7Answer)

	r.advance(board.UndoMS - 1)
	r.wantQueued(nil, "the window is not over")
	if r.has(CardSending) {
		t.Error("sending shows inside the window")
	}

	r.advance(1)
	r.wantQueued([]board.Action{board.Answer("D7", 1, d7Answer)}, "after the window")
	if !r.has(CardSending) {
		t.Error("it is written, not yet taken")
	}
	if r.has(CardUndo) {
		t.Error("a written answer can not be undone")
	}

	// The mod takes seq 1 and shows D7 answered.
	taken := recommendingSecond(t)
	taken.Rev, taken.Acted = 43, 1
	d7 := &taken.Items[itemIndex(t, taken, "D7")]
	d7.Open = false
	answer := d7Answer
	d7.Answer = &answer
	r.set(taken)
	r.wantQueued([]board.Action{}, "the taken action leaves the file")
	if !r.has(cardAnswerMark + d7Answer) {
		t.Errorf("the answer does not stay on the card: %v", r.tt.Texts())
	}
	if r.has(CardSending) || r.has(CardUndo) {
		t.Errorf("sending or undo stays: %v", r.tt.Texts())
	}
}

// Port of the Rust test a_refused_answer_shows_its_status_and_the_options_again.
func TestARefusedAnswerShowsItsStatusAndTheOptionsAgain(t *testing.T) {
	r := newQueuedPanelRig(t, recommendingSecond(t))
	r.click(itemLabel(panelFixture(t), 1))
	r.click(d7Answer)
	r.advance(board.UndoMS)

	refused := recommendingSecond(t)
	refused.Rev, refused.Acted = 43, 1
	key := "D7"
	refused.Failed = []board.ActionFailure{{Seq: 1, Key: &key, Text: "options changed"}}
	r.set(refused)

	if got := r.status(); !slices.Equal(got, []string{"D7 not sent: options changed"}) {
		t.Errorf("status lines %v", got)
	}
	if r.has(cardAnswerMark + d7Answer) {
		t.Error("the optimistic answer is not dropped")
	}
	if !r.has(d7Answer) || !r.has("one line per row") {
		t.Errorf("D7 is not open again: %v", r.tt.Texts())
	}
}

// Port of the Rust test a_to_do_shows_done_and_its_link_which_opens_with_http.
func TestAToDoShowsDoneAndItsLinkWhichOpensWithHttp(t *testing.T) {
	snap := panelFixture(t)
	link := "example.com/pull/41"
	snap.Items[itemIndex(t, snap, "T12")].Link = &link
	r := newQueuedPanelRig(t, snap)

	if !r.has(CardDone) {
		t.Error("T12 is the default pick, so Done shows")
	}
	if r.has("one line per row") || r.has(CardDismiss) {
		t.Errorf("a to-do has no options and no Dismiss: %v", r.tt.Texts())
	}
	r.click(CardOpenLink)
	if !slices.Equal(r.opened, []string{"http://example.com/pull/41"}) {
		t.Errorf("opened %v", r.opened)
	}
}

// Port of the Rust test done_is_written_after_the_undo_window.
func TestDoneIsWrittenAfterTheUndoWindow(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	r.click(CardDone)
	if !r.has(cardAnswerMark + "done") {
		t.Errorf("Done does not show its answer at once: %v", r.tt.Texts())
	}
	r.wantQueued(nil, "a held Done is not written yet")

	r.advance(board.UndoMS)
	r.wantQueued([]board.Action{board.Done("T12")}, "after the window")
}

// Port of the Rust test a_news_item_shows_dismiss_which_is_written_after_the_undo_window.
func TestANewsItemShowsDismissWhichIsWrittenAfterTheUndoWindow(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 2))
	if !r.has(CardDismiss) {
		t.Errorf("no Dismiss: %v", r.tt.Texts())
	}
	if r.has(CardDone) || r.has("one line per row") {
		t.Errorf("news has no Done and no options: %v", r.tt.Texts())
	}

	r.click(CardDismiss)
	r.advance(board.UndoMS)
	r.wantQueued([]board.Action{board.Dismiss("N4")}, "after the window")
}

// Port of the Rust test next_picks_the_next_open_item.
func TestNextPicksTheNextOpenItem(t *testing.T) {
	r := newQueuedPanelRig(t, panelFixture(t))
	if !r.has(NextLabel("D7")) {
		t.Fatalf("D7 comes after T12: %v", r.tt.Texts())
	}
	r.click(NextLabel("D7"))
	if r.pickedItem() != "D7" {
		t.Errorf("picked item %q", r.pickedItem())
	}
	if r.p.pick == nil || *r.p.pick != ItemPick("D7") {
		t.Errorf("pick %v", r.p.pick)
	}
}

// Port of the Rust test answer_in_chat_sends_chat_and_focuses_the_bridge_pane.
func TestAnswerInChatSendsChatAndFocusesTheBridgePane(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.click(itemLabel(snap, 1))
	r.click(AnswerInChat)

	r.wantQueued([]board.Action{board.Chat("D7")}, "chat")
	if !slices.Equal(r.focused, []string{"w1:p1"}) {
		t.Errorf("focused %v, want the bridge pane", r.focused)
	}
}

// Port of the Rust test answer_in_chat_without_a_bridge_pane_sends_but_focuses_nothing.
func TestAnswerInChatWithoutABridgePaneSendsButFocusesNothing(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	r.b.setForTest(snap, "", nil)
	r.p.Sync()
	r.settle()

	r.click(itemLabel(snap, 1))
	r.click(AnswerInChat)
	r.wantQueued([]board.Action{board.Chat("D7")}, "chat")
	if len(r.focused) != 0 {
		t.Errorf("focused %v, there is no pane", r.focused)
	}
}

// Port of the Rust test an_inert_board_dims_the_item_buttons_and_queues_nothing.
func TestAnInertBoardDimsTheItemButtonsAndQueuesNothing(t *testing.T) {
	snap := panelFixture(t)
	r := newQueuedPanelRig(t, snap)
	inert := panelFixture(t)
	inert.Actions = 0
	r.set(inert)
	if !r.has(CardInertNote) {
		t.Fatalf("the foot does not say the mod takes no actions: %v", r.tt.Texts())
	}

	r.click(CardDone)
	if r.has(cardAnswerMark + "done") {
		t.Error("Done does something")
	}
	r.click(CardOpenLink)
	if !slices.Equal(r.opened, []string{"https://example.com/pull/41"}) {
		t.Errorf("the link does not open: %v", r.opened)
	}
	r.click(NextLabel("D7"))
	if r.pickedItem() != "D7" {
		t.Errorf("Next does not pick: %q", r.pickedItem())
	}
	r.click("one line per row")
	r.click(AnswerInChat)
	r.advance(board.UndoMS)
	r.wantQueued(nil, "an inert board")
	if got := r.status(); len(got) != 0 {
		t.Errorf("status lines %v", got)
	}
	if len(r.focused) != 0 {
		t.Errorf("Answer in chat focuses %v", r.focused)
	}
	if !r.has("one line per row") {
		t.Errorf("the options do not stay: %v", r.tt.Texts())
	}
}
