package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBrief writes a brief with quotes and $ signs: it must reach the
// worker as is.
func writeBrief(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const brief = "Fix #12.\nRun `echo \"$HOME\"` and $(cat x) as text.\n"

func TestAgentsSpawnTaskFile(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	code, _, errs := run(t, core, nil, "spawn", "--name", "b", "--profile", "builder", "--task-file", writeBrief(t, brief))
	if code != 0 {
		t.Fatalf("code %d err %q", code, errs)
	}
	argv := core.params[0]["argv"].([]any)
	if argv[len(argv)-1] != brief {
		t.Errorf("task = %q, want the file text", argv[len(argv)-1])
	}
}

func TestAgentsSpawnTaskFileErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing", []string{"--task-file", "/no/such/brief.md"}, "task file:"},
		{"empty", []string{"--task-file", writeBrief(t, " \n\t\n")}, "is empty"},
		{"both", []string{"--task-file", writeBrief(t, brief), "--", "also", "text"}, "not both"},
	}
	for _, c := range cases {
		core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
		args := append([]string{"spawn", "--name", "b", "--profile", "builder"}, c.args...)
		code, _, errs := run(t, core, nil, args...)
		if code != 1 || !strings.Contains(errs, c.want) {
			t.Errorf("%s: code %d err %q, want %q", c.name, code, errs, c.want)
		}
		if len(core.calls) != 0 {
			t.Errorf("%s: a worker started: %v", c.name, core.calls)
		}
	}
	// --task-file belongs to spawn only.
	if code, _, _ := run(t, &cliCore{}, nil, "ls", "--task-file", "x"); code != 2 {
		t.Errorf("ls --task-file: code %d, want 2", code)
	}
}

func TestMCPSpawnTaskFile(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	_, r := mcpRun(t, dial, map[string]string{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		callLine(2, "spawn", map[string]any{"name": "b", "profile": "builder", "task_file": writeBrief(t, brief)}),
		callLine(3, "spawn", map[string]any{"name": "c", "profile": "builder"}),
		callLine(4, "spawn", map[string]any{"name": "d", "profile": "builder", "task": "x", "task_file": writeBrief(t, brief)}),
	)
	if got, isErr := toolText(t, r["2"]); isErr || !strings.Contains(got, `"address": "t4"`) {
		t.Errorf("task_file: %q, error %v", got, isErr)
	}
	if argv := core.params[0]["argv"].([]any); argv[len(argv)-1] != brief {
		t.Errorf("task = %q", argv[len(argv)-1])
	}
	if got, isErr := toolText(t, r["3"]); !isErr || !strings.Contains(got, "task or task_file") {
		t.Errorf("no brief: %q, error %v", got, isErr)
	}
	if got, isErr := toolText(t, r["4"]); !isErr || !strings.Contains(got, "not both") {
		t.Errorf("both: %q, error %v", got, isErr)
	}
	if len(core.calls) != 1 {
		t.Errorf("calls %v, want one spawn", core.calls)
	}
}
