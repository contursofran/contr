package main

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

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/hc"
)

func TestStartAgentsWithoutACoreShowsNoAgentsAndStops(t *testing.T) {
	env := map[string]string{
		"CONTR_HC":            filepath.Join(t.TempDir(), "no-such-hc"),
		"CONTR_AGENTS_CONFIG": filepath.Join(t.TempDir(), "agents.json"),
		"CONTR_OWNER_FILE":    filepath.Join(t.TempDir(), "owner.json"),
	}
	var notified atomic.Int32
	src, stop := startAgents(func(k string) string { return env[k] }, dialAgents)(func() { notified.Add(1) })
	if s := src.Snapshot(); len(s.Workers)+len(s.Orchestrators) != 0 {
		t.Errorf("snapshot %+v, want empty", s)
	}
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return")
	}
}

func TestStartAgentsShowsWhyTheCoreFailed(t *testing.T) {
	env := map[string]string{
		"CONTR_HC":            filepath.Join(t.TempDir(), "no-such-hc"),
		"CONTR_AGENTS_CONFIG": filepath.Join(t.TempDir(), "agents.json"),
		"CONTR_OWNER_FILE":    filepath.Join(t.TempDir(), "owner.json"),
	}
	var notified atomic.Int32
	src, stop := startAgents(func(k string) string { return env[k] }, dialAgents)(func() { notified.Add(1) })
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for src.Snapshot().Problem == "" {
		if time.Now().After(deadline) {
			t.Fatal("no problem shown")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p := src.Snapshot().Problem; !strings.Contains(p, "no-such-hc") {
		t.Errorf("problem %q does not name the program", p)
	}
	if notified.Load() == 0 {
		t.Error("the window was not told")
	}
}

// scriptDial gives the cores of a list in turn; a nil core, or the end of
// the list, is a failed dial. It counts the dials.
type scriptDial struct {
	mu    sync.Mutex
	cores []*cliCore
	n     int
}

func (d *scriptDial) dial(context.Context, func(string) string) (agentsCore, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.n
	d.n++
	if i >= len(d.cores) || d.cores[i] == nil {
		return nil, errors.New("no hc yet")
	}
	return d.cores[i], nil
}

func (d *scriptDial) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.n
}

// liveCore is a core whose subscription shows terms and stays open.
func liveCore(terms ...map[string]any) *cliCore {
	var raw []json.RawMessage
	for _, t := range terms {
		b, _ := json.Marshal(t)
		raw = append(raw, b)
	}
	return &cliCore{stream: &cliStream{snap: hc.Snapshot{Terminals: raw}, events: make(chan hc.Event)}}
}

// startScripted starts the agent state on d with a scratch config.
func startScripted(t *testing.T, d *scriptDial) agentstate.AgentSource {
	t.Helper()
	env := map[string]string{
		"CONTR_AGENTS_CONFIG": filepath.Join(t.TempDir(), "agents.json"),
		"CONTR_OWNER_FILE":    filepath.Join(t.TempDir(), "owner.json"),
	}
	src, stop := startAgents(func(k string) string { return env[k] }, d.dial)(func() {})
	t.Cleanup(stop)
	return src
}

func waitUntil(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStartAgentsDialsAgainAfterAFailedDial(t *testing.T) {
	d := &scriptDial{cores: []*cliCore{nil, liveCore(worker("t1", "w1", "running", nil))}}
	src := startScripted(t, d)
	waitUntil(t, 5*time.Second, "the dial problem", func() bool {
		return strings.Contains(src.Snapshot().Problem, "no hc yet")
	})
	waitUntil(t, 5*time.Second, "the list after a later dial", func() bool {
		s := src.Snapshot()
		return s.Problem == "" && len(s.Workers) == 1
	})
	if d.dials() != 2 {
		t.Errorf("dials = %d, want 2", d.dials())
	}
}

func TestStartAgentsActionsUseTheNewConnection(t *testing.T) {
	a := liveCore(worker("t1", "w", "running", nil))
	a.errs = map[string]error{"terminal.send": hc.ErrNotSent}
	b := liveCore()
	d := &scriptDial{cores: []*cliCore{a, b}}
	src := startScripted(t, d)
	waitUntil(t, 5*time.Second, "the list", func() bool { return len(src.Snapshot().Workers) == 1 })
	if err := src.Send("w", "x"); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.calls) != 1 || b.calls[0] != "terminal.send" {
		t.Fatalf("b calls = %v, want one terminal.send", b.calls)
	}
}

func TestStartAgentsKeepsVisibilitySetBeforeTheModel(t *testing.T) {
	d := &scriptDial{}
	src := startScripted(t, d)
	src.SetVisible(false)
	time.Sleep(1500 * time.Millisecond)
	if n := d.dials(); n != 1 {
		t.Fatalf("dials = %d while hidden, want 1", n)
	}
	src.SetVisible(true)
	waitUntil(t, 5*time.Second, "a dial on show", func() bool { return d.dials() >= 2 })
}

func TestStartAgentsReconnectClick(t *testing.T) {
	d := &scriptDial{}
	src := startScripted(t, d)
	waitUntil(t, 5*time.Second, "the problem", func() bool { return src.Snapshot().Problem != "" })
	src.Reconnect()
	waitUntil(t, 200*time.Millisecond, "a dial on Reconnect, before the 500 ms backoff", func() bool { return d.dials() >= 2 })
}
