//go:build hccore

package main

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestRealCoreMCPReconnects: after the core restarts, the MCP manager's
// connection ends. Every later call must work: the first one finds the
// connection closed before it sends, so it connects again (#262).
func TestRealCoreMCPReconnects(t *testing.T) {
	core := hctest.New(t, "contr-mr", hctest.Session("mr"))
	dial := func(ctx context.Context, _ func(string) string) (agentsCore, error) {
		cl, err := hc.Dial(ctx, hc.Command("", core.Env))
		if err != nil {
			return nil, err
		}
		return agents.HC{Client: cl}, nil
	}
	m := &managers{dial: dial, getenv: func(string) string { return "" }, cfg: agents.DefaultConfig()}
	defer m.close()
	list := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := m.do(ctx, func(mgr *agents.Manager) (string, error) {
			_, err := mgr.List(ctx, true)
			return "", err
		})
		return err
	}
	if err := list(); err != nil {
		t.Fatal(err)
	}
	restart := exec.Command("hc", "server", "restart")
	restart.Env = append(restart.Environ(), core.Env...)
	if out, err := restart.CombinedOutput(); err != nil {
		t.Fatalf("restart: %v %s", err, out)
	}
	var errs []error
	for i := 0; i < 4; i++ {
		errs = append(errs, list())
		time.Sleep(200 * time.Millisecond)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d after the restart: %v", i+1, err)
		}
	}
}
