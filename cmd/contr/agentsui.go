package main

import (
	"context"
	"errors"
	"log"
	"sync/atomic"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/hc"
)

// terminalSource gives the terminal view the source of one hc terminal. It
// dials the same core as the agents, on connections of its own, and only when
// the view attaches (D12).
func terminalSource(getenv func(string) string) func(address string) ghostty.Source {
	return func(address string) ghostty.Source {
		return ghostty.NewCoreSource(hc.Command(getenv("CONTR_HC"), nil), address)
	}
}

// lazySource is the frame's agent source. The connection to the hc core opens
// on a goroutine, so the window never waits for it; until the model is up, or
// when it fails, the snapshot is empty and the actions return errNoAgents.
type lazySource struct {
	model atomic.Pointer[agentstate.Model]
	fail  atomic.Pointer[string] // why the connection failed
}

var errNoAgents = errors.New("agents not connected")

func (l *lazySource) Snapshot() agentstate.Snapshot {
	if m := l.model.Load(); m != nil {
		return m.Snapshot()
	}
	if f := l.fail.Load(); f != nil {
		return agentstate.Snapshot{Problem: *f}
	}
	return agentstate.Snapshot{}
}

func (l *lazySource) Answer(id, answer string) error {
	if m := l.model.Load(); m != nil {
		return m.Answer(id, answer)
	}
	return errNoAgents
}

func (l *lazySource) Done(id string) error {
	if m := l.model.Load(); m != nil {
		return m.Done(id)
	}
	return errNoAgents
}

func (l *lazySource) ClearDone() {
	if m := l.model.Load(); m != nil {
		m.ClearDone()
	}
}

func (l *lazySource) Read(name string, lines int) (string, error) {
	if m := l.model.Load(); m != nil {
		return m.Read(name, lines)
	}
	return "", errNoAgents
}

func (l *lazySource) SendKeys(name string, keys []byte) error {
	if m := l.model.Load(); m != nil {
		return m.SendKeys(name, keys)
	}
	return errNoAgents
}

func (l *lazySource) Interrupt(name string) error {
	if m := l.model.Load(); m != nil {
		return m.Interrupt(name)
	}
	return errNoAgents
}

func (l *lazySource) Stop(name string) error {
	if m := l.model.Load(); m != nil {
		return m.Stop(name)
	}
	return errNoAgents
}

// startAgents gives the frame its agent state.
func startAgents(getenv func(string) string) func(notify func()) (app.AgentSource, func()) {
	return func(notify func()) (app.AgentSource, func()) {
		src := &lazySource{}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			m, closeCore, err := openAgentState(ctx, getenv, notify)
			if err != nil {
				log.Printf("contr: agents: %v", err)
				msg := "agents: " + err.Error()
				src.fail.Store(&msg)
				notify()
				return
			}
			defer closeCore()
			src.model.Store(m)
			m.Start(ctx)
			notify()
			<-ctx.Done()
			m.Wait()
		}()
		return src, func() { cancel(); <-done }
	}
}

func openAgentState(ctx context.Context, getenv func(string) string, notify func()) (*agentstate.Model, func(), error) {
	cfg, err := agents.LoadConfig(configPath(getenv))
	if err != nil {
		return nil, nil, err
	}
	store, err := ownerStore(getenv)
	if err != nil {
		return nil, nil, err
	}
	core, err := dialAgents(ctx, getenv)
	if err != nil {
		return nil, nil, err
	}
	return agentstate.New(agents.New(core, cfg), store, notify), func() { _ = core.Close() }, nil
}
