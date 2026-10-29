package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/herdr"
)

func connected() herdr.ConnState {
	return herdr.ConnState{Kind: herdr.Connected, ServerVersion: "0.9.3"}
}

func emptyTree() *herdr.HerdrTree {
	return &herdr.HerdrTree{Version: "0.9.3"}
}

func stateUpdate(s herdr.ConnState) herdr.Update { return herdr.Update{State: s} }
func treeUpdate() herdr.Update                   { return herdr.Update{Tree: emptyTree()} }

// detached is a Herdr with no reader, fed only by the test.
func detached(visible bool) (*Herdr, *atomic.Int32) {
	var count atomic.Int32
	h := &Herdr{
		onChange: func() { count.Add(1) },
		cancel:   func() {},
		live:     herdr.NewLiveHerdr(),
		visible:  visible,
	}
	return h, &count
}

func TestAReaderThatDidNotStartMeansNotRunning(t *testing.T) {
	reader, live := readerOrNotRunning(nil, errors.New("no thread"))
	if reader != nil {
		t.Fatal("reader should be nil")
	}
	want := herdr.LiveHerdr{State: herdr.ConnState{Kind: herdr.NotRunning, Reason: "no thread"}}
	if live != want {
		t.Fatalf("live = %+v, want %+v", live, want)
	}
}

func TestAStartedReaderIsKeptAndStartsConnecting(t *testing.T) {
	// A socket that does not exist: the reader retries until it is stopped.
	target := herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock"))
	h := NewHerdr(context.Background(), target, true, nil)
	defer h.Close()
	if h.reader == nil {
		t.Fatal("reader should be kept")
	}
	if got := h.Live(); got.State.Kind != herdr.Connecting && got.State.Kind != herdr.NotRunning {
		t.Fatalf("state = %+v, want connecting or not running", got.State)
	}
	if h.Live().Tree != nil {
		t.Fatal("tree should be nil")
	}
}

func TestApplyNotifiesOnlyWhenSomethingChanged(t *testing.T) {
	h, count := detached(true)
	step := func(u herdr.Update, want int32) {
		t.Helper()
		h.apply(u)
		if got := count.Load(); got != want {
			t.Fatalf("notifications = %d, want %d", got, want)
		}
	}
	step(stateUpdate(connected()), 1)
	step(stateUpdate(connected()), 1) // same state again
	step(treeUpdate(), 2)
	step(treeUpdate(), 2) // same tree, new allocation
	step(stateUpdate(herdr.ConnState{Kind: herdr.NotRunning, Reason: "gone"}), 3)
	if h.Live().Tree != nil {
		t.Fatal("tree should be dropped")
	}
}

func TestChangesWhileHiddenAreKeptAndAnnouncedOnceShown(t *testing.T) {
	h, count := detached(true)
	h.SetVisible(false)
	h.apply(stateUpdate(connected()))
	h.apply(treeUpdate())
	if count.Load() != 0 {
		t.Fatal("hidden changes must not notify")
	}
	if h.Live().Tree == nil {
		t.Fatal("hidden changes must be kept")
	}
	h.SetVisible(true)
	if got := count.Load(); got != 1 {
		t.Fatalf("showing announces the hidden changes once, got %d", got)
	}
	h.apply(stateUpdate(herdr.ConnState{Kind: herdr.Connecting}))
	if got := count.Load(); got != 2 {
		t.Fatalf("shown changes notify at once, got %d", got)
	}
}

func TestShowingWithoutAHiddenChangeDoesNotNotify(t *testing.T) {
	h, count := detached(true)
	h.apply(stateUpdate(connected()))
	before := count.Load()
	h.SetVisible(false)
	h.SetVisible(true)
	if got := count.Load(); got != before {
		t.Fatalf("notifications = %d, want %d", got, before)
	}
}

func TestTheReaderFollowsTheWindow(t *testing.T) {
	target := herdr.SocketTarget(filepath.Join(t.TempDir(), "missing.sock"))
	h := NewHerdr(context.Background(), target, false, nil)
	defer h.Close()
	if h.reader.Visible() {
		t.Fatal("a reader made while the window is hidden starts hidden")
	}
	h.SetVisible(true)
	if !h.reader.Visible() {
		t.Fatal("reader should be visible")
	}
	h.SetVisible(false)
	if h.reader.Visible() {
		t.Fatal("reader should be hidden")
	}
}

type fakeReader struct{ visible atomic.Bool }

func (f *fakeReader) SetVisible(v bool) { f.visible.Store(v) }
func (f *fakeReader) Visible() bool     { return f.visible.Load() }
func (f *fakeReader) Stop()             {}

func TestUpdatesFromTheReaderReachTheStateInOrder(t *testing.T) {
	var out chan<- herdr.Update
	start := func(_ context.Context, _ herdr.Target, o chan<- herdr.Update) (readerHandle, error) {
		out = o
		return &fakeReader{}, nil
	}
	changed := make(chan struct{}, 8)
	h := newHerdr(context.Background(), herdr.Target{}, true, func() { changed <- struct{}{} }, start)
	defer h.Close()
	out <- stateUpdate(connected())
	out <- treeUpdate()
	deadline := time.After(5 * time.Second)
	for h.Live().Tree == nil {
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("tree did not arrive")
		}
	}
	live := h.Live()
	if live.State != connected() {
		t.Fatalf("state = %+v, want connected", live.State)
	}
	if !live.Tree.Equal(*emptyTree()) {
		t.Fatalf("tree = %+v", live.Tree)
	}
}
