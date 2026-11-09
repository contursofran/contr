package agentstate

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// subStream is a stream with its own snapshot and events.
type subStream struct {
	snap   hc.Snapshot
	events chan hc.Event
}

func (s *subStream) Snapshot() hc.Snapshot   { return s.snap }
func (s *subStream) Events() <-chan hc.Event { return s.events }
func (s *subStream) Err() error              { return nil }
func (s *subStream) Close() error            { return nil }

// endStream gives a stream that sends the snapshot of terms and then ends,
// so the watch returns hc.ErrClosed.
func endStream(terms ...json.RawMessage) func() (agents.Stream, error) {
	return func() (agents.Stream, error) {
		ch := make(chan hc.Event)
		close(ch)
		return &subStream{snap: hc.Snapshot{Terminals: terms}, events: ch}, nil
	}
}

// liveStream gives a stream that sends the snapshot of terms and stays open.
func liveStream(terms ...json.RawMessage) func() (agents.Stream, error) {
	return func() (agents.Stream, error) {
		return &subStream{snap: hc.Snapshot{Terminals: terms}, events: make(chan hc.Event)}, nil
	}
}

// failSub gives a Subscribe that fails with err.
func failSub(err error) func() (agents.Stream, error) {
	return func() (agents.Stream, error) { return nil, err }
}

// retryWaits sets retryBase and retryMax for one test.
func retryWaits(t *testing.T, base, most time.Duration) {
	t.Helper()
	b, m := retryBase, retryMax
	retryBase, retryMax = base, most
	t.Cleanup(func() { retryBase, retryMax = b, m })
}

// problems is a rig whose notify records each problem text.
type problems struct {
	mu   sync.Mutex
	seen []string
}

func (p *problems) add(s string) {
	p.mu.Lock()
	p.seen = append(p.seen, s)
	p.mu.Unlock()
}

func (p *problems) any(sub string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.seen {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// newRetryRig is a rig whose Subscribe calls give subs in turn and whose
// notify records the problem texts.
func newRetryRig(t *testing.T, subs ...func() (agents.Stream, error)) (*rig, *problems) {
	t.Helper()
	r := newRig(t)
	r.core.subs = subs
	p := &problems{}
	r.m.notify = func() {
		r.pings.Add(1)
		p.add(r.m.Snapshot().Problem)
	}
	return r, p
}

func names(ws []agents.Worker) string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Name)
	}
	return strings.Join(out, ",")
}

func TestStreamEndShowsTheProblemThenConnectsAgain(t *testing.T) {
	retryWaits(t, 20*time.Millisecond, time.Second)
	r, p := newRetryRig(t,
		endStream(term("t1", "w1", "builder", "contr", "")),
		liveStream(term("t2", "w2", "builder", "contr", "")))
	r.start(t)
	waitFor(t, "fresh list", func() bool {
		s := r.m.Snapshot()
		return s.Problem == "" && names(s.Workers) == "w2"
	})
	if !p.any("connection closed") {
		t.Errorf("problems seen = %q, want one with connection closed", p.seen)
	}
	if n := r.core.subN.Load(); n != 2 {
		t.Errorf("subscribes = %d, want 2", n)
	}
}

func TestFirstSubscribeFailsThenWorks(t *testing.T) {
	retryWaits(t, 20*time.Millisecond, time.Second)
	r, p := newRetryRig(t, failSub(errors.New("no hc yet")),
		liveStream(term("t1", "w1", "builder", "contr", "")))
	r.start(t)
	waitFor(t, "list", func() bool {
		s := r.m.Snapshot()
		return s.Problem == "" && names(s.Workers) == "w1"
	})
	if !p.any("hc: no hc yet") {
		t.Errorf("problems seen = %q, want hc: no hc yet", p.seen)
	}
}

func TestGetErrorInApplyConnectsAgain(t *testing.T) {
	retryWaits(t, 20*time.Millisecond, time.Second)
	events := make(chan hc.Event, 1)
	events <- hc.Event{Method: "event", Terminal: "t1", Type: "updated"}
	r, p := newRetryRig(t,
		func() (agents.Stream, error) { return &subStream{events: events}, nil },
		liveStream(term("t1", "w1", "builder", "contr", "")))
	r.core.errs = map[string]error{"terminal.get": errors.New("boom")}
	r.start(t)
	waitFor(t, "second subscribe", func() bool {
		return r.core.subN.Load() == 2 && r.m.Snapshot().Problem == ""
	})
	if !p.any("boom") {
		t.Errorf("problems seen = %q, want boom", p.seen)
	}
}

func TestRetryDelay(t *testing.T) {
	for _, tc := range []struct {
		failures int
		want     time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 5 * time.Second},
		{10, 5 * time.Second},
	} {
		if got := retryDelay(tc.failures); got != tc.want {
			t.Errorf("retryDelay(%d) = %v, want %v", tc.failures, got, tc.want)
		}
	}
}

