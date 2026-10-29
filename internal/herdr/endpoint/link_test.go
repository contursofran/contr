// Tests of the surface link, ported from the Rust link tests. They use fake
// servers on sockets in temporary folders and never touch herdr.
// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	linkWait = 2 * time.Second
	// settle is how long a test waits to see that nothing else comes.
	settle = 300 * time.Millisecond
)

var (
	linkSize = GridSize{Cols: 60, Rows: 12}
	linkCell = CellPx{Width: 8, Height: 16}
)

// serveEach serves count connections one after the other. script runs for
// the n-th. A script that must keep the connection open waits on quit, which
// the cleanup closes. The cleanup closes the listener and every connection
// and waits for the goroutine, so none leaks.
func serveEach(t *testing.T, count int, script func(c net.Conn, n int, quit <-chan struct{})) string {
	t.Helper()
	path := filepath.Join(shortTempDir(t), "herdr-client.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	quit := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		defer close(done)
		for n := range count {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			script(c, n, quit)
			_ = c.Close()
		}
	}()
	t.Cleanup(func() {
		close(quit)
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		<-done
	})
	return path
}

// recorder keeps the updates of a link in order.
type recorder struct{ ch chan LinkUpdate }

func newRecorder() *recorder { return &recorder{ch: make(chan LinkUpdate, 1024)} }

func (r *recorder) notify(u LinkUpdate) { r.ch <- u }

// startLink starts a link on a socket path. The cleanup stops it and checks
// that its goroutines end.
func startLink(t *testing.T, path string) (*Link, Handle, *recorder) {
	t.Helper()
	rec := newRecorder()
	link, handle := SpawnLink(LinkTarget{Socket: path}, linkSize, linkCell, rec.notify)
	t.Cleanup(func() {
		link.Stop()
		select {
		case <-link.Done():
		case <-time.After(6 * time.Second):
			t.Error("the link goroutines did not end after Stop")
		}
	})
	return link, handle, rec
}

// next returns the next update. It fails after linkWait.
func (r *recorder) next(t *testing.T) LinkUpdate {
	t.Helper()
	select {
	case u := <-r.ch:
		return u
	case <-time.After(linkWait):
		t.Fatalf("no update within %v", linkWait)
		return nil
	}
}

func (r *recorder) nextState(t *testing.T) LinkState {
	t.Helper()
	switch u := r.next(t).(type) {
	case StateUpdate:
		return u.State
	default:
		t.Fatalf("expected a state, got %#v", u)
		return LinkState{}
	}
}

func (r *recorder) nextSurface(t *testing.T) *Surface {
	t.Helper()
	switch u := r.next(t).(type) {
	case SurfaceUpdate:
		return u.Surface
	default:
		t.Fatalf("expected a surface, got %#v", u)
		return nil
	}
}

// empty reports whether no update is queued.
func (r *recorder) empty() bool {
	select {
	case <-r.ch:
		return false
	default:
		return true
	}
}

func liveState(version string, untested bool) LinkState {
	return LinkState{Kind: LinkLive, ServerVersion: version, Untested: untested}
}

func putLinkCell(e *Enc, text string) {
	e.Str(text)
	e.Vu(0) // fg
	e.Vu(0) // bg
	e.Vu(0) // modifier
	e.Bool(false)
	e.None() // hyperlink
}

// surfaceFrame is one PaneSurface frame of cols × rows cells, each showing
// fill. Boot "b1", projection 7. Fields after the panes are left out: the
// decoder does not read them.
func surfaceFrame(revision uint64, cols, rows uint16, fill string) []byte {
	var e Enc
	e.Vu(SSurface)
	e.Str("b1")
	e.Vu(7)
	e.Vu(revision)
	cells := int(cols) * int(rows)
	e.Vu(uint64(cells))
	for range cells {
		putLinkCell(&e, fill)
	}
	e.Vu(uint64(cols))
	e.Vu(uint64(rows))
	e.None() // cursor
	e.Vu(0)  // hyperlinks
	e.Vu(0)  // graphics
	e.Vu(0)  // panes
	return e.Buf
}

