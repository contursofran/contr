package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func orchEnv(t *testing.T, profiles string) (func(string) string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agents.json")
	if profiles != "" {
		if err := os.WriteFile(path, []byte(profiles), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return func(k string) string {
		if k == "CONTR_AGENTS_CONFIG" {
			return path
		}
		return ""
	}, path
}

func TestSpawnOrchestratorStartsTheProfileWithRoleAndNoParent(t *testing.T) {
	getenv, _ := orchEnv(t, `{"profiles":{"orchestrator":{"argv":["claude"]}}}`)
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "o", "live", nil)}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	now := time.Date(2026, 11, 3, 10, 4, 5, 0, time.UTC)
	if err := spawnOrchestrator(context.Background(), getenv, dial, "/work/My Proj", now); err != nil {
		t.Fatal(err)
	}
	if len(core.params) != 1 {
		t.Fatalf("calls %q", core.calls)
	}
	p := core.params[0]
	l := p["labels"].(map[string]any)
	if l["role"] != "orchestrator" || l["profile"] != "orchestrator" || l["project"] != "My Proj" {
		t.Errorf("labels %v", l)
	}
	if p["no_parent"] != true || p["cwd"] != "/work/My Proj" || p["name"] != "orch-my-proj-0405" {
		t.Errorf("params %v", p)
	}
	if !core.closed {
		t.Error("the connection must close")
	}
}

func TestSpawnOrchestratorNamesTheFileWhenTheProfileIsMissing(t *testing.T) {
	getenv, path := orchEnv(t, "")
	dialed := false
	dial := func(context.Context, func(string) string) (agentsCore, error) { dialed = true; return &cliCore{}, nil }
	err := spawnOrchestrator(context.Background(), getenv, dial, "/work/p", time.Now())
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), `"orchestrator"`) {
		t.Fatalf("error %v", err)
	}
	if dialed {
		t.Error("no connection without the profile")
	}
}

func TestSpawnOrchestratorReturnsTheCoreError(t *testing.T) {
	getenv, _ := orchEnv(t, `{"profiles":{"orchestrator":{"argv":["claude"]}}}`)
	core := &cliCore{errs: map[string]error{"terminal.run": errors.New("name taken")}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	if err := spawnOrchestrator(context.Background(), getenv, dial, "/work/p", time.Now()); err == nil || !strings.Contains(err.Error(), "name taken") {
		t.Errorf("error %v", err)
	}
	dial = func(context.Context, func(string) string) (agentsCore, error) { return nil, errors.New("no hc") }
	if err := spawnOrchestrator(context.Background(), getenv, dial, "/work/p", time.Now()); err == nil || !strings.Contains(err.Error(), "no hc") {
		t.Errorf("dial error %v", err)
	}
}

func TestOrchestratorNameFitsHC(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 1, 2, 0, time.UTC)
	long := orchestratorName("/x/"+strings.Repeat("Abc.", 20), now)
	if len(long) > 32 || !strings.HasSuffix(long, "-0102") {
		t.Errorf("name %q", long)
	}
	if got := orchestratorName("/", now); got != "orch--0102" {
		t.Errorf("root: %q", got)
	}
}