func TestBackoffResetsAfterAGoodWatch(t *testing.T) {
	retryWaits(t, 20*time.Millisecond, time.Hour)
	var subs []func() (agents.Stream, error)
	for range 8 {
		subs = append(subs, endStream(term("t1", "w1", "builder", "contr", "")))
	}
	r, _ := newRetryRig(t, subs...)
	start := time.Now()
	r.start(t)
	// Without the reset the waits add up to 20 ms * (2^8 - 1), about 5.1 s.
	for r.core.subN.Load() < 8 {
		if time.Since(start) > 2*time.Second {
			t.Fatalf("subscribes = %d after 2 s, want 8: the backoff did not reset", r.core.subN.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHiddenModelHasNoRetryTimer(t *testing.T) {
	retryWaits(t, 5*time.Millisecond, 5*time.Millisecond)
	r, _ := newRetryRig(t, failSub(errors.New("no hc")))
	r.m.SetVisible(false)
	r.start(t)
	time.Sleep(200 * time.Millisecond)
	if n := r.core.subN.Load(); n != 1 {
		t.Fatalf("subscribes = %d while hidden, want 1", n)
	}
}

func TestShowTriesOnceAtOnce(t *testing.T) {
	retryWaits(t, time.Hour, time.Hour)
	r, _ := newRetryRig(t, failSub(errors.New("no hc")), failSub(errors.New("no hc")))
	r.m.SetVisible(false)
	r.start(t)
	waitFor(t, "problem", func() bool { return r.m.Snapshot().Problem != "" })
	r.m.SetVisible(true)
	waitFor(t, "a try on show", func() bool { return r.core.subN.Load() == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := r.core.subN.Load(); n != 2 {
		t.Fatalf("subscribes = %d, want one try on show", n)
	}
}

func TestHideStopsTheRetryTimer(t *testing.T) {
	retryWaits(t, 100*time.Millisecond, 100*time.Millisecond)
	r, _ := newRetryRig(t, failSub(errors.New("no hc")))
	r.start(t)
	waitFor(t, "problem", func() bool { return r.m.Snapshot().Problem != "" })
	r.m.SetVisible(false)
	time.Sleep(300 * time.Millisecond)
	if n := r.core.subN.Load(); n != 1 {
		t.Fatalf("subscribes = %d after hide, want 1", n)
	}
}

func TestReconnectTriesAtOnce(t *testing.T) {
	retryWaits(t, time.Hour, time.Hour)
	r, _ := newRetryRig(t, failSub(errors.New("no hc")))
	r.start(t)
	waitFor(t, "problem", func() bool { return r.m.Snapshot().Problem != "" })
	r.m.Reconnect()
	waitFor(t, "a try on Reconnect", func() bool { return r.core.subN.Load() == 2 })
}

func TestReconnectWhileOnlineDoesNothing(t *testing.T) {
	retryWaits(t, time.Hour, time.Hour)
	r, _ := newRetryRig(t)
	r.start(t)
	waitFor(t, "online", func() bool { return r.core.subN.Load() == 1 && r.pings.Load() >= 2 })
	r.m.Reconnect()
	time.Sleep(100 * time.Millisecond)
	if n := r.core.subN.Load(); n != 1 {
		t.Fatalf("subscribes = %d, want 1", n)
	}
	r.m.mu.Lock()
	tryNow := r.m.tryNow
	r.m.mu.Unlock()
	if tryNow {
		t.Fatal("Reconnect while online asked for a try")
	}
}

func TestStopWhileWaitingEndsGoroutines(t *testing.T) {
	retryWaits(t, time.Hour, time.Hour)
	r, _ := newRetryRig(t, failSub(errors.New("no hc")))
	ctx, cancel := context.WithCancel(context.Background())
	r.m.Start(ctx)
	waitFor(t, "problem", func() bool { return r.m.Snapshot().Problem != "" })
	cancel()
	done := make(chan struct{})
	go func() { r.m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutines did not end while the model waited to retry")
	}
}

// closableCore is a fakeCore that can be closed and can say that it ended.
type closableCore struct {
	*fakeCore
	ended  atomic.Bool
	closed atomic.Bool
}

func (c *closableCore) Close() error { c.closed.Store(true); return nil }
func (c *closableCore) Closed() bool { return c.ended.Load() }

func TestActionAfterReconnectGoesToTheNewConnection(t *testing.T) {
	retryWaits(t, 20*time.Millisecond, time.Second)
	a := &closableCore{fakeCore: &fakeCore{}}
	b := &closableCore{fakeCore: &fakeCore{events: make(chan hc.Event),
		snap: hc.Snapshot{Terminals: []json.RawMessage{term("t2", "w", "builder", "contr", "")}}}}
	a.subs = []func() (agents.Stream, error){func() (agents.Stream, error) {
		a.ended.Store(true)
		return endStream(term("t1", "w", "builder", "contr", ""))()
	}}
	conns := []*closableCore{a, b}
	var dials, dialled atomic.Int32
	rd := agents.NewRedial(func(context.Context) (agents.Conn, error) {
		i := dials.Add(1) - 1
		if int(i) >= len(conns) {
			return nil, errors.New("no more conns")
		}
		return conns[i], nil
	})
	rd.OnDial = func(agents.Conn) { dialled.Add(1) }
	store := &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")}
	m := New(agents.New(rd, agents.DefaultConfig()), store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	t.Cleanup(func() { cancel(); m.Wait() })
	waitFor(t, "the list from b", func() bool {
		s := m.Snapshot()
		return dials.Load() == 2 && s.Problem == "" && len(s.Workers) == 1 && s.Workers[0].Address == "t2"
	})
	if err := m.Send("w", "hi"); err != nil {
		t.Fatal(err)
	}
	if len(b.sends()) != 1 || len(a.sends()) != 0 {
		t.Fatalf("sends: a = %d, b = %d, want the send on b", len(a.sends()), len(b.sends()))
	}
	if !a.closed.Load() {
		t.Error("the ended conn a was not closed")
	}
	if dialled.Load() != 2 {
		t.Errorf("OnDial ran %d times, want 2", dialled.Load())
	}
}
