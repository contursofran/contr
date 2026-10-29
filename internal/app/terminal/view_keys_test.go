package terminal

import (
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// inputMsg is the message the view queues for events sent to pane.
func inputMsg(pane string, events ...endpoint.InputEvent) endpoint.ClientMsg {
	return endpoint.InputMsg{PaneID: pane, Events: events}
}

func keyEv(code endpoint.KeyCode, mods uint8) endpoint.InputEvent {
	return endpoint.KeyEvent{Code: code, Mods: mods}
}

func textEv(s string) endpoint.InputEvent { return endpoint.TextEvent{Text: s} }

// keysView is a settled view with an attached fake link and a surface that
// names paneID.
func keysView(t *testing.T) (*View, *ui.Tester, *fakeLink, *int) {
	t.Helper()
	v, tt, n := newTestView(t)
	settle(tt, 800, 600)
	return v, tt, attach(v, tt), n
}

func wantQueued(t *testing.T, f *fakeLink, want ...endpoint.ClientMsg) {
	t.Helper()
	if got := f.queued(); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
		t.Errorf("queued %v, want %v", got, want)
	}
}

func TestTheKeysTheTerminalTakesAreSentToTheFocusedPane(t *testing.T) {
	_, tt, f, _ := keysView(t)

	tt.Key(ui.Ctrl, ui.KeyC)
	wantQueued(t, f, inputMsg(paneID, keyEv(endpoint.CharKey('c'), endpoint.ModControl)))

	cases := []struct {
		name string
		mods ui.Modifiers
		key  ui.Key
		want endpoint.InputEvent
	}{
		{"enter", 0, ui.KeyEnter, keyEv(endpoint.KeyCode{Kind: endpoint.KeyEnter}, 0)},
		{"shift-enter", ui.Shift, ui.KeyEnter, keyEv(endpoint.KeyCode{Kind: endpoint.KeyEnter}, endpoint.ModShift)},
		{"escape", 0, ui.KeyEscape, keyEv(endpoint.KeyCode{Kind: endpoint.KeyEsc}, 0)},
		{"up", 0, ui.KeyUp, keyEv(endpoint.KeyCode{Kind: endpoint.KeyUp}, 0)},
		{"alt-backspace", ui.Alt, ui.KeyBackspace, keyEv(endpoint.KeyCode{Kind: endpoint.KeyBackspace}, endpoint.ModAlt)},
		{"shift-tab", ui.Shift, ui.KeyTab, keyEv(endpoint.KeyCode{Kind: endpoint.KeyBackTab}, endpoint.ModShift)},
	}
	for _, c := range cases {
		tt.Key(c.mods, c.key)
		got := f.queued()
		if want := []endpoint.ClientMsg{inputMsg(paneID, c.want)}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: queued %v, want %v", c.name, got, want)
		}
	}
}

func TestTypedTextIsSentAsTextAndOptionNeverMakesAKey(t *testing.T) {
	_, tt, f, _ := keysView(t)

	tt.Type("é")
	wantQueued(t, f, inputMsg(paneID, textEv("é")))

	// Option-E types "é" through the IME; the key itself sends nothing.
	tt.Key(ui.Alt, ui.KeyE)
	wantQueued(t, f)
	tt.TypeKey(ui.Alt, ui.KeyE, "´")
	wantQueued(t, f, inputMsg(paneID, textEv("´")))
}

func TestAKeyThatMakesNoEventIsTakenAndSendsNothing(t *testing.T) {
	v, tt, f, _ := keysView(t)
	if !v.onKey(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeySpace}) {
		t.Error("a plain Space was not taken")
	}
	if v.onKey(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyQ, Mods: ui.Super}) {
		t.Error("a Cmd chord was taken")
	}
	wantQueued(t, f)
	_ = tt
}

func TestCmdVPastesTheClipboardTextAsOnePaste(t *testing.T) {
	_, tt, f, _ := keysView(t)

	tt.Key(ui.Super, ui.KeyV)
	wantQueued(t, f) // an empty clipboard pastes nothing

	tt.SetClipboard("one\ntwo")
	tt.Key(ui.Super, ui.KeyV)
	wantQueued(t, f, inputMsg(paneID, endpoint.PasteEvent{Text: "one\ntwo"}))
}

func TestCmdShiftVAndCmdAltVAreNotAPaste(t *testing.T) {
	_, tt, f, _ := keysView(t)
	tt.SetClipboard("one")

	tt.Key(ui.Super|ui.Shift, ui.KeyV)
	tt.Key(ui.Super|ui.Alt, ui.KeyV)
	wantQueued(t, f) // only Cmd-V pastes

	tt.Key(ui.Super, ui.KeyV)
	wantQueued(t, f, inputMsg(paneID, endpoint.PasteEvent{Text: "one"}))
}

