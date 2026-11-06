package ghostty

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

type fakeStream struct {
	events chan hc.StreamEvent
	once   sync.Once

	mu      sync.Mutex
	err     error
	written []byte
	resizes [][2]uint16
	focus   []bool
}

func newFakeStream() *fakeStream { return &fakeStream{events: make(chan hc.StreamEvent, 64)} }

func (s *fakeStream) Events() <-chan hc.StreamEvent { return s.events }
func (s *fakeStream) Err() error                    { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *fakeStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, p...)
	return len(p), nil
}
func (s *fakeStream) Resize(c, r uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizes = append(s.resizes, [2]uint16{c, r})
	return nil
}
func (s *fakeStream) Focus(f bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focus = append(s.focus, f)
	return nil
}
func (s *fakeStream) end(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.events)
	})
}
func (s *fakeStream) Close() error  { s.end(nil); return nil }
func (s *fakeStream) data(t string) { s.events <- hc.StreamEvent{Data: []byte(t)} }
func (s *fakeStream) wrote() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.written)
}

type fakeSource struct {
	mu      sync.Mutex
	streams []*fakeStream
	params  []hc.AttachParams
	errs    []error // one per attach call, nil for success
	watchCh chan hc.Event
	closed  int
	gate    chan struct{} // attaches after the first wait for this to close, when set
	gate0   chan struct{} // every attach waits for this to close, when set
}

func newFakeSource(errs ...error) *fakeSource {
	return &fakeSource{errs: errs, watchCh: make(chan hc.Event, 8)}
}

func (f *fakeSource) Attach(ctx context.Context, p hc.AttachParams) (Stream, error) {
	f.mu.Lock()
	gate, n := f.gate, len(f.params)
	if f.gate0 != nil {
		gate, n = f.gate0, 1
	}
	f.mu.Unlock()
	if gate != nil && n >= 1 {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	i := len(f.params)
	f.params = append(f.params, p)
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	s := newFakeStream()
	f.streams = append(f.streams, s)
	return s, nil
}

func (f *fakeSource) Watch(context.Context) (<-chan hc.Event, func(), error) {
	return f.watchCh, func() {}, nil
}

func (f *fakeSource) Close() error { f.mu.Lock(); f.closed++; f.mu.Unlock(); return nil }

func (f *fakeSource) attachCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.params) }
func (f *fakeSource) param(i int) hc.AttachParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.params[i]
}

func (f *fakeSource) stream(t *testing.T, i int) *fakeStream {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		if i < len(f.streams) {
			s := f.streams[i]
			f.mu.Unlock()
			return s
		}
		f.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("stream %d was not attached", i)
	return nil
}

