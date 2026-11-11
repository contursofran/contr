package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/session"
)

// runTranscriptHook runs hook name with input and env, with no hc core, and
// returns the exit code, stdout and stderr.
func runTranscriptHook(name, input string, env map[string]string) (int, string, string) {
	var out, errb bytes.Buffer
	code := runHook([]string{name}, strings.NewReader(input), &out, &errb, nil, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

func transcriptInput(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHooksSaveAndUpdateTheTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_STATE_DIR": dir}
	runSession(t, dir, "post-tool", `{"tool_name":"TodoWrite","tool_input":{"todos":[{"content":"Keep me","status":"pending","activeForm":"x"}]}}`)

	steps := []struct {
		hook   string
		fields map[string]any
	}{
		// clear and resume print only the skill line and need no core.
		{"session-start", map[string]any{"source": "clear", "session_id": "s1", "transcript_path": "/x/s1.jsonl"}},
		{"session-start", map[string]any{"source": "resume", "session_id": "s2", "transcript_path": "/x/s2.jsonl"}},
		{"prompt", map[string]any{"prompt": "go on", "session_id": "s3", "transcript_path": "/x/s3.jsonl"}},
		// A coordinator stop with no transcript file does nothing else (D216).
		{"stop", map[string]any{"session_id": "s4", "transcript_path": filepath.Join(dir, "missing.jsonl")}},
	}
	for _, st := range steps {
		code, out, errs := runTranscriptHook(st.hook, transcriptInput(t, st.fields), env)
		if code != 0 || errs != "" {
			t.Fatalf("%s: code %d, stderr %q", st.hook, code, errs)
		}
		if st.hook == "session-start" && out != chatSkillLine {
			t.Errorf("%s %s printed %q", st.hook, st.fields["source"], out)
		}
		s := readSession(t, dir)
		if s.TranscriptPath != st.fields["transcript_path"] || s.SessionID != st.fields["session_id"] {
			t.Fatalf("after %s: path %q, id %q", st.hook, s.TranscriptPath, s.SessionID)
		}
		if len(s.Todos) != 1 || s.Todos[0].Text != "Keep me" {
			t.Fatalf("after %s the todos are %+v", st.hook, s.Todos)
		}
	}
	// No transcript_path in the input keeps the last one.
	if code, _, errs := runTranscriptHook("prompt", `{"prompt":"x"}`, env); code != 0 || errs != "" {
		t.Fatalf("code %d, stderr %q", code, errs)
	}
	if s := readSession(t, dir); s.SessionID != "s4" {
		t.Fatalf("an input with no path changed the file: %+v", s)
	}
}

func TestPromptHookWorkerSavesTheTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	out := runSessionEnv(t, dir, "prompt", `{"prompt":"<hc from=\"c1\">do it</hc>","session_id":"w1","transcript_path":"/x/w1.jsonl"}`,
		map[string]string{"CONTR_COORDINATOR": "c1"})
	if out != "" {
		t.Fatalf("worker got %q", out)
	}
	if s := readSession(t, dir); s.TranscriptPath != "/x/w1.jsonl" || s.SessionID != "w1" {
		t.Fatalf("got %+v", s)
	}
}

func TestHooksSaveNoTranscriptPathOutsideATerminal(t *testing.T) {
	dir := t.TempDir()
	in := `{"source":"resume","prompt":"x","session_id":"s1","transcript_path":"/x/s1.jsonl"}`
	// A worker env with no HC_TERMINAL_ID still runs session-start.
	for _, hook := range []string{"session-start", "prompt"} {
		code, out, errs := runTranscriptHook(hook, in, map[string]string{"CONTR_COORDINATOR": "c1", "CONTR_STATE_DIR": dir})
		if code != 0 || errs != "" {
			t.Fatalf("%s: code %d, stderr %q", hook, code, errs)
		}
		if out != "" {
			t.Errorf("%s outside a terminal printed %q", hook, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, session.Dir)); !os.IsNotExist(err) {
		t.Fatalf("a hook without HC_TERMINAL_ID made files: %v", err)
	}
}

func TestTranscriptSaveErrorDoesNotFailTheHook(t *testing.T) {
	// A file where the state dir should be: the save cannot make its folder.
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_STATE_DIR": bad}
	for _, hook := range []string{"session-start", "prompt"} {
		code, _, errs := runTranscriptHook(hook, `{"source":"clear","prompt":"x","transcript_path":"/x/a.jsonl"}`, env)
		if code != 0 || !strings.Contains(errs, "contr hook "+hook+": save transcript path:") {
			t.Fatalf("%s: code %d, stderr %q", hook, code, errs)
		}
	}
}

func TestKitSessionStartRunsOnResumeAndClear(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "kit", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	got := cfg.Hooks["SessionStart"]
	if len(got) != 1 || got[0].Matcher != "startup|resume|clear|compact" {
		t.Fatalf("SessionStart matchers %+v", got)
	}
}
