package agentstate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// silentCore is a core that never answers: each call waits until its
// context ends.
type silentCore struct{ calls atomic.Int32 }

func (c *silentCore) Call(ctx context.Context, _ string, _, _ any) error {
	c.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func (c *silentCore) Subscribe(ctx context.Context, _ hc.Filter) (agents.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *silentCore) Close() error { return nil }

// shortActions sets actionLimit to d for one test.
func shortActions(t *testing.T, d time.Duration) {
	t.Helper()
	old := actionLimit
	actionLimit = d
	t.Cleanup(func() { actionLimit = old })
}

func newSilentModel(t *testing.T, core agents.Core) *Model {
	t.Helper()
	store := &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")}
	return New(agents.New(core, agents.DefaultConfig()), store, nil)
}

// actions are the window's actions on hc, each as a func that returns its
// error.
func actions(m *Model) map[string]func() error {
	return map[string]func() error{
		"Read":      func() error { _, err := m.Read("w", 10); return err },
		"Send":      func() error { return m.Send("w", "hi") },
		"SendKeys":  func() error { return m.SendKeys("w", []byte{3}) },
		"Interrupt": func() error { return m.Interrupt("w") },
		"Stop":      func() error { return m.Stop("w") },
		"Close":     func() error { return m.Close("w") },
	}
}

// endsInTime runs fn and fails when it does not return
// context.DeadlineExceeded soon after the limit.
func endsInTime(t *testing.T, name string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: err = %v, want context.DeadlineExceeded", name, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: no return after the time limit", name)
	}
}

func TestEachActionEndsAtTheTimeLimit(t *testing.T) {
	shortActions(t, 30*time.Millisecond)
	core := &silentCore{}
	m := newSilentModel(t, core)
	for name, fn := range actions(m) {
		endsInTime(t, name, fn)
	}
	if core.calls.Load() == 0 {
		t.Error("no call reached the core")
	}
}

func TestAnswerShowsTheTimeLimitError(t *testing.T) {
	shortActions(t, 30*time.Millisecond)
	m := newSilentModel(t, &silentCore{})
	q, err := m.store.Ask(owner.Question{Coordinator: "t1", Question: "Proceed?"})
	if err != nil {
		t.Fatal(err)
	}
	endsInTime(t, "Answer", func() error { return m.Answer(q.ID, "yes") })
	p := m.Snapshot().Problem
	if !strings.Contains(p, "coordinator not reached") || !strings.Contains(p, context.DeadlineExceeded.Error()) {
		t.Errorf("Problem = %q", p)
	}
}

// A dial that does not end is under the same limit: Redial dials with the
// action's context.
func TestActionEndsWhenTheDialHangs(t *testing.T) {
	shortActions(t, 30*time.Millisecond)
	rd := agents.NewRedial(func(ctx context.Context) (agents.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	m := newSilentModel(t, rd)
	for name, fn := range actions(m) {
		endsInTime(t, name, fn)
	}
}

// A call that passes its limit keeps the connection: the next action uses
// the same one and does not dial again.
func TestTimedOutCallKeepsTheConnection(t *testing.T) {
	shortActions(t, 30*time.Millisecond)
	var dials atomic.Int32
	core := &silentCore{}
	rd := agents.NewRedial(func(context.Context) (agents.Conn, error) {
		dials.Add(1)
		return core, nil
	})
	m := newSilentModel(t, rd)
	endsInTime(t, "Send", func() error { return m.Send("w", "hi") })
	endsInTime(t, "Send again", func() error { return m.Send("w", "hi") })
	if dials.Load() != 1 {
		t.Errorf("dials = %d, want 1", dials.Load())
	}
	if core.calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", core.calls.Load())
	}
}