func waitAttaches(t *testing.T, src *fakeSource, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for src.attachCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("attaches = %d, want %d", src.attachCount(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// states collects the state changes a Conn reports.
type states struct{ ch chan State }

func (s *states) on(st State) { s.ch <- st }

func (s *states) wait(t *testing.T, kind StateKind) State {
	t.Helper()
	for {
		select {
		case st := <-s.ch:
			if st.Kind == kind {
				return st
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no state of kind %d", kind)
		}
	}
}

// fast shortens the waits of every test: the first attach is at once and the
// backoff is a few milliseconds.
func fast(t *testing.T) {
	t.Helper()
	b, m, f := baseRetry, maxRetry, firstWait
	baseRetry, maxRetry, firstWait = 5*time.Millisecond, 20*time.Millisecond, 0
	t.Cleanup(func() { baseRetry, maxRetry, firstWait = b, m, f })
}

func start(t *testing.T, src *fakeSource) (*Conn, *states) {
	t.Helper()
	fast(t)
	st := &states{ch: make(chan State, 256)}
	c := NewConn(src, Options{OnState: st.on})
	t.Cleanup(func() { _ = c.Close() })
	return c, st
}

func readN(t *testing.T, c *Conn, n int) string {
	t.Helper()
	got := make([]byte, 0, n)
	buf := make([]byte, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for len(got) < n {
			k, err := c.Read(buf)
			if err != nil {
				return
			}
			got = append(got, buf[:k]...)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("read %q, want %d bytes", got, n)
	}
	return string(got)
}

func TestReadGivesTheOutputInPieces(t *testing.T) {
	src := newFakeSource()
	c, _ := start(t, src)
	s := src.stream(t, 0)
	s.data("hello ")
	s.data("world")
	if got := readN(t, c, 11); got != "hello world" {
		t.Errorf("read %q", got)
	}
}

func TestFirstAttachAsksForHistoryAndTheViewsSize(t *testing.T) {
	fast(t)
	firstWait = time.Hour // the size comes first
	src := newFakeSource()
	st := &states{ch: make(chan State, 16)}
	c := NewConn(src, Options{OnState: st.on})
	defer c.Close()
	if err := c.ResizePixels(100, 30, 8, 17); err != nil {
		t.Fatal(err)
	}
	st.wait(t, Live)
	p := src.param(0)
	if p.Cols != 100 || p.Rows != 30 || p.History != HistoryLines || p.Resumed || p.Observe {
		t.Errorf("attach params %+v", p)
	}
	if p.Display == nil || p.Display.Cell == nil || *p.Display.Cell != [2]int{8, 17} {
		t.Errorf("display %+v, want the cell 8x17", p.Display)
	}
}

func TestAttachWithoutASizeUsesTheDefault(t *testing.T) {
	src := newFakeSource()
	_, st := start(t, src)
	st.wait(t, Live)
	if p := src.param(0); p.Cols != 80 || p.Rows != 24 {
		t.Errorf("attach size %dx%d, want 80x24", p.Cols, p.Rows)
	}
}

func TestLostConnectionReattachesResumedAndReadGoesOn(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s0 := src.stream(t, 0)
	s0.data("one")
	if got := readN(t, c, 3); got != "one" {
		t.Fatalf("read %q", got)
	}
	s0.end(errors.New("connection lost"))
	st.wait(t, NotRunning)
	waitAttaches(t, src, 2)
	st.wait(t, Live)
	if !src.param(1).Resumed {
		t.Error("the second attach should be resumed")
	}
	src.stream(t, 1).data("two")
	if got := readN(t, c, 3); got != "two" {
		t.Errorf("read after the reattach %q", got)
	}
}

func TestAttachFailureBacksOff(t *testing.T) {
	src := newFakeSource(&hc.Error{Kind: "unavailable"}, &hc.Error{Kind: "unavailable"})
	_, st := start(t, src)
	s := st.wait(t, NotRunning)
	if s.Message == "" {
		t.Error("the state has no message")
	}
	waitAttaches(t, src, 3)
	st.wait(t, Live)
}

func TestFinalFailuresDoNotRetry(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want StateKind
	}{{"finished", NotRunning}, {"not_found", NotRunning}, {hc.KindIncompatible, Incompatible}, {"needs_framed", Incompatible}} {
		t.Run(tc.kind, func(t *testing.T) {
			src := newFakeSource(&hc.Error{Kind: tc.kind, Message: "no"})
			c, st := start(t, src)
			if got := st.wait(t, tc.want); got.Message == "" {
				t.Error("the state has no message")
			}
			time.Sleep(60 * time.Millisecond)
			if n := src.attachCount(); n != 1 {
				t.Errorf("attaches = %d, want 1", n)
			}
			if _, err := c.Write([]byte("x")); !errors.Is(err, ErrNotRunning) {
				t.Errorf("Write error %v, want ErrNotRunning", err)
			}
		})
	}
}

func TestProgramEndIsFinalUntilRestarted(t *testing.T) {
	src := newFakeSource()
	_, st := start(t, src)
	src.stream(t, 0).end(&hc.StreamEnd{Reason: "exited"})
	got := st.wait(t, NotRunning)
	if got.Message == "" {
		t.Error("no message")
	}
	time.Sleep(60 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Fatalf("attaches = %d after the program ended, want 1", n)
	}
	src.watchCh <- hc.Event{Method: "event", Type: "restarted"}
	waitAttaches(t, src, 2)
	st.wait(t, Live)
	if !src.param(1).Resumed {
		t.Error("the attach after a restart should be resumed")
	}
}

func TestInputIsHeldWhileItReconnectsAndSentInOrderFirst(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s0 := src.stream(t, 0)
	st.wait(t, Live)
	if _, err := c.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	src.gate = make(chan struct{}) // the next attach waits
	s0.end(errors.New("connection lost"))
	st.wait(t, NotRunning)
	for _, k := range []string{"b", "c"} {
		if n, err := c.Write([]byte(k)); err != nil || n != 1 {
			t.Fatalf("Write while down = %d, %v", n, err)
		}
	}
	if s0.wrote() != "a" {
		t.Errorf("the old stream got %q, want a", s0.wrote())
	}
	close(src.gate)
	s1 := src.stream(t, 1)
	st.wait(t, Live)
	if _, err := c.Write([]byte("d")); err != nil {
		t.Fatal(err)
	}
	if got := s1.wrote(); got != "bcd" {
		t.Errorf("the new stream got %q, want bcd", got)
	}
}

func TestInputBeforeTheFirstAttachIsHeld(t *testing.T) {
	fast(t)
	firstWait = time.Hour
	src := newFakeSource()
	src.gate = make(chan struct{})
	c := NewConn(src, Options{})
	defer c.Close()
	if _, err := c.Write([]byte("ls")); err != nil { // starts the attach, as a size does
		t.Fatal(err)
	}
	if got := src.stream(t, 0).wrote(); got != "ls" {
		t.Errorf("the stream got %q, want ls", got)
	}
}

func TestHeldInputIsDroppedWhenTheAttachFailsForGood(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s0 := src.stream(t, 0)
	st.wait(t, Live)
	src.gate = make(chan struct{})
	s0.end(errors.New("connection lost"))
	st.wait(t, NotRunning)
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	src.errs = append(src.errs, nil, &hc.Error{Kind: "finished"})
	close(src.gate)
	st.wait(t, NotRunning)
	src.mu.Lock()
	src.errs = nil
	src.mu.Unlock()
	src.watchCh <- hc.Event{Method: "event", Type: "restarted"}
	s := src.stream(t, len(src.streams))
	st.wait(t, Live)
	if got := s.wrote(); got != "" {
		t.Errorf("the next program got the old input %q", got)
	}
}

func TestHeldInputIsLimited(t *testing.T) {
	src := newFakeSource()
	src.gate = make(chan struct{})
	c, st := start(t, src)
	st.wait(t, Live)
	src.stream(t, 0).end(errors.New("lost"))
	st.wait(t, NotRunning)
	if _, err := c.Write(make([]byte, maxHeld)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Write over the limit: %v, want ErrNotRunning", err)
	}
	close(src.gate)
}

func TestResizeIsSentWhenItChangesAndAfterAnAttach(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	_ = c.ResizePixels(90, 20, 8, 17)
	_ = c.ResizePixels(90, 20, 8, 17) // the same: nothing
	_ = c.ResizePixels(0, 20, 8, 17)  // no size: nothing
	s.mu.Lock()
	got := s.resizes
	s.mu.Unlock()
	if len(got) != 1 || got[0] != [2]uint16{90, 20} {
		t.Errorf("resizes %v, want one of 90x20", got)
	}
	s.end(errors.New("lost"))
	waitAttaches(t, src, 2)
	if p := src.param(1); p.Cols != 90 || p.Rows != 20 {
		t.Errorf("reattach size %dx%d, want 90x20", p.Cols, p.Rows)
	}
}

func TestResizeDuringTheAttachIsSentOnceLive(t *testing.T) {
	fast(t)
	src := newFakeSource()
	src.gate0 = make(chan struct{})
	st := &states{ch: make(chan State, 16)}
	c := NewConn(src, Options{OnState: st.on}) // attaches at 80x24 and waits
	defer c.Close()
	time.Sleep(30 * time.Millisecond)
	_ = c.ResizePixels(101, 31, 8, 17)
	close(src.gate0)
	s := src.stream(t, 0)
	st.wait(t, Live)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.resizes) != 1 || s.resizes[0] != [2]uint16{101, 31} {
		t.Errorf("resizes %v, want 101x31", s.resizes)
	}
}

func TestFocusSetDuringTheAttachIsSentOnceLive(t *testing.T) {
	fast(t)
	src := newFakeSource()
	src.gate0 = make(chan struct{})
	st := &states{ch: make(chan State, 16)}
	c := NewConn(src, Options{OnState: st.on})
	defer c.Close()
	time.Sleep(30 * time.Millisecond)
	_ = c.SetFocus(true)
	close(src.gate0)
	s := src.stream(t, 0)
	st.wait(t, Live)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.focus) != 1 || !s.focus[0] {
		t.Errorf("focus %v, want [true]", s.focus)
	}
}

func TestFocusIsSentAndSentAgainAfterAnAttach(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s0 := src.stream(t, 0)
	st.wait(t, Live)
	if err := c.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	s0.mu.Lock()
	if len(s0.focus) != 1 || !s0.focus[0] {
		t.Errorf("focus %v, want [true]", s0.focus)
	}
	s0.mu.Unlock()
	s0.end(errors.New("lost"))
	s1 := src.stream(t, 1)
	st.wait(t, Live)
	s1.mu.Lock()
	defer s1.mu.Unlock()
	if len(s1.focus) != 1 || !s1.focus[0] {
		t.Errorf("focus after the reattach %v, want [true]", s1.focus)
	}
}

func TestNoRetryWhileHidden(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	c.SetVisible(false)
	s.end(errors.New("lost"))
	st.wait(t, NotRunning)
	time.Sleep(80 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Fatalf("attaches while hidden = %d, want 1", n)
	}
	c.SetVisible(true)
	waitAttaches(t, src, 2)
	st.wait(t, Live)
}

func TestHidingCancelsAPendingRetry(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	baseRetry = 50 * time.Millisecond
	s.end(errors.New("lost"))
	st.wait(t, NotRunning)
	c.SetVisible(false) // the timer is pending
	time.Sleep(150 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Fatalf("attaches = %d, want 1", n)
	}
}

func TestCloseEndsReadAndClosesTheStreamAndSource(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	errc := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 8)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != io.EOF {
			t.Errorf("Read error %v, want io.EOF", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Read did not end")
	}
	select {
	case _, ok := <-s.events:
		if ok {
			t.Error("the stream is still open")
		}
	default:
		t.Error("the stream was not closed")
	}
	if src.closed != 1 {
		t.Errorf("source closed %d times, want 1", src.closed)
	}
	if err := c.Close(); err != nil { // a second Close is fine
		t.Error(err)
	}
	if _, err := c.Write([]byte("x")); err == nil {
		t.Error("Write after Close should fail")
	}
}

func TestCloseDoesNotWaitForAReaderThatStopped(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	s.data("unread") // nobody calls Read: the pump waits to hand it over
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close waits for the pump")
	}
}

func TestCloseDuringAnAttachEndsIt(t *testing.T) {
	fast(t)
	src := newFakeSource()
	src.gate = make(chan struct{}) // blocks every attach after the first
	c, st := start(t, src)
	st.wait(t, Live)
	src.stream(t, 0).end(errors.New("lost"))
	st.wait(t, NotRunning)
	waitAttaches(t, src, 1)
	done := make(chan struct{})
	go func() { _ = c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close waits for a blocked attach")
	}
}

func TestRetryDelayCapped(t *testing.T) {
	if retryDelay(0) != baseRetry || retryDelay(1) != 2*baseRetry {
		t.Error("delay does not double")
	}
	if retryDelay(50) != maxRetry {
		t.Errorf("delay = %v, want %v", retryDelay(50), maxRetry)
	}
}

func TestResizeAndTheFirstTimerDoNotSkipTheBackoff(t *testing.T) {
	fast(t)
	firstWait = 30 * time.Millisecond
	baseRetry, maxRetry = time.Hour, time.Hour
	src := newFakeSource(&hc.Error{Kind: "unavailable"})
	st := &states{ch: make(chan State, 16)}
	c := NewConn(src, Options{OnState: st.on})
	defer c.Close()
	_ = c.ResizePixels(100, 30, 8, 17) // starts the first attach, which fails
	st.wait(t, NotRunning)
	time.Sleep(100 * time.Millisecond) // the first timer has fired
	_ = c.ResizePixels(101, 30, 8, 17)
	time.Sleep(20 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Errorf("attaches = %d, want 1: only the backoff or input retries", n)
	}
}

func TestWriteAfterTheProgramEndedFailsAndShowingTheWindowDoesNotAttach(t *testing.T) {
	src := newFakeSource()
	c, st := start(t, src)
	src.stream(t, 0).end(&hc.StreamEnd{Reason: "exited"})
	st.wait(t, NotRunning)
	if _, err := c.Write([]byte("x")); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Write error %v, want ErrNotRunning", err)
	}
	c.SetVisible(false)
	c.SetVisible(true) // retries only a failure that is not final
	time.Sleep(60 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Errorf("attaches = %d, want 1", n)
	}
}

