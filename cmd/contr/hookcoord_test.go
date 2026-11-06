package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/owner"
)

const orchestrateLine = `{"type":"user","message":{"content":"<command-name>/contr:orchestrate</command-name>\n<command-args>ship #12</command-args>"}}`

func usageLine(tokens int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"cache_read_input_tokens":%d,"cache_creation_input_tokens":0}}}`, tokens-1)
}

// coordRig runs the coordinator hooks with a transcript, a fake core and
// a scratch TMPDIR for the markers.
type coordRig struct {
	t    *testing.T
	dir  string
	tr   string
	core *cliCore
	env  map[string]string
}

func newCoordRig(t *testing.T, lines ...string) *coordRig {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	r := &coordRig{t: t, dir: t.TempDir(), core: &cliCore{reply: map[string]any{}}}
	r.tr = filepath.Join(r.dir, "t.jsonl")
	if err := os.WriteFile(r.tr, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.env = map[string]string{"HC_TERMINAL_ID": "t1", "CONTR_OWNER_FILE": filepath.Join(r.dir, "owner.json"), "CONTR_AGENTS_CONFIG": filepath.Join(r.dir, "agents.json")}
	return r
}

func (r *coordRig) run(event string, extra map[string]any) (int, string, string) {
	in := map[string]any{"session_id": "s1", "transcript_path": r.tr, "cwd": r.dir}
	for k, v := range extra {
		in[k] = v
	}
	b, _ := json.Marshal(in)
	dial := func(context.Context, func(string) string) (agentsCore, error) { return r.core, nil }
	var out, errb bytes.Buffer
	code := runHook([]string{event}, bytes.NewReader(b), &out, &errb, dial, func(k string) string { return r.env[k] })
	return code, out.String(), errb.String()
}

func TestHookCoordinatorSavesStateOnce(t *testing.T) {
	r := newCoordRig(t, orchestrateLine, usageLine(230000))
	code, out, errs := r.run("stop", nil)
	if code != 0 || !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "230k tokens") || !strings.Contains(out, "COORDINATOR-STATE.md") {
		t.Fatalf("first stop: code %d out %q err %q", code, out, errs)
	}
	// Once per compaction: the next turn ends.
	if _, out, _ := r.run("stop", nil); out != "" {
		t.Errorf("second stop: %q, want nothing", out)
	}
	// After the compaction the marker goes, so the next cycle asks again.
	if code, _, errs := r.run("session-start", map[string]any{"source": "compact"}); code != 0 {
		t.Fatalf("session-start: %d %q", code, errs)
	}
	if _, out, _ := r.run("stop", nil); !strings.Contains(out, "block") {
		t.Errorf("after compaction: %q, want a new ask", out)
	}
	// The transcript decided, so hc was asked only for the worker list (and to label the size).
	if strings.Join(withoutLabels(r.core.calls), " ") != "terminal.list" {
		t.Errorf("calls %v, want only terminal.list", r.core.calls)
	}
}

func TestHookCoordinatorStopQuiet(t *testing.T) {
	// Below the limit.
	r := newCoordRig(t, orchestrateLine, usageLine(150000))
	if _, out, _ := r.run("stop", nil); out != "" {
		t.Errorf("below: %q", out)
	}
	// The limit from CONTR_SAVE_AT.
	r.env["CONTR_SAVE_AT"] = "100000"
	if _, out, _ := r.run("stop", nil); !strings.Contains(out, "block") {
		t.Errorf("CONTR_SAVE_AT: %q", out)
	}
	// stop_hook_active: the turn already goes on because of a hook.
	r = newCoordRig(t, orchestrateLine, usageLine(240000))
	if _, out, _ := r.run("stop", map[string]any{"stop_hook_active": true}); out != "" {
		t.Errorf("stop_hook_active: %q", out)
	}
	// Not a coordinator: no command, and hc says role builder.
	r = newCoordRig(t, usageLine(240000))
	r.core.reply["terminal.get"] = worker("t1", "b1", "live", nil)
	if code, out, errs := r.run("stop", nil); code != 0 || out != "" {
		t.Errorf("builder: code %d out %q err %q", code, out, errs)
	}
}

func TestHookCoordinatorByLabel(t *testing.T) {
	r := newCoordRig(t, usageLine(240000))
	w := worker("t1", "orch", "live", nil)
	w["labels"] = map[string]any{"contr": "agent", "role": "orchestrator"}
	r.core.reply["terminal.get"] = w
	if _, out, _ := r.run("stop", nil); !strings.Contains(out, "block") {
		t.Errorf("orchestrator label: %q, want a block", out)
	}
}

func TestHookSessionStartRestores(t *testing.T) {
	r := newCoordRig(t, orchestrateLine)
	kit, err := filepath.Abs("../../kit")
	if err != nil {
		t.Fatal(err)
	}
	r.env["CLAUDE_PLUGIN_ROOT"] = kit
	mine := worker("t7", "b-12", "live", map[string]any{"state": "working"})
	mine["parent"] = "t1"
	other := worker("t8", "b-99", "live", nil)
	other["parent"] = "t5"
	r.core.reply["terminal.list"] = map[string]any{"terminals": []any{mine, other}}
	store := &owner.Store{Path: r.env["CONTR_OWNER_FILE"]}
	if _, err := store.Ask(owner.Question{Coordinator: "t1", Question: "Ship it?", Options: []string{"yes", "no"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ask(owner.Question{Coordinator: "t5", Question: "Not mine?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pin(owner.Todo{Coordinator: "t1", Kind: "do", Text: "Test the bag"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.env["CONTR_AGENTS_CONFIG"], []byte(`{"flow":{"review":"none","workers":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errs := r.run("session-start", map[string]any{"source": "compact"})
	if code != 0 {
		t.Fatalf("code %d err %q", code, errs)
	}
	for _, want := range []string{
		filepath.Join(r.dir, "COORDINATOR-STATE.md"),
		"- b-12 (t7) builder, working, issue 158, /w/a",
		"Ship it?", "[do] Test the bag",
		"You coordinate worker agents", "Goal: (see the state file)",
		"The flow:\n- Plan first: when needed.", "- Review: none.", "- Workers at once: at most 2,", "- Merge: ask the owner",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("restore text lacks %q:\n%s", want, out)
		}
	}
	for _, not := range []string{"b-99", "Not mine?", "description:", "$ARGUMENTS"} {
		if strings.Contains(out, not) {
			t.Errorf("restore text has %q", not)
		}
	}
	if n := len([]rune(out)); n > restoreMax {
		t.Errorf("restore text is %d characters, more than %d", n, restoreMax)
	}
}

