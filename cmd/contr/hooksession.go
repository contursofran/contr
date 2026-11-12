// contr hook post-tool and contr hook prompt: the agent's todos and
// artifacts, one session file per hc terminal (D195), and the transcript
// path of the terminal's session (D243).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/session"
	"github.com/contursofran/contr/internal/statedir"
)

// artifactsPrefix starts the tool names of the artifacts MCP server.
const artifactsPrefix = "mcp__claude-artifacts__"

// artifactTools are the artifacts tools (name after artifactsPrefix) that
// record an artifact. Other artifacts tools record nothing.
var artifactTools = map[string]bool{
	"publish_version": true, "publish_files": true, "edit_version": true,
	"read_artifact": true, "get_artifact": true, "create_from_type": true,
	"read_file": true, "list_files": true,
}

// youContext is the context the prompt hook gives Claude.
const youContext = "contr: before you start, add each new request in this prompt to your todo list (TodoWrite or TaskCreate). Start the text of each such item with \"" + session.YouPrefix + "\"."

// toolInput is the part of the PostToolUse input that post-tool reads.
type toolInput struct {
	ToolName     string          `json:"tool_name"`
	ToolInput    json.RawMessage `json:"tool_input"`
	ToolResponse json.RawMessage `json:"tool_response"`
}

// runSessionHook is `contr hook post-tool` and `contr hook prompt`. It always
// returns 0: these hooks never block Claude Code. Errors go to stderr.
func runSessionHook(name string, stdin io.Reader, stdout, stderr io.Writer, dial func(context.Context, func(string) string) (agentsCore, error), getenv func(string) string, stateDir string, now time.Time) int {
	id := getenv(statedir.EnvTerminal)
	if id == "" {
		return 0
	}
	// A worker is never a coordinator: it gets no flow context.
	worker := getenv("CONTR_COORDINATOR") != ""
	flow := ""
	if !worker {
		var err error
		if flow, err = flowChange(getenv, dial, stateDir, id); err != nil {
			fmt.Fprintf(stderr, "contr hook %s: flow: %v\n", name, err)
		}
	}
	var err error
	if name == "prompt" {
		// A worker's prompts come from the coordinator, not the owner: it gets
		// no context text, but its transcript path is saved.
		err = promptHook(stdin, stdout, stderr, stateDir, id, worker, flow)
	} else {
		err = postToolHook(stdin, stateDir, id, uint64(now.Unix()))
		if err == nil && flow != "" {
			err = writeContext(stdout, "PostToolUse", flow)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "contr hook %s: %v\n", name, err)
	}
	return 0
}

// writeContext prints additionalContext for the hook event.
func writeContext(stdout io.Writer, event, text string) error {
	out := map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     event,
		"additionalContext": text,
	}}
	return json.NewEncoder(stdout).Encode(out)
}

func promptHook(stdin io.Reader, stdout, stderr io.Writer, stateDir, id string, worker bool, flow string) error {
	var in struct {
		Prompt         string `json:"prompt"`
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("bad hook input: %w", err)
	}
	saveTranscript(stderr, "prompt", stateDir, id, in.SessionID, in.TranscriptPath)
	if worker {
		return nil
	}
	text := youContext
	if strings.HasPrefix(strings.TrimSpace(in.Prompt), "<hc from=") || strings.TrimSpace(in.Prompt) == "" {
		text = ""
	}
	if flow != "" {
		text = strings.TrimLeft(text+"\n\n"+flow, "\n")
	}
	if text == "" {
		return nil
	}
	return writeContext(stdout, "UserPromptSubmit", text)
}

func postToolHook(stdin io.Reader, stateDir, id string, now uint64) error {
	var in toolInput
	if err := json.NewDecoder(stdin).Decode(&in); err != nil {
		return fmt.Errorf("bad hook input: %w", err)
	}
	if !recordedTool(in.ToolName) {
		return nil
	}
	// One locked update: hooks of parallel tool calls may run at the same time.
	return session.Update(stateDir, id, func(s *session.Session) (bool, error) {
		var changed bool
		var err error
		switch {
		case in.ToolName == "TodoWrite":
			changed, err = todoWrite(s, in.ToolInput)
		case in.ToolName == "TaskCreate":
			changed, err = taskCreate(s, in.ToolInput, in.ToolResponse)
		case in.ToolName == "TaskUpdate":
			changed, err = taskUpdate(s, in.ToolInput)
		default:
			changed = artifactCall(s, in.ToolInput, in.ToolResponse, now)
		}
		if err == nil && changed {
			s.UpdatedAt = now
		}
		return changed, err
	})
}

// saveTranscript saves the transcript path and the session id of a hook
// input in the session file of terminal id (D243). It writes only when one of
// them changed. An error goes to stderr and never fails the hook.
func saveTranscript(stderr io.Writer, hook, stateDir, id, sessionID, path string) {
	if id == "" || path == "" {
		return
	}
	err := session.Update(stateDir, id, func(s *session.Session) (bool, error) {
		return s.SetTranscript(sessionID, path), nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "contr hook %s: save transcript path: %v\n", hook, err)
	}
}