func TestDataResetsTheBackoff(t *testing.T) {
	src := newFakeSource(&hc.Error{Kind: "unavailable"}, &hc.Error{Kind: "unavailable"})
	c, st := start(t, src)
	s := src.stream(t, 0)
	st.wait(t, Live)
	c.mu.Lock()
	failed := c.failures
	c.mu.Unlock()
	if failed != 2 {
		t.Fatalf("failures = %d, want 2", failed)
	}
	s.data("x")
	readN(t, c, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures != 0 {
		t.Errorf("failures = %d after data, want 0", c.failures)
	}
}

// fakeScreen records the sizes Conn gives the screen.
type fakeScreen struct {
	mu    sync.Mutex
	sizes [][2]int
}

func (s *fakeScreen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sizes = append(s.sizes, [2]int{cols, rows})
}

func (s *fakeScreen) last() [2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sizes) == 0 {
		return [2]int{}
	}
	return s.sizes[len(s.sizes)-1]
}

func (s *fakeScreen) wait(t *testing.T, cols, rows int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for s.last() != [2]int{cols, rows} {
		if time.Now().After(deadline) {
			t.Fatalf("screen size %v, want %dx%d", s.last(), cols, rows)
		}
		time.Sleep(time.Millisecond)
	}
}

func startWatch(t *testing.T, src *fakeSource) (*Conn, *states, *fakeScreen) {
	t.Helper()
	fast(t)
	return startWatchNow(t, src)
}