// patchFrame is a PaneSurfacePatch from base to revision that sets cell
// (0, 0) to text.
func patchFrame(base, revision uint64, text string) []byte {
	var e Enc
	e.Vu(SPatch)
	e.Str("b1")
	e.Vu(7)
	e.Vu(base)
	e.Vu(revision)
	e.Vu(1) // spans
	e.Vu(0) // x
	e.Vu(0) // y
	e.Vu(1) // cells in the span
	putLinkCell(&e, text)
	e.Vu(0)  // panes
	e.None() // cursor
	return e.Buf
}

// clipboardFrame is a Clipboard frame whose data is the base64 text data.
func clipboardFrame(data string) []byte {
	var e Enc
	e.Vu(SClipboard)
	e.Str(data)
	return e.Buf
}

func shutdownFrame(reason *string) []byte {
	var e Enc
	e.Vu(SShutdown)
	if reason == nil {
		e.None()
	} else {
		e.Some()
		e.Str(*reason)
	}
	return e.Buf
}

// helloBody is the JSON body of a client hello payload.
func helloBody(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	d := NewDec(payload)
	if tag, err := d.Vu(); err != nil || tag != CControl {
		t.Fatalf("hello tag = %d, %v", tag, err)
	}
	if kind, err := d.Str(); err != nil || kind != HelloKind {
		t.Fatalf("hello kind = %q, %v", kind, err)
	}
	body, err := d.Str()
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func missingSocket() string { return "/nonexistent/herdr-client.sock" }

func TestRetryWaitsDoubleFromHalfASecondAndStopAtFive(t *testing.T) {
	for _, c := range []struct {
		failures uint
		want     time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, time.Second},
		{3, 4 * time.Second},
		{4, 5 * time.Second},
		{40, 5 * time.Second},
		{^uint(0), 5 * time.Second},
	} {
		if got := retryDelay(c.failures); got != c.want {
			t.Errorf("retryDelay(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

func TestResolveUsesTheGivenSocketOrTheEnvironment(t *testing.T) {
	// A given socket is used as it is.
	if got, ok := (LinkTarget{Socket: "/s/given.sock"}).path(func(string) string { return "" }); !ok || got != "/s/given.sock" {
		t.Errorf("given socket = %q, %v", got, ok)
	}
	// Local needs HOME or one of the socket variables. Without them, no path.
	if got, ok := (LinkTarget{}).path(func(string) string { return "" }); ok {
		t.Errorf("local without environment = %q, true; want none", got)
	}
	fromEnv := func(key string) string {
		if key == "HERDR_SOCKET_PATH" {
			return "/s/a.sock"
		}
		return ""
	}
	if got, ok := (LinkTarget{}).path(fromEnv); !ok || got != "/s/a-client.sock" {
		t.Errorf("local from environment = %q, %v", got, ok)
	}
}

func TestTakeLatestIsNilBeforeAnySurface(t *testing.T) {
	_, handle, _ := startLink(t, missingSocket())
	if handle.TakeLatest() != nil {
		t.Error("TakeLatest returned a surface before any came")
	}
}

func TestAMissingSocketIsConnectingThenNotRunning(t *testing.T) {
	_, _, rec := startLink(t, missingSocket())
	if got := rec.nextState(t); got.Kind != LinkConnecting {
		t.Fatalf("first state = %#v, want connecting", got)
	}
	got := rec.nextState(t)
	if got.Kind != LinkNotRunning || !strings.Contains(got.Message, "not running") {
		t.Errorf("second state = %#v, want not running", got)
	}
}

func TestARepeatedFailureIsNotSentAgain(t *testing.T) {
	_, _, rec := startLink(t, missingSocket())
	rec.nextState(t)
	if got := rec.nextState(t); got.Kind != LinkNotRunning {
		t.Fatalf("second state = %#v", got)
	}
	// The next attempt, 500 ms later, fails the same way: no new update.
	time.Sleep(700 * time.Millisecond)
	if !rec.empty() {
		t.Error("a repeated failure sent another update")
	}
}

func TestASocketPathTooLongIsNotRunningWithTheIoReason(t *testing.T) {
	_, _, rec := startLink(t, "/tmp/"+strings.Repeat("a", 120))
	rec.nextState(t)
	got := rec.nextState(t)
	if got.Kind != LinkNotRunning || !strings.Contains(got.Message, "endpoint socket") {
		t.Errorf("state = %#v, want not running with the io reason", got)
	}
}

func TestInputAndResizeBeforeAConnectionAreNotConnected(t *testing.T) {
	_, handle, _ := startLink(t, missingSocket())
	events := []InputEvent{TextEvent{Text: "a"}}
	if err := handle.SendInput("w1:p1", events); !errors.Is(err, ErrLinkNotConnected) {
		t.Errorf("SendInput = %v", err)
	}
	if err := handle.SetFocus(true); !errors.Is(err, ErrLinkNotConnected) {
		t.Errorf("SetFocus = %v", err)
	}
	if err := handle.Resize(GridSize{Cols: 40, Rows: 8}, CellPx{Width: 9, Height: 18}); !errors.Is(err, ErrLinkNotConnected) {
		t.Errorf("Resize = %v", err)
	}
}

func TestTheLinkPublishesLiveThenASnapshotAndASurface(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		snapshot := `{"boot_id":"b1","revision":1,"focused_pane_id":"w1:p1"}`
		put(t, c, control(SnapshotKind, snapshot))
		put(t, c, surfaceFrame(1, 60, 12, "a"))
		<-quit
	})
	_, handle, rec := startLink(t, path)
	if got := rec.nextState(t); got.Kind != LinkConnecting {
		t.Fatalf("first state = %#v", got)
	}
	if got := rec.nextState(t); got != liveState("0.9.3", false) {
		t.Fatalf("second state = %#v", got)
	}
	s, ok := rec.next(t).(SnapshotUpdate)
	if !ok || s.Snapshot.FocusedPaneID == nil || *s.Snapshot.FocusedPaneID != "w1:p1" {
		t.Fatalf("snapshot update = %#v", s)
	}
	rec.nextSurface(t)
	surface := handle.TakeLatest()
	if surface == nil {
		t.Fatal("no surface to take")
	}
	if surface.Revision != 1 || surface.Size != linkSize || surface.Rows[0][0].Text != "a" {
		t.Errorf("surface = rev %d, size %v, cell %q", surface.Revision, surface.Size, surface.Rows[0][0].Text)
	}
}

func TestAClipboardWriteReachesTheUIAsOneUpdate(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		put(t, c, clipboardFrame("aGk="))
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	if got := rec.nextState(t); got != liveState("0.9.3", false) {
		t.Fatalf("state = %#v", got)
	}
	if got, ok := rec.next(t).(ClipboardUpdate); !ok || got.Text != "hi" {
		t.Fatalf("clipboard update = %#v", got)
	}
	time.Sleep(settle)
	if !rec.empty() {
		t.Error("one write sent more than one update")
	}
}

func TestAClipboardPayloadThatIsDroppedKeepsTheConnectionUp(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		// Not base64: dropped without an update or a state change.
		put(t, c, clipboardFrame("!!"))
		put(t, c, clipboardFrame("aGk="))
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	// Had the link ended, a state other than live would come next.
	if got := rec.nextState(t); got != liveState("0.9.3", false) {
		t.Fatalf("state = %#v", got)
	}
	if got, ok := rec.next(t).(ClipboardUpdate); !ok || got.Text != "hi" {
		t.Fatalf("clipboard update = %#v", got)
	}
}

func TestAVersionContrHasNotTestedIsLiveWithTheFlagSet(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.9"))
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	if got := rec.nextState(t); got != liveState("0.9.9", true) {
		t.Errorf("state = %#v", got)
	}
}

func TestAPatchIsAppliedAndTheNextTakeHasTheNewRevision(t *testing.T) {
	goOn := make(chan struct{})
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		put(t, c, surfaceFrame(1, 60, 12, "a"))
		select {
		case <-goOn:
		case <-quit:
			return
		}
		put(t, c, patchFrame(1, 2, "Z"))
		<-quit
	})
	_, handle, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	rec.nextSurface(t)
	first := handle.TakeLatest()
	if first == nil || first.Revision != 1 {
		t.Fatalf("first take = %#v", first)
	}
	close(goOn)
	rec.nextSurface(t)
	patched := handle.TakeLatest()
	if patched == nil || patched.Revision != 2 {
		t.Fatalf("second take = %#v", patched)
	}
	if patched.Rows[0][0].Text != "Z" || patched.Rows[0][1].Text != "a" {
		t.Errorf("row 0 = %q %q, want Z a", patched.Rows[0][0].Text, patched.Rows[0][1].Text)
	}
	// The surface taken first is a copy: the patch did not change it.
	if first.Rows[0][0].Text != "a" {
		t.Errorf("the first surface changed to %q", first.Rows[0][0].Text)
	}
}

