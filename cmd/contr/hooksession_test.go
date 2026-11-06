package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/session"
)

// runSession runs one session hook in terminal t1 with a temp state dir.
func runSession(t *testing.T, dir, name, input string) (string, string) {
	t.Helper()
	env := map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_STATE_DIR": dir}
	var out, errOut bytes.Buffer
	code := runHook([]string{name}, strings.NewReader(input), &out, &errOut, nil, func(k string) string { return env[k] })
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	return out.String(), errOut.String()
}

func readSession(t *testing.T, dir string) *session.Session {
	t.Helper()
	s, err := session.Read(dir, "t1")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPostToolTodoWrite(t *testing.T) {
	dir := t.TempDir()
	runSession(t, dir, "post-tool", `{"tool_name":"TodoWrite","tool_input":{"todos":[
		{"content":"[you] Design the panel","status":"in_progress","activeForm":"Designing the panel"},
		{"content":"Write tests","status":"pending","activeForm":"Writing tests"}]}}`)
	s := readSession(t, dir)
	if len(s.Todos) != 2 || !s.Todos[0].You || s.Todos[0].Text != "Design the panel" || s.Todos[0].Active != "Designing the panel" || s.Todos[1].You || s.UpdatedAt == 0 {
		t.Fatalf("got %+v", s)
	}
	runSession(t, dir, "post-tool", `{"tool_name":"TodoWrite","tool_input":{"todos":[{"content":"Only","status":"completed","activeForm":"x"}]}}`)
	if s := readSession(t, dir); len(s.Todos) != 1 || s.Todos[0].Status != "completed" {
		t.Fatalf("not replaced: %+v", s.Todos)
	}
}

func TestPostToolTasks(t *testing.T) {
	dir := t.TempDir()
	runSession(t, dir, "post-tool", `{"tool_name":"TaskCreate","tool_input":{"subject":"[you] Fix it","activeForm":"Fixing it"},"tool_response":{"task":{"id":"1"}}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"TaskCreate","tool_input":{"subject":"Two"},"tool_response":{"task":{"id":"2"}}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"TaskUpdate","tool_input":{"taskId":"1","status":"in_progress"}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"TaskUpdate","tool_input":{"taskId":"2","status":"deleted"}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"TaskUpdate","tool_input":{"taskId":"9","status":"completed"}}`)
	s := readSession(t, dir)
	if len(s.Todos) != 1 || s.Todos[0].ID != "1" || !s.Todos[0].You || s.Todos[0].Text != "Fix it" || s.Todos[0].Status != "in_progress" {
		t.Fatalf("got %+v", s.Todos)
	}
}

func TestPostToolArtifacts(t *testing.T) {
	dir := t.TempDir()
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__publish","tool_input":{"slug":"a","title":"Board"},"tool_response":[{"type":"text","text":"{\"url\":\"https://x/a\"}"}]}`)
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__read","tool_input":{"slug":"b"},"tool_response":{"title":"B","url":"https://x/b"}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__edit","tool_input":{"slug":"a"}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__guide","tool_input":{}}`)
	s := readSession(t, dir)
	if len(s.Artifacts) != 2 || s.Artifacts[0].Slug != "a" || s.Artifacts[0].Title != "Board" || s.Artifacts[0].URL != "https://x/a" || s.Artifacts[1].URL != "https://x/b" {
		t.Fatalf("got %+v", s.Artifacts)
	}
}

func TestPostToolUnknownAndBad(t *testing.T) {
	dir := t.TempDir()
	if _, e := runSession(t, dir, "post-tool", `{"tool_name":"Bash","tool_input":{}}`); e != "" {
		t.Fatalf("stderr %q", e)
	}
	if s := readSession(t, dir); s.UpdatedAt != 0 {
		t.Fatal("unknown tool wrote a file")
	}
	if _, e := runSession(t, dir, "post-tool", `{`); !strings.Contains(e, "bad hook input") {
		t.Fatalf("stderr %q", e)
	}
	if _, e := runSession(t, dir, "post-tool", `{"tool_name":"TodoWrite","tool_input":{"todos":3}}`); !strings.Contains(e, "TodoWrite") {
		t.Fatalf("stderr %q", e)
	}
	// Outside hc: nothing, exit 0.
	var out bytes.Buffer
	if runHook([]string{"post-tool"}, strings.NewReader(`{`), &out, &out, nil, func(string) string { return "" }) != 0 || out.Len() != 0 {
		t.Fatal("ran outside hc")
	}
}

func TestPromptHook(t *testing.T) {
	dir := t.TempDir()
	out, _ := runSession(t, dir, "prompt", `{"prompt":"fix the bug"}`)
	var v struct {
		H struct {
			Event   string `json:"hookEventName"`
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || v.H.Event != "UserPromptSubmit" || !strings.Contains(v.H.Context, `"[you] "`) {
		t.Fatalf("got %q", out)
	}
	if out, _ := runSession(t, dir, "prompt", `{"prompt":"<hc from=\"t2\">done</hc>"}`); out != "" {
		t.Fatalf("hc report got %q", out)
	}
	if _, e := runSession(t, dir, "prompt", `nope`); !strings.Contains(e, "bad hook input") {
		t.Fatalf("stderr %q", e)
	}
}
