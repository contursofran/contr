package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/contursofran/contr/internal/session"
)

// runSessionEnv runs one session hook in terminal t1 with extra env.
func runSessionEnv(t *testing.T, dir, name, input string, extra map[string]string) string {
	t.Helper()
	env := map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_STATE_DIR": dir}
	for k, v := range extra {
		env[k] = v
	}
	var out bytes.Buffer
	if code := runHook([]string{name}, strings.NewReader(input), &out, &out, nil, func(k string) string { return env[k] }); code != 0 {
		t.Fatalf("exit %d", code)
	}
	return out.String()
}

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
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__publish_version","tool_input":{"slug":"a","title":"Board"},"tool_response":[{"type":"text","text":"{\"url\":\"https://x/a\"}"}]}`)
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__get_artifact","tool_input":{"slug":"b"},"tool_response":{"title":"B","url":"https://x/b"}}`)
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__edit_version","tool_input":{"slug":"a"}}`)
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

func TestPostToolArtifactTextBlocks(t *testing.T) {
	dir := t.TempDir()
	// A new publish: no slug in the input, the result only in a text block.
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__publish_version","tool_input":{"content":"<p>x</p>"},"tool_response":{"content":[{"type":"text","text":"{\"slug\":\"new\",\"title\":\"New\",\"url\":\"https://x/new\"}"}]}}`)
	// read_artifact: JSON, then HTML.
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__read_artifact","tool_input":{},"tool_response":[{"type":"text","text":"{\"slug\":\"r\",\"title\":\"R\"}\n<html><body>{x}</body></html>"}]}`)
	// list_artifacts records nothing.
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__list_artifacts","tool_input":{},"tool_response":[{"type":"text","text":"{\"slug\":\"l\"}"}]}`)
	// A slug that is a link is the URL too.
	runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__get_artifact","tool_input":{"slug":"https://claude.ai/artifact/abc"}}`)
	s := readSession(t, dir)
	if len(s.Artifacts) != 3 {
		t.Fatalf("got %+v", s.Artifacts)
	}
	link, r, n := s.Artifacts[0], s.Artifacts[1], s.Artifacts[2]
	if link.Slug != "https://claude.ai/artifact/abc" || link.URL != link.Slug {
		t.Fatalf("link %+v", link)
	}
	if r.Slug != "r" || r.Title != "R" {
		t.Fatalf("read %+v", r)
	}
	if n.Slug != "new" || n.Title != "New" || n.URL != "https://x/new" {
		t.Fatalf("new %+v", n)
	}
}

func TestPromptHookWorker(t *testing.T) {
	if out := runSessionEnv(t, t.TempDir(), "prompt", `{"prompt":"fix the bug"}`, map[string]string{"CONTR_COORDINATOR": "c1"}); out != "" {
		t.Fatalf("worker got %q", out)
	}
}

func TestPostToolParallelKeepsAll(t *testing.T) {
	dir := t.TempDir()
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := fmt.Sprintf(`{"tool_name":"TaskCreate","tool_input":{"subject":"task %d"},"tool_response":{"task":{"id":"%d"}}}`, i, i)
			env := map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_STATE_DIR": dir}
			var out bytes.Buffer
			runHook([]string{"post-tool"}, strings.NewReader(in), &out, &out, nil, func(k string) string { return env[k] })
			if out.Len() != 0 {
				t.Errorf("output %q", out.String())
			}
		}()
	}
	wg.Wait()
	if s := readSession(t, dir); len(s.Todos) != n {
		t.Fatalf("kept %d of %d todos", len(s.Todos), n)
	}
}

func TestPostToolOtherToolNoFiles(t *testing.T) {
	dir := t.TempDir()
	if _, e := runSession(t, dir, "post-tool", `{"tool_name":"mcp__claude-artifacts__guide","tool_input":{}}`); e != "" {
		t.Fatalf("stderr %q", e)
	}
	if _, err := os.Stat(filepath.Join(dir, session.Dir)); !os.IsNotExist(err) {
		t.Fatalf("an unrecorded tool made files: %v", err)
	}
}
