//go:build hccore

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestRealCoreAgentListReconnects: after the core restarts, the window's
// agent list connects again by itself, shows a terminal opened after the
// restart, and an action on it works (#559).
func TestRealCoreAgentListReconnects(t *testing.T) {
	core := hctest.New(t, "contr-ar", hctest.Session("ar"))
	dial := func(ctx context.Context, _ func(string) string) (agentsCore, error) {
		cl, err := hc.Dial(ctx, hc.Command("", core.Env))
		if err != nil {
			return nil, err
		}
		return agents.HC{Client: cl}, nil
	}
	dir := t.TempDir()
	env := map[string]string{
		"CONTR_AGENTS_CONFIG": filepath.Join(dir, "agents.json"),
		"CONTR_OWNER_FILE":    filepath.Join(dir, "owner.json"),
	}
	src, stop := startAgents(func(k string) string { return env[k] }, dial)(func() {})
	defer stop()
	src.SetVisible(true)
	waitUntil(t, 20*time.Second, "the core version", func() bool { return src.Snapshot().CoreVersion != "" })

	restart := exec.Command("hc", "server", "restart")
	restart.Env = append(restart.Environ(), core.Env...)
	if out, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("restart: %v %s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	w, err := agents.New(agents.HC{Client: cl}, agents.DefaultConfig()).OpenTerminal(ctx, agents.TerminalSpec{Name: "after-restart", Dir: dir, Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, "the list after the restart", func() bool {
		s := src.Snapshot()
		if s.Problem != "" || s.CoreVersion == "" {
			return false
		}
		for _, x := range s.Terminals {
			if x.Address == w.Address {
				return true
			}
		}
		return false
	})
	if err := src.Send(w.Address, "echo hi"); err != nil {
		t.Fatalf("send after the reconnect: %v", err)
	}
}
