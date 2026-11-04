package main

import (
	"errors"
	"strings"
	"testing"
)

const permissionScreen = `╭──────────────────────────────╮
│ Bash command                 │
│                              │
│   npm test                   │
│                              │
│ Do you want to proceed?      │
│ ❯ 1. Yes                     │
│   2. No                      │
╰──────────────────────────────╯`

func blockedWorker(state string, status map[string]any) map[string]any {
	w := worker("t7", "b1", state, status)
	w["foreground"] = map[string]any{"name": "claude"}
	return w
}

func TestAgentsPromptPrintsTheMenu(t *testing.T) {
	core := &cliCore{reply: map[string]any{
		"terminal.get":  blockedWorker("live", map[string]any{"state": "blocked", "kind": "permission"}),
		"terminal.read": map[string]any{"text": permissionScreen},
	}}
	code, out, errs := run(t, core, nil, "prompt", "b1")
	want := "Bash command\nnpm test\nDo you want to proceed?\n1. Yes\n2. No\n"
	if code != 0 || out != want {
		t.Errorf("prompt: code %d, out %q, err %q; want %q", code, out, errs, want)
	}
	if core.calls[1] != "terminal.read" || core.params[1]["lines"] != float64(promptLines) {
		t.Errorf("calls %v, read params %v", core.calls, core.params)
	}
}

func TestAgentsPromptNone(t *testing.T) {
	// Not blocked: the screen is not read.
	core := &cliCore{reply: map[string]any{"terminal.get": blockedWorker("live", map[string]any{"state": "working"})}}
	if code, out, _ := run(t, core, nil, "prompt", "b1"); code != 0 || out != "no prompt\n" || len(core.calls) != 1 {
		t.Errorf("working: code %d, out %q, calls %v", code, out, core.calls)
	}
	// Blocked, but no menu that contr can read.
	core = &cliCore{reply: map[string]any{
		"terminal.get":  blockedWorker("live", map[string]any{"state": "blocked", "kind": "permission"}),
		"terminal.read": map[string]any{"text": "just text"},
	}}
	if code, out, _ := run(t, core, nil, "prompt", "b1"); code != 0 || out != "no prompt\n" {
		t.Errorf("unreadable: code %d, out %q", code, out)
	}
	// An hc error is an error.
	core = &cliCore{errs: map[string]error{"terminal.get": errors.New("not_found")}}
	if code, _, errs := run(t, core, nil, "prompt", "zz"); code != 1 || !strings.Contains(errs, "not_found") {
		t.Errorf("unknown: code %d, err %q", code, errs)
	}
	if code, _, _ := run(t, &cliCore{}, nil, "prompt"); code != 2 {
		t.Errorf("no name: code %d, want 2", code)
	}
}