func TestTwoSurfacesBeforeATakeQueueOneNoticeAndTakeTheNewest(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		put(t, c, surfaceFrame(1, 60, 12, "a"))
		put(t, c, surfaceFrame(2, 60, 12, "b"))
		<-quit
	})
	_, handle, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	rec.nextSurface(t)
	// Let the link read the second surface too: it must not queue a second notice.
	time.Sleep(settle)
	if !rec.empty() {
		t.Error("a second surface queued a second notice")
	}
	if got := handle.TakeLatest(); got == nil || got.Revision != 2 {
		t.Errorf("take = %#v, want revision 2", got)
	}
	if handle.TakeLatest() != nil {
		t.Error("a second take returned a surface")
	}
}

func TestAStalePatchReconnectsForAFullSurface(t *testing.T) {
	hello := make(chan struct{}, 1)
	path := serveEach(t, 2, func(c net.Conn, n int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		if n == 0 {
			put(t, c, surfaceFrame(1, 60, 12, "a"))
			// Based on revision 99, which the link does not hold: out of sync.
			put(t, c, patchFrame(99, 100, "X"))
			return
		}
		hello <- struct{}{}
		put(t, c, surfaceFrame(5, 60, 12, "b"))
		<-quit
	})
	_, handle, rec := startLink(t, path)
	if got := rec.nextState(t); got.Kind != LinkConnecting {
		t.Fatalf("state = %#v", got)
	}
	if got := rec.nextState(t); got != liveState("0.9.3", false) {
		t.Fatalf("state = %#v", got)
	}
	rec.nextSurface(t)
	handle.TakeLatest()
	if got := rec.nextState(t); got.Kind != LinkConnecting {
		t.Fatalf("state after the stale patch = %#v", got)
	}
	if got := rec.nextState(t); got != liveState("0.9.3", false) {
		t.Fatalf("state after the reconnect = %#v", got)
	}
	select {
	case <-hello:
	case <-time.After(linkWait):
		t.Fatal("no second hello")
	}
	rec.nextSurface(t)
	if got := handle.TakeLatest(); got == nil || got.Revision != 5 {
		t.Errorf("take = %#v, want revision 5", got)
	}
}

