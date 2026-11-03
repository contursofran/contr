package main

import (
	"context"
	"log"
	"sync/atomic"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/terminal"
	"github.com/contursofran/contr/internal/hc"
)

// terminalSource gives the terminal view the source of one hc terminal. It
// dials the same core as the agents, on connections of its own, and only when
// the view attaches (D12).
func terminalSource(getenv func(string) string) func(address string) terminal.HCSource {
	return func(address string) terminal.HCSource {
		return terminal.NewCoreSource(hc.Command(getenv("CONTR_HC"), nil), address)
	}
}

// startAgents gives the frame its agent state. The connection to the hc core
// opens on a goroutine, so the window never waits for it; until it is up, or
// when it fails, the snapshot is empty and the sidebar shows no agents.
func startAgents(getenv func(string) string) func(notify func()) (func() agentstate.Snapshot, func()) {
	return func(notify func()) (func() agentstate.Snapshot, func()) {
		var model atomic.Pointer[agentstate.Model]
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			m, closeCore, err := openAgentState(ctx, getenv, notify)
			if err != nil {
				log.Printf("contr: agents: %v", err)
				return
			}
			defer closeCore()
			model.Store(m)
			m.Start(ctx)
			notify()
			<-ctx.Done()
			m.Wait()
		}()
		snap := func() agentstate.Snapshot {
			if m := model.Load(); m != nil {
				return m.Snapshot()
			}
			return agentstate.Snapshot{}
		}
		return snap, func() { cancel(); <-done }
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
