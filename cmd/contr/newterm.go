package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/contursofran/contr/internal/agents"
)

// termCount makes the names of terminals opened in the same second differ.
var termCount atomic.Uint32

// terminalName is `term-<MMSS><n>`: the time as in orchestrator names, then a
// counter.
func terminalName(now time.Time, n uint32) string {
	return fmt.Sprintf("term-%s%d", now.Format("0405"), n)
}

// openTerminal starts a login shell in a plain hc terminal for project, in
// dir, on a connection of its own (D81), and returns the terminal's address.
func openTerminal(ctx context.Context, getenv func(string) string, dial func(context.Context, func(string) string) (agentsCore, error), project, dir string, now time.Time) (string, error) {
	core, err := dial(ctx, getenv)
	if err != nil {
		return "", err
	}
	defer core.Close()
	w, err := agents.New(core, agents.DefaultConfig()).OpenTerminal(ctx, agents.TerminalSpec{
		Name: terminalName(now, termCount.Add(1)), Project: project, Dir: dir,
	})
	if err != nil {
		return "", err
	}
	return w.Address, nil
}