// startWatchSized is startWatch, but the first attach waits for the view's
// size, as in the app. With the wait at zero, a loaded machine can attach at
// the default size before the test gives the size (issue #369).
func startWatchSized(t *testing.T, src *fakeSource) (*Conn, *states, *fakeScreen) {
	t.Helper()
	fast(t)
	firstWait = time.Hour // fast restores it on cleanup
	return startWatchNow(t, src)
}

func startWatchNow(t *testing.T, src *fakeSource) (*Conn, *states, *fakeScreen) {
	t.Helper()
	st := &states{ch: make(chan State, 256)}
	c := NewConn(src, Options{OnState: st.on, Watch: true})
	scr := &fakeScreen{}
	c.SetScreen(scr)
	t.Cleanup(func() { _ = c.Close() })
	return c, st, scr
}

// While it watches and the core's size is the view's, Conn sends no size or
// focus, attaches no more, and the screen follows the core's size (D91, D199).
func TestWatchKeepsTheCoreSize(t *testing.T) {
	src := newFakeSource()
	c, st, scr := startWatchSized(t, src)
	if err := c.ResizePixels(90, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	st.wait(t, Live)
	if p := src.param(0); !p.Observe || !p.ResizeNotifications || p.Cols != 90 || p.Rows != 20 {
		t.Errorf("attach params %+v", p)
	}
	s := src.stream(t, 0)
	s.events <- hc.StreamEvent{Resize: &hc.Size{Cols: 90, Rows: 20}}
	s.data("ab")
	if got := readN(t, c, 2); got != "ab" {
		t.Errorf("read %q", got)
	}
	if got := scr.last(); got != [2]int{90, 20} {
		t.Errorf("screen %v, want the core's 90x20 before the output", got)
	}
	if err := c.ResizePixels(90, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Errorf("%d attaches, want 1", n)
	}
	s.mu.Lock()
	sent := len(s.resizes) + len(s.focus)
	s.mu.Unlock()
	if sent != 0 {
		t.Errorf("an observer sent resizes %v focus %v", s.resizes, s.focus)
	}
}

// A core size other than the view's makes Conn attach with input at the
// view's size, with no input and no focus; the screen takes the view's size.
// The focus goes with the first key (D199).
func TestWatchFitsTheViewWhenTheCoreIsWider(t *testing.T) {
	src := newFakeSource()
	c, st, scr := startWatchSized(t, src)
	if err := c.ResizePixels(90, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	st.wait(t, Live)
	obs := src.stream(t, 0)
	obs.events <- hc.StreamEvent{Resize: &hc.Size{Cols: 120, Rows: 40}}
	go func() {
		buf := make([]byte, 8)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	waitAttaches(t, src, 2)
	if p := src.param(1); p.Observe || p.Cols != 90 || p.Rows != 20 || !p.Resumed {
		t.Errorf("fit attach %+v", p)
	}
	scr.wait(t, 90, 20)
	in := src.stream(t, 1)
	time.Sleep(20 * time.Millisecond)
	in.mu.Lock()
	focus, resizes := len(in.focus), len(in.resizes)
	in.mu.Unlock()
	if in.wrote() != "" || obs.wrote() != "" || focus != 0 || resizes != 0 {
		t.Errorf("fit sent input %q/%q focus %d resizes %d", obs.wrote(), in.wrote(), focus, resizes)
	}
	if err := c.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	in.mu.Lock()
	focus = len(in.focus)
	in.mu.Unlock()
	if focus != 0 {
		t.Errorf("focus sent before a key")
	}
	if _, err := c.Write([]byte("k")); err != nil {
		t.Fatal(err)
	}
	in.mu.Lock()
	gotFocus := append([]bool(nil), in.focus...)
	in.mu.Unlock()
	if len(gotFocus) != 1 || !gotFocus[0] || in.wrote() != "k" {
		t.Errorf("after a key: focus %v input %q", gotFocus, in.wrote())
	}
	if n := src.attachCount(); n != 2 {
		t.Errorf("%d attaches, want 2", n)
	}
}

// A view resize while it watches fits the view when the core's size differs.
func TestWatchFitsAfterAViewResize(t *testing.T) {
	src := newFakeSource()
	c, st, scr := startWatchSized(t, src)
	if err := c.ResizePixels(90, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	st.wait(t, Live)
	obs := src.stream(t, 0)
	obs.events <- hc.StreamEvent{Resize: &hc.Size{Cols: 90, Rows: 20}}
	obs.data("a")
	if got := readN(t, c, 1); got != "a" {
		t.Fatalf("read %q", got)
	}
	if err := c.ResizePixels(70, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	waitAttaches(t, src, 2)
	if p := src.param(1); p.Observe || p.Cols != 70 || p.Rows != 20 {
		t.Errorf("fit attach %+v", p)
	}
	go func() { buf := make([]byte, 8); _, _ = c.Read(buf) }()
	scr.wait(t, 70, 20)
}

// The first key ends the observer stream and attaches with input at the
// view's size; the key reaches the program, and the screen takes that size.
func TestWatchTakesOverOnTheFirstKey(t *testing.T) {
	src := newFakeSource()
	c, st, scr := startWatchSized(t, src)
	if err := c.ResizePixels(90, 20, 8, 16); err != nil {
		t.Fatal(err)
	}
	if err := c.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	st.wait(t, Live)
	obs := src.stream(t, 0)
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	waitAttaches(t, src, 2)
	if p := src.param(1); p.Observe || p.Cols != 90 || p.Rows != 20 || !p.Resumed {
		t.Errorf("second attach %+v", p)
	}
	in := src.stream(t, 1)
	deadline := time.Now().Add(2 * time.Second)
	for in.wrote() != "x" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if in.wrote() != "x" || obs.wrote() != "" {
		t.Errorf("input: observer %q, live %q", obs.wrote(), in.wrote())
	}
	go func() { buf := make([]byte, 8); _, _ = c.Read(buf) }()
	scr.wait(t, 90, 20)
	in.mu.Lock()
	focus := in.focus
	in.mu.Unlock()
	if len(focus) != 1 || !focus[0] {
		t.Errorf("focus %v, want true once", focus)
	}

	// From now on Conn sizes the terminal, and the screen follows at once.
	if err := c.ResizePixels(100, 30, 8, 16); err != nil {
		t.Fatal(err)
	}
	scr.wait(t, 100, 30)
	in.mu.Lock()
	resizes := in.resizes
	in.mu.Unlock()
	if len(resizes) != 1 || resizes[0] != [2]uint16{100, 30} {
		t.Errorf("resizes %v", resizes)
	}
	for {
		select {
		case s := <-st.ch:
			if s.Kind == NotRunning {
				t.Errorf("the take-over showed %+v", s)
			}
			continue
		default:
		}
		break
	}
}

// A key during the first attach, an observer's, leads to an attach with
// input, and the key goes only to that one.
func TestWatchKeyDuringTheFirstAttach(t *testing.T) {
	src := newFakeSource()
	gate := make(chan struct{})
	src.gate0 = gate
	c, st, _ := startWatch(t, src)
	time.Sleep(20 * time.Millisecond) // the first attach waits at the gate
	if _, err := c.Write([]byte("y")); err != nil {
		t.Fatal(err)
	}
	close(gate)
	st.wait(t, Live)
	n := src.attachCount()
	live := src.stream(t, n-1)
	if p := src.param(n - 1); p.Observe {
		t.Errorf("last attach %+v, want input", p)
	}
	deadline := time.Now().Add(2 * time.Second)
	for live.wrote() != "y" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if live.wrote() != "y" {
		t.Errorf("live stream got %q", live.wrote())
	}
	for i := range n - 1 {
		if got := src.stream(t, i).wrote(); got != "" {
			t.Errorf("observer stream %d got %q", i, got)
		}
	}
}

// A resize frame that comes before the view's first size does not fit the
// core to the 80x24 default: the fit waits for the view's size (D199).
func TestWatchFitWaitsForTheViewSize(t *testing.T) {
	src := newFakeSource()
	c, st, _ := startWatch(t, src)
	st.wait(t, Live)
	obs := src.stream(t, 0)
	go func() {
		buf := make([]byte, 8)
		for {
			if _, err := c.Read(buf); err != nil {
				return
			}
		}
	}()
	obs.events <- hc.StreamEvent{Resize: &hc.Size{Cols: 200, Rows: 120}}
	time.Sleep(50 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Fatalf("attaches before the view's size = %d, want 1", n)
	}
	if err := c.ResizePixels(124, 47, 8, 16); err != nil {
		t.Fatal(err)
	}
	waitAttaches(t, src, 2)
	if p := src.param(1); p.Observe || p.Cols != 124 || p.Rows != 47 {
		t.Errorf("fit attach %+v", p)
	}
}
