package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	hookPrompt = `{"type":"user","message":{"role":"user","content":"do the task"}}`
	hookSend   = `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"hc send t1 \"b1: done\""}}]}}`
	hookWords  = `{"type":"assistant","message":{"content":[{"type":"text","text":"PR #5 is open.\nHead abc."}]}}`
)

// hookRun runs `contr hook stop` with a transcript of lines in a workerEnv
// folder, and returns the exit code, stderr, the fake core and the folder.
func hookRun(t *testing.T, env map[string]string, input string, lines ...string) (int, string, *cliCore, string) {
	t.Helper()
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(tr, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if input == "" {
		input = `{"transcript_path":"` + tr + `","cwd":"` + dir + `"}`
	}
	env = withStateDir(env, dir)
	core := &cliCore{}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	var stderr bytes.Buffer
	code := runHook([]string{"stop"}, strings.NewReader(input), io.Discard, &stderr, dial, func(k string) string { return env[k] })
	return code, stderr.String(), core, dir
}

var workerEnv = map[string]string{"CONTR_COORDINATOR": "t1", "HC_TERMINAL_ID": "t7"}

// withStateDir is a copy of env with CONTR_STATE_DIR under dir, so the hook
// saves its session file there and not in the real state dir.
func withStateDir(env map[string]string, dir string) map[string]string {
	out := map[string]string{"CONTR_STATE_DIR": filepath.Join(dir, "state")}
	for k, v := range env {
		out[k] = v
	}
	return out
}

func TestHookReportsATurnWithoutReport(t *testing.T) {
	code, stderr, core, dir := hookRun(t, workerEnv, "", hookPrompt, hookWords)
	if code != 0 || len(core.calls) != 1 || core.calls[0] != "terminal.send" {
		t.Fatalf("code %d, stderr %q, calls %v", code, stderr, core.calls)
	}
	p := core.params[0]
	want := "ended its turn without a report. Last words: PR #5 is open. Head abc."
	if p["terminal"] != "t1" || p["caller"] != "t7" || p["text"] != want {
		t.Errorf("params = %v", p)
	}
	// With RESULT.md in the folder, the report names it.
	if err := os.WriteFile(filepath.Join(dir, "RESULT.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := missedReport("done", dir); got != "ended its turn without a report. Last words: done; RESULT.md in "+dir {
		t.Errorf("missedReport = %q", got)
	}
}

func TestHookPrefersLastAssistantMessage(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(tr, []byte(hookPrompt), 0o600); err != nil {
		t.Fatal(err)
	}
	in := `{"transcript_path":"` + tr + `","last_assistant_message":"All done."}`
	_, _, core, _ := hookRun(t, workerEnv, in)
	if len(core.params) != 1 || core.params[0]["text"] != "ended its turn without a report. Last words: All done." {
		t.Errorf("params = %v", core.params)
	}
}

// D216: with no transcript file, the worker still reports its last message
// and RESULT.md, and the report says that it may repeat one.
func TestHookReportsWithoutTranscript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "RESULT.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := `{"transcript_path":"` + filepath.Join(dir, "gone.jsonl") + `","cwd":"` + dir + `","last_assistant_message":"PR #5 is open."}`
	code, stderr, core, _ := hookRun(t, workerEnv, in)
	if code != 0 || len(core.calls) != 1 || core.calls[0] != "terminal.send" {
		t.Fatalf("code %d, stderr %q, calls %v", code, stderr, core.calls)
	}
	want := "ended its turn; it may have sent a report already (no transcript, so contr cannot tell). Last words: PR #5 is open.; RESULT.md in " + dir
	if p := core.params[0]; p["terminal"] != "t1" || p["text"] != want {
		t.Errorf("params = %v", p)
	}
}

func TestHookQuiet(t *testing.T) {
	// A turn that reported sends nothing.
	if code, stderr, core, _ := hookRun(t, workerEnv, "", hookPrompt, hookSend, hookWords); code != 0 || len(core.calls) != 0 {
		t.Errorf("reported: code %d, stderr %q, calls %v", code, stderr, core.calls)
	}
	// Outside hc: nothing is read or sent, even with bad input.
	if code, _, core, _ := hookRun(t, map[string]string{}, "not json"); code != 0 || len(core.calls) != 0 {
		t.Errorf("outside hc: code %d, calls %v", code, core.calls)
	}
}

func TestHookErrors(t *testing.T) {
	if code, stderr, _, _ := hookRun(t, workerEnv, "not json"); code != 1 || !strings.Contains(stderr, "bad hook input") {
		t.Errorf("bad input: code %d, stderr %q", code, stderr)
	}
	// A transcript that exists but cannot be read still fails.
	dir := t.TempDir()
	if code, stderr, core, _ := hookRun(t, workerEnv, `{"transcript_path":"`+dir+`"}`); code != 1 || !strings.Contains(stderr, "contr hook:") || len(core.calls) != 0 {
		t.Errorf("unreadable transcript: code %d, stderr %q, calls %v", code, stderr, core.calls)
	}
	if code, stderr, _, _ := hookRun(t, workerEnv, `{}`); code != 1 || !strings.Contains(stderr, "no transcript_path") {
		t.Errorf("no transcript path: code %d, stderr %q", code, stderr)
	}
	code, stderr, core, _ := func() (int, string, *cliCore, string) {
		dir := t.TempDir()
		tr := filepath.Join(dir, "t.jsonl")
		_ = os.WriteFile(tr, []byte(hookPrompt), 0o600)
		core := &cliCore{errs: map[string]error{"terminal.send": errors.New("not_found")}}
		dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
		var e bytes.Buffer
		env := withStateDir(workerEnv, dir)
		c := runHook([]string{"stop"}, strings.NewReader(`{"transcript_path":"`+tr+`"}`), io.Discard, &e, dial, func(k string) string { return env[k] })
		return c, e.String(), core, dir
	}()
	if code != 1 || !strings.Contains(stderr, "report to t1: not_found") || len(core.calls) != 1 {
		t.Errorf("send error: code %d, stderr %q", code, stderr)
	}
	var e bytes.Buffer
	if code := runHook([]string{"start"}, strings.NewReader(""), io.Discard, &e, nil, func(string) string { return "" }); code != 1 || !strings.Contains(e.String(), "Usage:") {
		t.Errorf("usage: code %d, %q", code, e.String())
	}
}

func TestMissedReportCutsLongWords(t *testing.T) {
	got := missedReport(strings.Repeat("é", saidMax+10), "")
	if !strings.HasSuffix(got, strings.Repeat("é", saidMax)+"…") {
		t.Errorf("missedReport = %q", got)
	}
	if got := missedReport("  ", ""); got != "ended its turn without a report. Last words: (none)" {
		t.Errorf("empty words: %q", got)
	}
}

// The skill line is one line, and it names a skill of the kit.
func TestChatSkillLineNamesTheKitSkill(t *testing.T) {
	if strings.Count(chatSkillLine, "\n") != 1 || !strings.HasSuffix(chatSkillLine, "\n") {
		t.Fatalf("not one line: %q", chatSkillLine)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "kit", "skills", "chat-writing", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nname: chat-writing\n") || !strings.Contains(chatSkillLine, "contr:chat-writing") {
		t.Fatalf("the line %q does not name the skill of the kit", chatSkillLine)
	}
}