func TestShutdownWithoutAReasonIsNotRunningHerdrStopped(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		put(t, c, shutdownFrame(nil))
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	want := LinkState{Kind: LinkNotRunning, Message: "herdr stopped"}
	if got := rec.nextState(t); got != want {
		t.Errorf("state = %#v, want %#v", got, want)
	}
}

func TestShutdownWithAReasonPassesTheReasonOn(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		reason := "server stop"
		put(t, c, shutdownFrame(&reason))
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	want := LinkState{Kind: LinkNotRunning, Message: "server stop"}
	if got := rec.nextState(t); got != want {
		t.Errorf("state = %#v, want %#v", got, want)
	}
}

func TestEndOfFileAfterLiveIsNotRunning(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, _ <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	want := LinkState{Kind: LinkNotRunning, Message: "herdr closed the endpoint"}
	if got := rec.nextState(t); got != want {
		t.Errorf("state = %#v, want %#v", got, want)
	}
}

func TestAHerdrThatAcceptsAndClosesAtOnceIsRetriedWithBackoff(t *testing.T) {
	var accepts atomic.Int32
	path := serveEach(t, 8, func(c net.Conn, _ int, _ <-chan struct{}) {
		accepts.Add(1)
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		// The connection closes on return: herdr closed it at once.
	})
	startLink(t, path)
	time.Sleep(1200 * time.Millisecond)
	// Attempts at about 0 s, 0.5 s and 1.5 s (waits of 500 ms, then 1 s). A
	// fixed 500 ms wait would make three attempts by 1.2 s.
	if n := accepts.Load(); n < 1 || n > 2 {
		t.Errorf("%d attempts in 1.2 s, want 1 or 2", n)
	}
}

