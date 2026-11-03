package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/hc"
)

// lateCore is an agents.Core whose connection is made on a goroutine. Calls
// wait for it, so the window never does. If the dial fails, every call returns
// that error, and the model shows it as its problem.
type lateCore struct {
	ready chan struct{}
	mu    sync.Mutex
	core  agentsCore
	err   error
}

func (l *lateCore) set(c agentsCore, err error) {
	l.mu.Lock()
	l.core, l.err = c, err
	l.mu.Unlock()
	close(l.ready)
}

func (l *lateCore) wait(ctx context.Context) (agentsCore, error) {
	select {
	case <-l.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.core, l.err
}

func (l *lateCore) Call(ctx context.Context, method string, params, result any) error {
	c, err := l.wait(ctx)
	if err != nil {
		return err
	}
	return c.Call(ctx, method, params, result)
}

func (l *lateCore) Subscribe(ctx context.Context, f hc.Filter) (agents.Stream, error) {
	c, err := l.wait(ctx)
	if err != nil {
		return nil, err
	}
	return c.Subscribe(ctx, f)
}

// startAgentState is the window's [app.Frame.StartAgents]: it follows hc and
// owner.json. The needs-you row works from owner.json alone when hc is down.
func startAgentState(notify func()) (app.AgentSource, func()) {
	store, err := ownerStore(os.Getenv)
	if err != nil {
		log.Printf("contr: owner file: %v", err)
		return nil, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	core := &lateCore{ready: make(chan struct{})}
	go func() {
		c, err := dialAgents(ctx, os.Getenv)
		if err != nil {
			log.Printf("contr: hc: %v", err)
			core.set(nil, fmt.Errorf("hc: %w", err))
			return
		}
		core.set(c, nil)
	}()
	m := agentstate.New(agents.New(core, agents.DefaultConfig()), store, notify)
	m.Start(ctx)
	return m, func() {
		cancel()
		m.Wait()
		core.mu.Lock()
		if core.core != nil {
			_ = core.core.Close()
		}
		core.mu.Unlock()
	}
}
