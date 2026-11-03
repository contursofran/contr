package hc

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recv returns the next event, or fails the test after a second.
func recv(t *testing.T, s *Subscription) (Event, bool) {
	t.Helper()
	select {
	case e, ok := <-s.Events():
		return e, ok
	case <-time.After(time.Second):
		t.Fatal("no event")
		return Event{}, false
	}
}

func TestSubscribeUsesItsOwnConnection(t *testing.T) {
	var dials atomic.Int32
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		dials.Add(1)
		if !f.hello(1) {
			return
		}
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method != "events.subscribe" {
			// The call connection: answer calls until the end.
			for ok {
				f.reply(r.ID, "ok")
				r, ok = f.next()
			}
			return
		}
		if string(r.Params) != `{"labels":{"app":"contr"}}` {
			t.Errorf("subscribe params %s", r.Params)
		}
		f.reply(r.ID, map[string]any{"seq": 3, "terminals": []any{map[string]any{"address": "t1"}}})
		f.notify("event", map[string]any{"seq": 4, "terminal": "t2", "type": "created", "data": map[string]any{"address": "t2"}})
		f.notify("heartbeat", map[string]any{})
		f.notify("event", map[string]any{"seq": 5, "terminal": "t2", "type": "finished", "data": map[string]any{"exit_code": 0}})
		f.next() // until the client closes
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	s, err := cl.Subscribe(ctx(t), Filter{Labels: map[string]string{"app": "contr"}})
	if err != nil {
		t.Fatal(err)
	}
	if n := dials.Load(); n != 2 {
		t.Errorf("dials = %d, want 2", n)
	}
	if s.Snapshot.Seq != 3 || len(s.Snapshot.Terminals) != 1 {
		t.Errorf("snapshot = %+v", s.Snapshot)
	}

	e, _ := recv(t, s)
	if e.Method != "event" || e.Seq != 4 || e.Type != "created" || e.Terminal != "t2" {
		t.Errorf("event 1 = %+v", e)
	}
	if e, _ = recv(t, s); e.Method != "heartbeat" {
		t.Errorf("event 2 = %+v", e)
	}
	e, _ = recv(t, s)
	var data struct {
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal(e.Data, &data); err != nil || e.Type != "finished" || e.Seq != 5 {
		t.Errorf("event 3 = %+v (%v)", e, err)
	}

	// The call connection still works while the subscription is open.
	var got string
	if err := cl.Call(ctx(t), "terminal.list", nil, &got); err != nil || got != "ok" {
		t.Errorf("call = %q, %v", got, err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := recv(t, s); ok {
		t.Error("Events still open after Close")
	}
	if s.Err() != nil {
		t.Errorf("Err after Close = %v", s.Err())
	}
}

func TestSubscriptionEndsWhenTheCoreGoesAway(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method != "events.subscribe" {
			f.next()
			return
		}
		f.reply(r.ID, map[string]any{"seq": 0, "terminals": []any{}})
		// End the connection with no more events.
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	s, err := cl.Subscribe(ctx(t), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := recv(t, s); ok {
		t.Fatal("got an event, want the channel closed")
	}
	if s.Err() == nil {
		t.Error("Err = nil after the core went away")
	}
}

func TestSubscribeReportsAnErrorReply(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method == "events.subscribe" {
			f.replyErr(r.ID, "not_found", "no terminal t9", "")
		}
		f.next()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if _, err := cl.Subscribe(ctx(t), Filter{Terminal: "t9"}); !IsKind(err, "not_found") {
		t.Fatalf("err = %v, want not_found", err)
	}
}

func TestCloseDoesNotWaitForAReceiver(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method != "events.subscribe" {
			f.next()
			return
		}
		f.reply(r.ID, map[string]any{"seq": 0, "terminals": []any{}})
		for i := range 200 { // more than the channel holds
			f.notify("event", map[string]any{"seq": i + 1, "terminal": "t1", "type": "status"})
		}
		f.next()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	s, err := cl.Subscribe(ctx(t), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // let the reader fill the channel and wait
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked on a full event channel")
	}
}

func TestABadEventEndsTheSubscription(t *testing.T) {
	cl, err := Dial(ctx(t), fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method != "events.subscribe" {
			f.next()
			return
		}
		f.reply(r.ID, map[string]any{"seq": 0, "terminals": []any{}})
		f.notify("event", map[string]any{"seq": "not a number"})
		f.notify("event", map[string]any{"seq": 2, "terminal": "t1", "type": "status"})
		f.next()
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	s, err := cl.Subscribe(ctx(t), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if e, ok := recv(t, s); ok {
		t.Fatalf("got %+v after a bad event, want the channel closed", e)
	}
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "bad event") {
		t.Errorf("Err = %v, want a bad event error", err)
	}
}

func TestErrCarriesTheCoreSideOutput(t *testing.T) {
	base := fakeDialer(t, func(f *fakeConn) {
		f.hello(1)
		r, ok := f.next()
		if !ok {
			return
		}
		if r.Method != "events.subscribe" {
			f.next()
			return
		}
		f.reply(r.ID, map[string]any{"seq": 0, "terminals": []any{}})
	})
	dial := func(c context.Context) (Pipe, error) {
		p, err := base(c)
		p.Stderr = func() string { return "core start failed" }
		return p, err
	}
	cl, err := Dial(ctx(t), dial)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	s, err := cl.Subscribe(ctx(t), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	recv(t, s)
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "core start failed") {
		t.Errorf("Err = %v, want the core side output", err)
	}
}