func TestAClipboardUpdateWritesTheClipboardOnce(t *testing.T) {
	v, tt, _ := newTestView(t)
	tt.Frame()
	tt.SetClipboard("kept")

	v.onUpdate(endpoint.ClipboardUpdate{Text: "one\ntwo"})
	if got := tt.Clipboard(); got != "one\ntwo" {
		t.Errorf("clipboard = %q", got)
	}
}

func TestAClipboardUpdateBeforeTheFirstFrameIsWrittenByTheFirstFrame(t *testing.T) {
	v := NewView(Options{})
	t.Cleanup(v.Close)
	v.onUpdate(endpoint.ClipboardUpdate{Text: "early"})
	if v.hasServices {
		t.Fatal("services before the first frame")
	}
	tt := ui.NewTester(v.Build, 800, 600)
	tt.Frame()
	if got := tt.Clipboard(); got != "early" {
		t.Errorf("clipboard = %q, want the early text", got)
	}
	tt.SetClipboard("later")
	tt.Frame()
	if got := tt.Clipboard(); got != "later" {
		t.Errorf("a later frame wrote the text again: %q", got)
	}
}

func TestThePasteActionPastesTheClipboardTextAsOnePaste(t *testing.T) {
	_, tt, f, _ := keysView(t)

	tt.Command("paste")
	wantQueued(t, f) // an empty clipboard pastes nothing

	tt.SetClipboard("one\ntwo")
	tt.Command("paste")
	wantQueued(t, f, inputMsg(paneID, endpoint.PasteEvent{Text: "one\ntwo"}))
}

func TestThePasteActionWithNoLinkSendsNothing(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	tt.SetClipboard("one")
	// No link is running, so there is no pane to paste into.
	tt.Command("paste")
	if v.link != nil {
		t.Error("a link started")
	}
}

func TestCopyAndSelectAllAreEnabledAndDoNothingWithoutASelection(t *testing.T) {
	_, tt, f, _ := keysView(t)
	tt.SetClipboard("kept")

	tt.Command("copy")
	tt.Command("selectAll")
	wantQueued(t, f)
	if got := tt.Clipboard(); got != "kept" {
		t.Errorf("clipboard = %q, want it kept", got)
	}
}

func TestCmdVKeyIsOnePasteAndCmdCAndCmdAKeysSendNothing(t *testing.T) {
	// The real Edit menu path is checked in the e2e test (M5.7e).
	_, tt, f, _ := keysView(t)
	tt.SetClipboard("one")

	tt.Key(ui.Super, ui.KeyV)
	wantQueued(t, f, inputMsg(paneID, endpoint.PasteEvent{Text: "one"}))

	tt.Key(ui.Super, ui.KeyC)
	tt.Key(ui.Super, ui.KeyA)
	wantQueued(t, f)
}

func TestComposedTextIsPaintedAndSentOnceWhenCommitted(t *testing.T) {
	v, tt, f, n := keysView(t)
	before := *n

	tt.Compose("ni", 2)
	if r, ok := v.ime.MarkedRange(); !ok || r != (ui.TextInputRange{Start: 0, End: 2}) {
		t.Errorf("marked range = %v, %v", r, ok)
	}
	if v.ime.marked != "ni" || *n == before {
		t.Errorf("marked = %q, invalidations %d -> %d", v.ime.marked, before, *n)
	}
	wantQueued(t, f) // composing sends nothing

	tt.Type("你")
	wantQueued(t, f, inputMsg(paneID, textEv("你")))
	if v.ime.marked != "" {
		t.Errorf("marked = %q after the commit", v.ime.marked)
	}
	if _, ok := v.ime.MarkedRange(); ok {
		t.Error("a marked range is left after the commit")
	}
}

func TestAnUnmarkedCompositionIsSentAsTextOnce(t *testing.T) {
	v, tt, f, _ := keysView(t)

	tt.Compose("ni", 2)
	v.ime.UnmarkText() // the system commits a composition this way
	if v.ime.marked != "" {
		t.Errorf("marked = %q", v.ime.marked)
	}
	wantQueued(t, f, inputMsg(paneID, textEv("ni")))

	v.ime.UnmarkText() // no composition is left
	wantQueued(t, f)
}

func TestAnEmptyMarkedTextEndsTheCompositionWithoutASend(t *testing.T) {
	v, tt, f, _ := keysView(t)
	tt.Compose("ni", 2)
	tt.Compose("", 0)
	if v.ime.marked != "" {
		t.Errorf("marked = %q", v.ime.marked)
	}
	wantQueued(t, f)
}

func TestTheImeWindowIsPlacedAtTheCursorCell(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	if _, _, ok := v.ime.BoundsForRange(ui.TextInputRange{}); ok {
		t.Fatal("bounds without a surface")
	}

	v.acceptSurface(surface(1, "a"))
	tt.Frame()
	grid, _ := tt.Find(LabelGrid)
	m := v.metrics
	r, _, ok := v.ime.BoundsForRange(ui.TextInputRange{})
	if !ok {
		t.Fatal("no bounds with a surface")
	}
	// The cursor is in column 1, row 0.
	want := ui.Rect{X: m.Pad + m.CellW, Y: m.Pad, W: m.CellW, H: m.CellH}
	if r != want {
		t.Errorf("bounds = %v, want %v", r, want)
	}
	caret, active := tt.TextCaret()
	if !active || caret.X != grid.X+want.X || caret.Y != grid.Y+want.Y {
		t.Errorf("caret = %v (%v), want the cell at %v in window coordinates", caret, active, want)
	}
}