// recordedTool reports whether post-tool records calls of the tool name.
func recordedTool(name string) bool {
	switch name {
	case "TodoWrite", "TaskCreate", "TaskUpdate":
		return true
	}
	return strings.HasPrefix(name, artifactsPrefix) && artifactTools[strings.TrimPrefix(name, artifactsPrefix)]
}

func todoWrite(s *session.Session, raw json.RawMessage) (bool, error) {
	var in struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return false, fmt.Errorf("TodoWrite input: %w", err)
	}
	s.Todos = s.Todos[:0]
	for _, t := range in.Todos {
		s.Todos = append(s.Todos, session.NewTodo(t.Content, t.ActiveForm, t.Status))
	}
	return true, nil
}

// taskFields are the TaskCreate and TaskUpdate input fields contr reads.
type taskFields struct {
	TaskID     string `json:"taskId"`
	Subject    string `json:"subject"`
	ActiveForm string `json:"activeForm"`
	Status     string `json:"status"`
}

func taskCreate(s *session.Session, raw, resp json.RawMessage) (bool, error) {
	var in taskFields
	if err := json.Unmarshal(raw, &in); err != nil {
		return false, fmt.Errorf("TaskCreate input: %w", err)
	}
	var out struct {
		Task struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	_ = json.Unmarshal(resp, &out) // the id is optional
	status := in.Status
	if status == "" {
		status = session.Pending
	}
	t := session.NewTodo(in.Subject, in.ActiveForm, status)
	t.ID = out.Task.ID
	if t.ID != "" {
		for i := range s.Todos {
			if s.Todos[i].ID == t.ID {
				s.Todos[i] = t
				return true, nil
			}
		}
	}
	s.Todos = append(s.Todos, t)
	return true, nil
}

func taskUpdate(s *session.Session, raw json.RawMessage) (bool, error) {
	var in taskFields
	if err := json.Unmarshal(raw, &in); err != nil {
		return false, fmt.Errorf("TaskUpdate input: %w", err)
	}
	for i := range s.Todos {
		t := &s.Todos[i]
		if in.TaskID == "" || t.ID != in.TaskID {
			continue
		}
		if in.Status == "deleted" {
			s.Todos = append(s.Todos[:i], s.Todos[i+1:]...)
			return true, nil
		}
		next := session.NewTodo(in.Subject, in.ActiveForm, in.Status)
		if in.Subject != "" {
			t.Text = next.Text
		}
		if in.ActiveForm != "" {
			t.Active = next.Active
		}
		if in.Status != "" {
			t.Status = in.Status
		}
		t.You = t.You || next.You
		return true, nil
	}
	return false, nil
}

// artifactCall records the slug, title and URL found in the input and the
// response of an artifacts tool call. A call with no slug changes nothing.
func artifactCall(s *session.Session, raw, resp json.RawMessage, now uint64) bool {
	a := session.Artifact{UpdatedAt: now}
	for _, m := range []json.RawMessage{raw, resp} {
		var v any
		if json.Unmarshal(m, &v) == nil {
			findArtifact(v, &a, 0)
		}
	}
	if a.Slug == "" {
		return false
	}
	if a.URL == "" && strings.HasPrefix(a.Slug, "https://") {
		// The slug input may be a claude.ai link.
		a.URL = a.Slug
	}
	s.AddArtifact(a)
	return true
}

// findArtifact fills empty fields of a from the first "slug", "title" and
// "url" keys in v. MCP text content is read too: the first JSON value at the
// start of a text is decoded and the text after it is ignored.
func findArtifact(v any, a *session.Artifact, depth int) {
	if depth > 6 {
		return
	}
	switch x := v.(type) {
	case map[string]any:
		for key, dst := range map[string]*string{"slug": &a.Slug, "title": &a.Title, "url": &a.URL} {
			if s, ok := x[key].(string); ok && *dst == "" {
				*dst = s
			}
		}
		for _, sub := range x {
			findArtifact(sub, a, depth+1)
		}
	case []any:
		for _, sub := range x {
			findArtifact(sub, a, depth+1)
		}
	case string:
		if t := strings.TrimSpace(x); strings.HasPrefix(t, "{") {
			var inner any
			if json.NewDecoder(strings.NewReader(t)).Decode(&inner) == nil {
				findArtifact(inner, a, depth+1)
			}
		}
	}
}

// sessionStateDir is statedir.Dir, read through getenv so tests can set
// CONTR_STATE_DIR.
func sessionStateDir(getenv func(string) string) string {
	home, _ := os.UserHomeDir()
	return statedir.From(getenv, home, runtime.GOOS == "darwin")
}
