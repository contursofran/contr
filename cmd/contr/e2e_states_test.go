//go:build e2e

package main

import (
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/agents"
)

// TestWorkerStates is #532: a fake Claude that reports working or blocked
// shows that state in the sidebar, not idle. The PNG goes to
// tmp/e2e/worker-states.png.
//
//	go test -tags e2e -run TestWorkerStates ./cmd/contr
func TestWorkerStates(t *testing.T) {
	h := newHarness(t)
	repo := seeWorkersRepo(t, h)
	h.spawn(t, agents.Spec{Name: "b252-s1-cleanup", Profile: "claude-blocked", Role: "builder", Issue: "252", Project: "demo", Worktree: repo})
	h.theme(t, "dark")
	c := h.launch(t)
	c.waitLabel(t, "lead, ")
	for label, want := range map[string]string{
		"b201-s1-notch, ":   "b201-s1-notch, working, 201",
		"b252-s1-cleanup, ": "b252-s1-cleanup, needs you, 252, permission", // the block kind (D249)
		"b202-s1-outline, ": "b202-s1-outline, idle, 202",
	} {
		if got := c.waitLabel(t, label); got != want {
			t.Errorf("row %q = %q, want %q", label, got, want)
		}
	}
	c.settle(t)
	c.capture(t, filepath.Join(h.out, "worker-states.png"))
}
