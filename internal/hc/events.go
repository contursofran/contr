package hc

import (
	"context"
	"encoding/json"
	"sync"
)

// Filter selects the terminals of a subscription. Empty fields match all.
type Filter struct {
	Labels     map[string]string `json:"labels,omitempty"`
	Terminal   string            `json:"terminal,omitempty"`
	ChildrenOf string            `json:"children_of,omitempty"`
}

// Event is one notification of a subscription.
//
// Method "event" carries a fact change: Type is for example "created",
// "status", "turn", "labels", "finished" or "removed", Terminal is its
// address and Data its JSON. Method "heartbeat" comes every 15 s. Method
// "missed" means that the subscriber fell behind; a fresh snapshot follows in
// Data.
type Event struct {
	Method   string          `json:"-"`
	Seq      uint64          `json:"seq"`
	Terminal string          `json:"terminal"`
	Type     string          `json:"type"`
	Data     json.RawMessage `json:"data"`
}

// Snapshot is the reply to a subscription: the matching terminals at Seq.
// Each terminal stays raw JSON, so callers decode only what they need.
type Snapshot struct {
	Seq       uint64            `json:"seq"`
	Terminals []json.RawMessage `json:"terminals"`
}

// Subscription delivers the events of one filter on its own connection.
type Subscription struct {
	Snapshot Snapshot

	c      *conn
	events chan Event
	stop   chan struct{}
	once   sync.Once
}

// Subscribe opens a new connection and subscribes it to the events of f.
// The events come on Events, in order, until Close or until the connection
// ends; then Events is closed and Err tells why.
func (cl *Client) Subscribe(ctx context.Context, f Filter) (*Subscription, error) {
	pipe, err := cl.dial(ctx)
	if err != nil {
		return nil, err
	}
	s := &Subscription{events: make(chan Event, 64), stop: make(chan struct{})}
	s.c = newConn(pipe, s.deliver)
	go func() {
		<-s.c.done
		close(s.events)
	}()
	if _, err := hello(ctx, s.c); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := s.c.call(ctx, "events.subscribe", f, &s.Snapshot); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// deliver runs on the reader goroutine. It waits for the receiver, so a slow
// receiver slows the reader and the core sends "missed" when it falls behind.
func (s *Subscription) deliver(m *message) {
	e := Event{Method: m.Method}
	if m.Method == "event" && len(m.Params) > 0 {
		if err := json.Unmarshal(m.Params, &e); err != nil {
			s.c.fail(err)
			return
		}
		e.Method = m.Method
	} else {
		e.Data = m.Params
	}
	select {
	case s.events <- e:
	case <-s.stop:
	}
}

// Events returns the event channel. It is closed when the connection ends.
func (s *Subscription) Events() <-chan Event { return s.events }

// Err returns why the connection ended, or nil while it is open or after
// Close.
func (s *Subscription) Err() error {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	select {
	case <-s.stop:
		return nil
	default:
	}
	if !s.c.closed {
		return nil
	}
	return s.c.readErr
}

// Close ends the subscription and its connection.
func (s *Subscription) Close() error {
	var err error
	s.once.Do(func() {
		close(s.stop)
		err = s.c.close()
	})
	return err
}