func TestHookSessionStartQuiet(t *testing.T) {
	r := newCoordRig(t, orchestrateLine)
	if _, out, _ := r.run("session-start", map[string]any{"source": "startup"}); out != "" {
		t.Errorf("startup: %q", out)
	}
	r = newCoordRig(t, usageLine(10))
	r.core.reply["terminal.get"] = worker("t1", "b1", "live", nil)
	if _, out, _ := r.run("session-start", map[string]any{"source": "compact"}); out != "" {
		t.Errorf("not a coordinator: %q", out)
	}
}

func withoutLabels(calls []string) []string {
	var out []string
	for _, c := range calls {
		if c != "terminal.label" {
			out = append(out, c)
		}
	}
	return out
}

func TestHookStopLabelsTheContextSize(t *testing.T) {
	r := newCoordRig(t, usageLine(145000))
	if code, out, errs := r.run("stop", nil); code != 0 || out != "" {
		t.Fatalf("stop: %d %q %q", code, out, errs)
	}
	if len(r.core.calls) != 1 || r.core.calls[0] != "terminal.label" || r.core.params[0]["terminal"] != "t1" {
		t.Fatalf("calls %v %v", r.core.calls, r.core.params)
	}
	labels := r.core.params[0]["labels"].(map[string]any)
	if labels["contr.context"] != "145000" || labels["contr.context-limit"] != "250000" {
		t.Errorf("labels %v", labels)
	}
	r.env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] = "300000"
	r.run("stop", nil)
	if l := r.core.params[1]["labels"].(map[string]any); l["contr.context-limit"] != "300000" {
		t.Errorf("limit from the env: %v", l)
	}
	// After a compaction the old size goes.
	r.run("session-start", map[string]any{"source": "compact"})
	if l := r.core.params[2]["labels"].(map[string]any); r.core.calls[2] != "terminal.label" || l["contr.context"] != "" {
		t.Errorf("after compaction: %v %v", r.core.calls, l)
	}
	// hc down: the hook still succeeds.
	r.core.errs = map[string]error{"terminal.label": fmt.Errorf("hc down")}
	if code, _, errs := r.run("stop", nil); code != 0 {
		t.Errorf("hc down: %d %q", code, errs)
	}
}
