package terminal

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/term"
)

type fakeStream struct {
	events chan hc.StreamEvent
	done   chan struct{}
	once   sync.Once

	mu      sync.Mutex
	err     error
	written []byte
	resizes []hc.Size
	focus   []bool
}

func newFakeStream() *fakeStream {
	return &fakeStream{events: make(chan hc.StreamEvent, 256), done: make(chan struct{})}
}

func (s *fakeStream) Events() <-chan hc.StreamEvent { return s.events }
func (s *fakeStream) Done() <-chan struct{}         { return s.done }
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
	s.resizes = append(s.resizes, hc.Size{Cols: c, Rows: r})
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
		close(s.done)
	})
}
func (s *fakeStream) Close() error { s.end(nil); return nil }
func (s *fakeStream) data(t string) {
	s.events <- hc.StreamEvent{Data: []byte(t)}
}
func (s *fakeStream) wrote() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.written)
}

type fakeSource struct {
	mu       sync.Mutex
	streams  []*fakeStream
	params   []hc.AttachParams
	errs     []error // one per attach call, nil for success
	watchCh  chan hc.Event
	attached chan struct{}
}

func newFakeSource(errs ...error) *fakeSource {
	return &fakeSource{errs: errs, watchCh: make(chan hc.Event, 8), attached: make(chan struct{}, 16)}
}

func (f *fakeSource) Attach(_ context.Context, p hc.AttachParams) (HCStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := len(f.params)
	f.params = append(f.params, p)
	f.attached <- struct{}{}
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

func (f *fakeSource) attachCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.params)
}

type updates struct{ ch chan term.LinkUpdate }

func newUpdates() *updates { return &updates{ch: make(chan term.LinkUpdate, 1024)} }

func (u *updates) notify(x term.LinkUpdate) { u.ch <- x }

// next returns the next update of the wanted type name; others are skipped.
func (u *updates) waitState(t *testing.T, kind term.LinkStateKind) term.LinkState {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case x := <-u.ch:
			if s, ok := x.(term.StateUpdate); ok && s.State.Kind == kind {
				return s.State
			}
		case <-timeout:
			t.Fatalf("no state %d", kind)
		}
	}
}

func (u *updates) waitScreen(t *testing.T) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case x := <-u.ch:
			if _, ok := x.(term.ScreenUpdate); ok {
				return
			}
		case <-timeout:
			t.Fatal("no screen update")
		}
	}
}

func (u *updates) count() (screens int) {
	for {
		select {
		case x := <-u.ch:
			if _, ok := x.(term.ScreenUpdate); ok {
				screens++
			}
		default:
			return
		}
	}
}

func rowText(s *term.Screen, y int) string {
	var b strings.Builder
	for _, c := range s.Rows[y] {
		b.WriteString(c.Text)
	}
	return b.String()
}

func startLink(t *testing.T, src *fakeSource) (*HCLink, *updates) {
	t.Helper()
	u := newUpdates()
	l := NewHCLink(src, term.GridSize{Cols: 40, Rows: 10}, term.CellPx{}, u.notify)
	t.Cleanup(l.Stop)
	return l, u
}

func TestLinkPublishesScreen(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	src.stream(t, 0).data("hello\r\nworld")
	u.waitScreen(t)
	// The first paint can arrive before the data; wait for the text.
	deadline := time.Now().Add(2 * time.Second)
	for {
		scr := l.TakeLatest()
		if scr != nil && strings.HasPrefix(rowText(scr, 0), "hello") && strings.HasPrefix(rowText(scr, 1), "world") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen = %q", rowText(l.TakeLatest(), 0))
		}
		time.Sleep(time.Millisecond)
	}
	p := src.params[0]
	if p.History != 1000 || p.Cols != 40 || p.Rows != 10 || p.Resumed || !p.ResizeNotifications {
		t.Errorf("attach params = %+v", p)
	}
}