func TestARefusedWelcomeIsIncompatibleWithHerdrsReason(t *testing.T) {
	body := welcomeBody(1, "0.9.3", `,"error":{"code":"invalid_hello","message":"bad hello"}`)
	path := serveEach(t, 1, func(c net.Conn, _ int, _ <-chan struct{}) {
		readHello(t, c)
		put(t, c, control(WelcomeKind, body))
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	got := rec.nextState(t)
	if got.Kind != LinkIncompatible || !strings.Contains(got.Message, "invalid_hello") || !strings.Contains(got.Message, "bad hello") {
		t.Errorf("state = %#v", got)
	}
}

func TestAnUndecodableFrameAfterLiveIsIncompatibleWithTheVersion(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		put(t, c, []byte{0xFF}) // a varint marker that does not exist
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	want := LinkState{Kind: LinkIncompatible, Message: "herdr 0.9.3 sent a surface contr cannot read · update contr"}
	if got := rec.nextState(t); got != want {
		t.Errorf("state = %#v, want %#v", got, want)
	}
}

func TestAnOversizedFrameHeaderAfterLiveIsIncompatibleToo(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		header := binary.LittleEndian.AppendUint32(nil, MaxFrame+1)
		if _, err := c.Write(header); err != nil {
			t.Errorf("write header: %v", err)
		}
		<-quit
	})
	_, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	want := LinkState{Kind: LinkIncompatible, Message: "herdr 0.9.3 sent a surface contr cannot read · update contr"}
	if got := rec.nextState(t); got != want {
		t.Errorf("state = %#v, want %#v", got, want)
	}
}

func TestInputAndFocusReachTheServerInOrder(t *testing.T) {
	sent := make(chan []byte, 2)
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		for range 2 {
			frame, err := ReadFrame(c)
			if err != nil {
				t.Errorf("read frame: %v", err)
				return
			}
			sent <- frame
		}
		<-quit
	})
	_, handle, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	events := []InputEvent{TextEvent{Text: "ls"}}
	if err := handle.SendInput("w1:p1", events); err != nil {
		t.Fatal(err)
	}
	if err := handle.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	recv := func() []byte {
		select {
		case f := <-sent:
			return f
		case <-time.After(linkWait):
			t.Fatal("the server got no frame")
			return nil
		}
	}
	input := recv()
	if tag, _ := NewDec(input).Vu(); tag != CPaneInput {
		t.Errorf("first frame tag = %d, want %d", tag, CPaneInput)
	}
	if want := (InputMsg{PaneID: "w1:p1", Events: events}).Encode(); string(input) != string(want) {
		t.Errorf("input frame = %x, want %x", input, want)
	}
	focus := recv()
	if string(focus) != string(FocusMsg{Focused: true}.Encode()) {
		t.Errorf("focus frame = %x", focus)
	}
	if tag, _ := NewDec(focus).Vu(); tag != CFocus {
		t.Errorf("second frame tag = %d, want %d", tag, CFocus)
	}
}

