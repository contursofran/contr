//go:build e2e

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/session"
)

// seeChatFlow spawns lead and writes a transcript whose answer has a flow diagram (D284).
func seeChatFlow(t *testing.T, h *harness) {
	repo := h.repo(t, "demo", "main")
	lead, err := h.mgr.Spawn(h.ctx, agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatalf("spawn lead: %v", err)
	}
	path := filepath.Join(h.home, ".claude", "projects", "demo", "flow.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(flowTranscript()), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session.Session{UpdatedAt: uint64(time.Now().Unix())}
	s.SetTranscript("flow", path)
	if err := session.Write(filepath.Join(h.dir, "state"), lead.Address, s); err != nil {
		t.Fatal(err)
	}
}

// flowTranscript is a prompt and an answer with a contr-flow block: a branch, a loop back and
// two states.
func flowTranscript() string {
	start := time.Now().Add(-2 * time.Minute)
	line := func(n int, typ string, content any) string {
		b, _ := json.Marshal(map[string]any{"type": typ, "uuid": fmt.Sprintf("u%d", n), "sessionId": "flow",
			"timestamp": start.Add(time.Duration(n*10) * time.Second).UTC().Format(time.RFC3339Nano),
			"message": map[string]any{"role": typ, "content": content}})
		return string(b) + "\n"
	}
	return strings.Join([]string{
		line(1, "user", "how does a fix get merged?"),
		line(2, "assistant", []any{map[string]any{"type": "text", "text": "It goes through review:\n\n```contr-flow\n" +
			"issue -> plan: owner picks\nplan -> build\nbuild -> review: PR\nreview -> merge: approved\n" +
			"review -> build: blockers\nmerge -> release\nmerge [done]\nreview [waiting]\n```\n\nA blocker sends it back to the builder."}}),
	}, "")
}