func TestLinkCoalescesNotify(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	s := src.stream(t, 0)
	u.count()
	for i := 0; i < 50; i++ {
		s.data("x\r\n")
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := u.count(); n != 1 {
		t.Fatalf("screen updates without TakeLatest = %d, want 1", n)
	}
	l.TakeLatest()
	s.data("y")
	u.waitScreen(t) // taking re-arms the notify
}

func TestLinkResizeSendsFrame(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	s := src.stream(t, 0)
	if err := l.Resize(term.GridSize{Cols: 100, Rows: 30}, term.CellPx{}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	got := append([]hc.Size(nil), s.resizes...)
	s.mu.Unlock()
	if len(got) != 1 || got[0] != (hc.Size{Cols: 100, Rows: 30}) {
		t.Fatalf("resizes = %v", got)
	}
	deadline := time.Now().Add(2 * time.Second)
	for l.TakeLatest() == nil || l.TakeLatest().Size.Cols != 100 {
		if time.Now().After(deadline) {
			t.Fatal("emulator did not resize")
		}
		time.Sleep(time.Millisecond)
	}
	// The same size again sends nothing.
	_ = l.Resize(term.GridSize{Cols: 100, Rows: 30}, term.CellPx{})
	s.mu.Lock()
	n := len(s.resizes)
	s.mu.Unlock()
	if n != 1 {
		t.Errorf("same-size resize sent a frame: %d", n)
	}
}

func TestLinkCoreResizeFollowed(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	src.stream(t, 0).events <- hc.StreamEvent{Resize: &hc.Size{Cols: 60, Rows: 20}}
	deadline := time.Now().Add(2 * time.Second)
	for l.TakeLatest() == nil || l.TakeLatest().Size.Cols != 60 {
		if time.Now().After(deadline) {
			t.Fatal("emulator did not follow the core")
		}
		time.Sleep(time.Millisecond)
	}
	// The core's size is not echoed back.
	src.stream(t, 0).mu.Lock()
	defer src.stream(t, 0).mu.Unlock()
	if len(src.stream(t, 0).resizes) != 0 {
		t.Errorf("sent a resize for a core resize")
	}
}

func TestLinkReattachOnRestarted(t *testing.T) {
	src := newFakeSource()
	_, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	src.stream(t, 0).end(&hc.StreamEnd{Reason: "exited"})
	st := u.waitState(t, term.LinkNotRunning)
	if !strings.Contains(st.Message, "exited") {
		t.Errorf("message = %q", st.Message)
	}
	src.watchCh <- hc.Event{Method: "event", Type: "labels"} // other events do nothing
	src.watchCh <- hc.Event{Method: "event", Type: "restarted"}
	u.waitState(t, term.LinkLive)
	src.stream(t, 1)
	p := src.params[1]
	if !p.Resumed || p.History != 1000 {
		t.Errorf("reattach params = %+v", p)
	}
	if src.attachCount() != 2 {
		t.Errorf("attaches = %d", src.attachCount())
	}
}

func TestLinkFinishedNotRunning(t *testing.T) {
	src := newFakeSource(&hc.Error{Kind: "finished", Message: "done"})
	l, u := startLink(t, src)
	st := u.waitState(t, term.LinkNotRunning)
	if !strings.Contains(st.Message, "done") {
		t.Errorf("message = %q", st.Message)
	}
	// Input does not retry a finished terminal; a restarted event does.
	if err := l.SendInput("", []term.InputEvent{term.TextEvent{Text: "a"}}); !errors.Is(err, term.ErrNotConnected) {
		t.Errorf("err = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := src.attachCount(); n != 1 {
		t.Errorf("attaches after input = %d, want 1", n)
	}
	src.watchCh <- hc.Event{Method: "event", Type: "restarted"}
	u.waitState(t, term.LinkLive)
}

func TestLinkIncompatible(t *testing.T) {
	src := newFakeSource(&hc.Error{Kind: hc.KindIncompatible, Message: "no attach"})
	_, u := startLink(t, src)
	st := u.waitState(t, term.LinkIncompatible)
	if st.Message == "" {
		t.Error("no message")
	}
}

func TestLinkUnavailableRetriesOnInput(t *testing.T) {
	src := newFakeSource(&hc.Error{Kind: "unavailable", Message: "recovering"})
	l, u := startLink(t, src)
	u.waitState(t, term.LinkNotRunning)
	if err := l.SendInput("", []term.InputEvent{term.TextEvent{Text: "a"}}); !errors.Is(err, term.ErrNotConnected) {
		t.Errorf("err = %v", err)
	}
	u.waitState(t, term.LinkLive)
	if n := src.attachCount(); n != 2 {
		t.Errorf("attaches = %d, want 2", n)
	}
}

func TestLinkSendInputEncodes(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	s := src.stream(t, 0)
	err := l.SendInput("", []term.InputEvent{
		term.TextEvent{Text: "ls"},
		term.KeyEvent{Code: term.KeyCode{Kind: term.KeyEnter}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.wrote(); got != "ls\r" {
		t.Errorf("written = %q", got)
	}
	// With DECCKM set by the program, an arrow key is SS3.
	s.data("\x1b[?1h")
	deadline := time.Now().Add(2 * time.Second)
	for !l.emu.Modes().AppCursor {
		if time.Now().After(deadline) {
			t.Fatal("mode not set")
		}
		time.Sleep(time.Millisecond)
	}
	_ = l.SendInput("", []term.InputEvent{term.KeyEvent{Code: term.KeyCode{Kind: term.KeyUp}}})
	if got := s.wrote(); got != "ls\r\x1bOA" {
		t.Errorf("written = %q", got)
	}
}

func TestLinkWheelScrollsHistory(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	s := src.stream(t, 0)
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("line\r\n")
	}
	s.data(b.String())
	waitFor := func(want uint64) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			if scr := l.TakeLatest(); scr != nil {
				if p := scr.FocusedPane(); p != nil && p.Scroll != nil && p.Scroll.OffsetFromBottom == want {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("offset never reached %d", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(0)
	if err := l.SendInput("", []term.InputEvent{term.WheelEvent{Dir: term.WheelUp, Lines: 3}}); err != nil {
		t.Fatal(err)
	}
	waitFor(3)
	if got := s.wrote(); got != "" {
		t.Errorf("a history scroll wrote %q to the program", got)
	}
	_ = l.SendInput("", []term.InputEvent{term.TextEvent{Text: "x"}})
	waitFor(0) // typing shows the bottom
}

func TestLinkFocusReport(t *testing.T) {
	src := newFakeSource()
	l, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	s := src.stream(t, 0)
	if err := l.SetFocus(true); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if len(s.focus) != 1 || !s.focus[0] {
		t.Errorf("focus frames = %v", s.focus)
	}
	s.mu.Unlock()
	if got := s.wrote(); got != "" {
		t.Errorf("focus report without mode 1004: %q", got)
	}
	s.data("\x1b[?1004h")
	deadline := time.Now().Add(2 * time.Second)
	for !l.emu.Modes().Focus {
		if time.Now().After(deadline) {
			t.Fatal("mode not set")
		}
		time.Sleep(time.Millisecond)
	}
	_ = l.SetFocus(false)
	if got := s.wrote(); got != "\x1b[O" {
		t.Errorf("written = %q", got)
	}
}

func TestLinkClipboard(t *testing.T) {
	src := newFakeSource()
	_, u := startLink(t, src)
	u.waitState(t, term.LinkLive)
	src.stream(t, 0).data("\x1b]52;c;aGVsbG8=\x07")
	timeout := time.After(2 * time.Second)
	for {
		select {
		case x := <-u.ch:
			if c, ok := x.(term.ClipboardUpdate); ok {
				if c.Text != "hello" {
					t.Fatalf("text = %q", c.Text)
				}
				return
			}
		case <-timeout:
			t.Fatal("no clipboard update")
		}
	}
}

func TestLinkNoGoroutineLeakOnStop(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()
	src := newFakeSource()
	u := newUpdates()
	l := NewHCLink(src, term.GridSize{Cols: 40, Rows: 10}, term.CellPx{}, u.notify)
	u.waitState(t, term.LinkLive)
	src.stream(t, 0).data("abc")
	l.Stop()
	select {
	case <-src.stream(t, 0).done:
	default:
		t.Error("Stop did not close the stream")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines %d > %d\n%s", runtime.NumGoroutine(), before, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(5 * time.Millisecond)
	}
	l.Stop() // a second Stop is fine
}