func TestAFullOutboxRefusesInputWithFull(t *testing.T) {
	path := serveEach(t, 1, func(c net.Conn, _ int, quit <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		// Never read: the writer blocks once the socket buffer is full.
		<-quit
	})
	_, handle, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	events := []InputEvent{PasteEvent{Text: strings.Repeat("x", 32*1024)}}
	var refused error
	for range 1000 {
		if err := handle.SendInput("w1:p1", events); err != nil {
			refused = err
			break
		}
	}
	if !errors.Is(refused, ErrLinkFull) {
		t.Errorf("refusal = %v, want ErrLinkFull", refused)
	}
}

func TestAResizeIsStoredForTheNextHello(t *testing.T) {
	hellos := make(chan map[string]any, 1)
	path := serveEach(t, 2, func(c net.Conn, n int, quit <-chan struct{}) {
		hello, err := ReadFrame(c)
		if err != nil {
			t.Errorf("read hello: %v", err)
			return
		}
		body := helloBody(t, hello)
		if n == 0 {
			if size := body["surface_size"].(map[string]any); size["cols"] != float64(60) {
				t.Errorf("first hello cols = %v, want 60", size["cols"])
			}
			put(t, c, welcome("0.9.3"))
			// The resize is the first frame after the hello. Then close.
			if _, err := ReadFrame(c); err != nil {
				t.Errorf("read resize: %v", err)
			}
			return
		}
		hellos <- body
		put(t, c, welcome("0.9.3"))
		<-quit
	})
	_, handle, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	if err := handle.Resize(GridSize{Cols: 40, Rows: 8}, linkCell); err != nil {
		t.Fatal(err)
	}
	// The first connection closes, and the link reconnects with the new size.
	want := LinkState{Kind: LinkNotRunning, Message: "herdr closed the endpoint"}
	if got := rec.nextState(t); got != want {
		t.Fatalf("state = %#v, want %#v", got, want)
	}
	var body map[string]any
	select {
	case body = <-hellos:
	case <-time.After(linkWait):
		t.Fatal("no second hello")
	}
	size := body["surface_size"].(map[string]any)
	if size["cols"] != float64(40) || size["rows"] != float64(8) {
		t.Errorf("surface_size = %v, want 40 x 8", size)
	}
	if body["cell_width_px"] != float64(8) || body["cell_height_px"] != float64(16) {
		t.Errorf("cell = %v x %v, want 8 x 16", body["cell_width_px"], body["cell_height_px"])
	}
}

func TestStoppingTheLinkSendsADetach(t *testing.T) {
	detach := make(chan []byte, 1)
	path := serveEach(t, 1, func(c net.Conn, _ int, _ <-chan struct{}) {
		readHello(t, c)
		put(t, c, welcome("0.9.3"))
		frame, err := ReadFrame(c)
		if err != nil {
			t.Errorf("read frame: %v", err)
			return
		}
		detach <- frame
	})
	link, _, rec := startLink(t, path)
	rec.nextState(t)
	rec.nextState(t)
	link.Stop()
	select {
	case frame := <-detach:
		if string(frame) != string(DetachMsg{}.Encode()) {
			t.Errorf("frame = %x, want a detach", frame)
		}
		if tag, _ := NewDec(frame).Vu(); tag != CDetach {
			t.Errorf("tag = %d, want %d", tag, CDetach)
		}
	case <-time.After(linkWait):
		t.Fatal("the server got no detach")
	}
	select {
	case <-link.Done():
	case <-time.After(linkWait):
		t.Error("the goroutines did not end after Stop")
	}
}

func TestStoppingDuringABackoffEndsTheGoroutines(t *testing.T) {
	link, _, rec := startLink(t, missingSocket())
	rec.nextState(t)
	rec.nextState(t)
	link.Stop()
	link.Stop() // twice is safe
	select {
	case <-link.Done():
	case <-time.After(linkWait):
		t.Fatal("the goroutines did not end after Stop")
	}
	if err := (Handle{link.s}).SetFocus(true); !errors.Is(err, ErrLinkNotConnected) {
		t.Errorf("SetFocus after Stop = %v", err)
	}
}