func TestImeBoundsNeedACursor(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	v.acceptSurface(shownSurface(1, endpoint.GridSize{Cols: 3, Rows: 2}, paneID))
	if _, _, ok := v.ime.BoundsForRange(ui.TextInputRange{}); ok {
		t.Error("bounds for a surface with no cursor")
	}
}

func TestAKeyWithNoPaneSendsNothing(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := &fakeLink{}
	v.link = f
	// A surface that names no pane, and no snapshot yet: nowhere to send.
	v.acceptSurface(shownSurface(1, endpoint.GridSize{Cols: 3, Rows: 2}, ""))
	tt.Frame()

	tt.Key(ui.Ctrl, ui.KeyC)
	wantQueued(t, f)
}

func TestAKeyWithoutALinkIsDroppedAndTheViewKeepsRunning(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	v.acceptSurface(shownSurface(1, endpoint.GridSize{Cols: 3, Rows: 2}, paneID))
	tt.Frame()

	before := len(v.sends)
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Type("a")
	if len(v.sends) != before || v.link != nil {
		t.Errorf("sends %d -> %d, link %v", before, len(v.sends), v.link)
	}
}

func TestTheShellPaneTakesKeysWhenTheSurfaceNamesNone(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := &fakeLink{}
	v.link = f
	p9 := "p9"
	v.onUpdate(endpoint.SnapshotUpdate{Snapshot: &endpoint.ShellSnapshot{FocusedPaneID: &p9}})
	v.acceptSurface(shownSurface(1, v.sent.size, ""))
	tt.Frame()

	tt.Key(ui.Ctrl, ui.KeyC)
	wantQueued(t, f, inputMsg("p9", keyEv(endpoint.CharKey('c'), endpoint.ModControl)))
}

func TestInputThatIsNotQueuedDoesNotReSendTheSize(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	sent := v.sent

	// The link refuses each call, as one that lost its connection does.
	dead := &fakeLink{refuse: endpoint.ErrLinkNotConnected}
	v.link = dead
	// herdr shows another size: a re-send would be due if the input were queued.
	v.acceptSurface(shownSurface(1, endpoint.GridSize{Cols: 3, Rows: 2}, paneID))
	t0 := time.Now()
	v.send([]endpoint.InputEvent{textEv("a")}, t0)
	if !v.reassertedAt.IsZero() {
		t.Error("nothing was queued, but the size was marked as sent again")
	}

	// The link works now. The failed input did not re-send the size, so the next one does.
	f := &fakeLink{}
	v.link = f
	v.send([]endpoint.InputEvent{textEv("b")}, t0)
	wantQueued(t, f,
		inputMsg(paneID, textEv("b")),
		endpoint.ResizeMsg{Size: sent.size, Cell: sent.cell})
}

func TestALostLinkForgetsTheShellPane(t *testing.T) {
	v, _, _ := newTestView(t)
	p9 := "p9"
	v.onUpdate(endpoint.SnapshotUpdate{Snapshot: &endpoint.ShellSnapshot{FocusedPaneID: &p9}})
	if v.shellPane != "p9" {
		t.Fatalf("shellPane = %q", v.shellPane)
	}
	v.onUpdate(endpoint.StateUpdate{State: stateNotRunning})
	if v.shellPane != "" {
		t.Errorf("shellPane = %q after the link was lost", v.shellPane)
	}
}

func TestTypingSendsTheSizeAgainOnlyWhenHerdrShowsAnotherSize(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := &fakeLink{}
	v.link = f
	sent := v.sent
	resize := endpoint.ResizeMsg{Size: sent.size, Cell: sent.cell}
	send := func(s string, at time.Time) { v.send([]endpoint.InputEvent{textEv(s)}, at) }

	// herdr shows the size contr sent: typing sends the text only.
	v.acceptSurface(shownSurface(1, sent.size, paneID))
	t0 := time.Now()
	send("a", t0)
	wantQueued(t, f, inputMsg(paneID, textEv("a")))

	// herdr shows another size: the first key sends it again, and a key within
	// a second does not.
	v.acceptSurface(shownSurface(2, endpoint.GridSize{Cols: 3, Rows: 2}, paneID))
	send("b", t0.Add(500*time.Millisecond))
	wantQueued(t, f, inputMsg(paneID, textEv("b")), resize)
	send("c", t0.Add(900*time.Millisecond))
	wantQueued(t, f, inputMsg(paneID, textEv("c")))

	// A second later it is sent again.
	send("d", t0.Add(2*time.Second))
	wantQueued(t, f, inputMsg(paneID, textEv("d")), resize)
}
