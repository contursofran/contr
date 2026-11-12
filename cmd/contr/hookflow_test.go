package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// flowRig runs the session hooks of terminal t1 with a scratch agents.json,
// state folder and a fake core.
type flowRig struct {
	t    *testing.T
	cfg  string
	core *cliCore
	env  map[string]string
}

func newFlowRig(t *testing.T, coordinator bool) *flowRig {
	t.Helper()
	dir := t.TempDir()
	w := worker("t1", "orch", "live", nil)
	if coordinator {
		w["labels"] = map[string]any{"contr": "agent", "contr.orchestrating": "1"}
	}
	r := &flowRig{t: t, cfg: filepath.Join(dir, "agents.json"), core: &cliCore{reply: map[string]any{"terminal.get": w}}}
	r.env = map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_AGENTS_CONFIG": r.cfg, "CONTR_STATE_DIR": filepath.Join(dir, "state"), "CONTR_OWNER_FILE": filepath.Join(dir, "owner.json")}
	r.write(`{"profiles":{"builder":{"argv":["claude","--model","opus","--effort","low"]}}}`)
	return r
}

func (r *flowRig) write(s string) {
	r.t.Helper()
	if err := os.WriteFile(r.cfg, []byte(s), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *flowRig) run(event, input string) (string, string) {
	r.t.Helper()
	dial := func(context.Context, func(string) string) (agentsCore, error) { return r.core, nil }
	var out, errb bytes.Buffer
	if code := runHook([]string{event}, strings.NewReader(input), &out, &errb, dial, func(k string) string { return r.env[k] }); code != 0 {
		r.t.Fatalf("%s: code %d err %q", event, code, errb.String())
	}
	return out.String(), errb.String()
}

func (r *flowRig) start() {
	r.t.Helper()
	if _, errs := r.run("session-start", `{"source":"startup","session_id":"s1"}`); errs != "" {
		r.t.Fatalf("start: %q", errs)
	}
	r.core.calls = nil
}

const flowPost = `{"tool_name":"Bash"}`
const flowPrompt = `{"prompt":"go on","session_id":"s1"}`

func TestFlowNoChangeSaysNothing(t *testing.T) {
	r := newFlowRig(t, true)
	r.start()
	if out, _ := r.run("post-tool", flowPost); out != "" {
		t.Errorf("post-tool: %q, want nothing", out)
	}
	if out, _ := r.run("prompt", flowPrompt); strings.Contains(out, "changed the flow") {
		t.Errorf("prompt: %q, want no flow", out)
	}
	if len(r.core.calls) != 0 {
		t.Errorf("calls %v, want none", r.core.calls)
	}
}

func TestFlowChangeReachesCoordinator(t *testing.T) {
	r := newFlowRig(t, true)
	r.start()
	r.write(`{"profiles":{"builder":{"argv":["claude","--model","opus","--effort","low"]}},"flow":{"workers":7}}`)
	out, errs := r.run("post-tool", flowPost)
	if !strings.Contains(out, `"hookEventName":"PostToolUse"`) || !strings.Contains(out, "contr: the owner changed the flow and models in agents.json; follow these from now on.") || !strings.Contains(out, "builder: model opus, effort low") {
		t.Fatalf("post-tool: %q err %q", out, errs)
	}
	// Once: the next call says nothing.
	if out, _ := r.run("post-tool", flowPost); out != "" {
		t.Errorf("second post-tool: %q", out)
	}
}

func TestFlowModelChangeReachesPrompt(t *testing.T) {
	r := newFlowRig(t, true)
	r.start()
	r.write(`{"profiles":{"builder":{"argv":["claude","--model","sonnet","--effort","high"]}}}`)
	out, _ := r.run("prompt", flowPrompt)
	if !strings.Contains(out, `"hookEventName":"UserPromptSubmit"`) || !strings.Contains(out, "builder: model sonnet, effort high") || !strings.Contains(out, "todo list") {
		t.Fatalf("prompt: %q", out)
	}
}

func TestFlowChangeSkipsOtherTerminals(t *testing.T) {
	r := newFlowRig(t, false)
	r.start()
	r.write(`{"profiles":{"builder":{"argv":["claude","--model","sonnet"]}}}`)
	if out, _ := r.run("post-tool", flowPost); out != "" {
		t.Errorf("post-tool: %q, want nothing", out)
	}
	r.env["CONTR_COORDINATOR"] = "t0"
	r.write(`{"profiles":{"builder":{"argv":["claude","--model","haiku"]}}}`)
	r.core.calls = nil
	if out, _ := r.run("post-tool", flowPost); out != "" || len(r.core.calls) != 0 {
		t.Errorf("worker: %q calls %v, want nothing", out, r.core.calls)
	}
}

func TestFlowBadFileIsLogged(t *testing.T) {
	r := newFlowRig(t, true)
	r.start()
	r.write(`{`)
	out, errs := r.run("post-tool", flowPost)
	if out != "" || !strings.Contains(errs, "contr hook post-tool: flow:") {
		t.Errorf("bad file: out %q err %q", out, errs)
	}
	// The prompt context still comes.
	if out, _ := r.run("prompt", flowPrompt); !strings.Contains(out, "todo list") {
		t.Errorf("prompt: %q", out)
	}
}
