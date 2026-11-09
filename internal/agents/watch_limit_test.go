package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

// shortWatchLimits sets subscribeLimit and getLimit for one test.
func shortWatchLimits(t *testing.T, sub, get time.Duration) {
	t.Helper()
	oldSub, oldGet := subscribeLimit, getLimit
	subscribeLimit, getLimit = sub, get
	t.Cleanup(func() { subscribeLimit, getLimit = oldSub, oldGet })
}

// silentSub is a core whose Subscribe and terminal.get never answer: they
// wait until their context ends. With stream set, Subscribe returns it.
type silentSub struct {
	fakeCore
	silentGet bool
}

func (c *silentSub) Subscribe(ctx context.Context, f hc.Filter) (Stream, error) {
	if c.stream != nil {
		return c.stream, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *silentSub) Call(ctx context.Context, method string, params, result any) error {
	if c.silentGet && method == "terminal.get" {
		<-ctx.Done()
		return ctx.Err()
	}
	return c.fakeCore.Call(ctx, method, params, result)
}

// watchEnds runs WatchAll on core with a context that does not end and
// returns its error, or fails when the watch does not end in time.
func watchEnds(t *testing.T, core Core) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- New(core, DefaultConfig()).WatchAll(context.Background(), func([]Worker) {}) }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the watch did not end at the time limit")
		return nil
	}
}

func TestWatchSubscribeEndsAtTheTimeLimit(t *testing.T) {
	shortWatchLimits(t, 30*time.Millisecond, time.Hour)
	if err := watchEnds(t, &silentSub{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestWatchGetEndsAtTheTimeLimit(t *testing.T) {
	shortWatchLimits(t, time.Hour, 30*time.Millisecond)
	s := &fakeStream{events: make(chan hc.Event, 1)}
	s.events <- hc.Event{Method: "event", Terminal: "t1", Type: "status"}
	if err := watchEnds(t, &silentSub{fakeCore: fakeCore{stream: s}, silentGet: true}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
}

// The subscribe limit ends only the subscribe: the stream goes on after it.
func TestWatchOutlivesTheSubscribeLimit(t *testing.T) {
	shortWatchLimits(t, 10*time.Millisecond, time.Hour)
	s := &fakeStream{events: make(chan hc.Event)}
	core := &fakeCore{stream: s, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(map[string]any) (any, error) {
			return term("t1", "w", "running", map[string]string{"contr": "agent", "role": "builder"}, nil), nil
		},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lists := make(chan int, 4)
	done := make(chan error, 1)
	go func() {
		done <- New(core, DefaultConfig()).WatchAll(ctx, func(ws []Worker) { lists <- len(ws) })
	}()
	<-lists // the snapshot
	time.Sleep(50 * time.Millisecond)
	select {
	case s.events <- hc.Event{Method: "event", Terminal: "t1", Type: "created"}:
	case err := <-done:
		t.Fatalf("the watch ended after the subscribe limit: %v", err)
	}
	select {
	case n := <-lists:
		if n != 1 {
			t.Errorf("list has %d workers, want 1", n)
		}
	case err := <-done:
		t.Fatalf("the watch ended after the subscribe limit: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("no list after the event")
	}
	cancel()
	<-done
}
