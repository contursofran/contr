package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// mcpRun sends the lines to `contr mcp` and returns the replies by id.
func mcpRun(t *testing.T, dial func(context.Context, func(string) string) (agentsCore, error), env map[string]string, lines ...string) (int, map[string]map[string]any) {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["CONTR_AGENTS_CONFIG"]; !ok {
		env["CONTR_AGENTS_CONFIG"] = filepath.Join(t.TempDir(), "none.json")
	}
	if _, ok := env["CONTR_OWNER_FILE"]; !ok {
		env["CONTR_OWNER_FILE"] = filepath.Join(t.TempDir(), "owner.json")
	}
	var out, errb bytes.Buffer
	code := runMCP(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, &errb, dial, func(k string) string { return env[k] })
	replies := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad reply %q (stderr %q)", line, errb.String())
		}
		id, _ := json.Marshal(m["id"])
		replies[string(id)] = m
	}
	return code, replies
}

func callLine(id int, tool string, args any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	return string(b)
}

func toolText(t *testing.T, r map[string]any) (string, bool) {
	t.Helper()
	res := r["result"].(map[string]any)
	isErr, _ := res["isError"].(bool)
	return res["content"].([]any)[0].(map[string]any)["text"].(string), isErr
}

func TestMCPParsesAsASubcommand(t *testing.T) {
	same(t, parse("contr", "mcp"), Command{Kind: kindMCP})
	same(t, parse("contr", "mcp", "x"), Command{Kind: kindUnknown, Unknown: "mcp"})
}

func TestMCPListsAndRunsTheAgentTools(t *testing.T) {
	core := &cliCore{
		reply: map[string]any{
			"terminal.run":       worker("t4", "b-160", "live", nil),
			"terminal.list":      map[string]any{"terminals": []any{worker("t4", "b-160", "live", map[string]any{"state": "working"})}},
			"terminal.interrupt": map[string]any{"outcome": "stopped"},
			"terminal.read":      map[string]any{"text": "screen"},
		},
		errs: map[string]error{"terminal.get": &hc.Error{Kind: "not_found", Message: `no terminal "zz"`}},
	}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	wt := t.TempDir()
	code, r := mcpRun(t, dial, map[string]string{"HC_TERMINAL_ID": "t1"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		callLine(3, "spawn", map[string]any{"name": "b-160", "profile": "builder", "task": "read TASK.md", "issue": "160", "worktree": wt}),
		callLine(4, "message", map[string]any{"name": "b-160", "text": "fix it"}),
		callLine(5, "interrupt", map[string]any{"name": "b-160"}),
		callLine(6, "read", map[string]any{"name": "b-160", "lines": 20}),
		callLine(7, "list", map[string]any{"all": true}),
		callLine(8, "stop", map[string]any{"name": "zz"}),
		callLine(9, "spawn", map[string]any{"name": "x", "profile": "nope", "task": "t"}),
	)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var names []string
	for _, tool := range r["2"]["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	if !strings.HasPrefix(strings.Join(names, " "), "spawn message interrupt stop read prompt list ") {
		t.Errorf("tools = %v", names)
	}
	if got, isErr := toolText(t, r["3"]); isErr || !strings.Contains(got, `"address": "t4"`) {
		t.Errorf("spawn = %q, %v", got, isErr)
	}
	byMethod := map[string]map[string]any{}
	for i, method := range core.calls {
		byMethod[method] = core.params[i]
	}
	if p := byMethod["terminal.run"]; p["parent"] != "t1" || p["cwd"] != wt {
		t.Errorf("run params = %v", p)
	}
	if p := byMethod["terminal.send"]; p["text"] != "fix it" {
		t.Errorf("send params = %v", p)
	}
	if p := byMethod["terminal.read"]; p["lines"] != float64(20) {
		t.Errorf("read params = %v", p)
	}
	for id, want := range map[string]string{"4": "sent", "5": "stopped", "6": "screen"} {
		if got, isErr := toolText(t, r[id]); got != want || isErr {
			t.Errorf("reply %s = %q, %v; want %q", id, got, isErr, want)
		}
	}
	if got, _ := toolText(t, r["7"]); !strings.Contains(got, `"status": "working"`) {
		t.Errorf("list = %q", got)
	}
	if got, isErr := toolText(t, r["8"]); !isErr || !strings.Contains(got, "not_found") {
		t.Errorf("stop error = %q, %v", got, isErr)
	}
	if got, isErr := toolText(t, r["9"]); !isErr || !strings.Contains(got, "no profile") {
		t.Errorf("unknown profile = %q, %v", got, isErr)
	}
}

// TestMCPMessageNeedsItsText: a misnamed argument (message for text) and a
// blank text are tool errors, and nothing is typed into the worker (#483).
func TestMCPMessageNeedsItsText(t *testing.T) {
	core := &cliCore{}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	_, r := mcpRun(t, dial, nil,
		callLine(1, "message", map[string]any{"name": "w", "message": "fix it"}),
		callLine(2, "message", map[string]any{"name": "w", "text": "  "}),
	)
	if got, isErr := toolText(t, r["1"]); !isErr || !strings.Contains(got, `unknown field "message"`) {
		t.Errorf("misnamed = %q, %v", got, isErr)
	}
	if got, isErr := toolText(t, r["2"]); !isErr || !strings.Contains(got, "no text") {
		t.Errorf("blank = %q, %v", got, isErr)
	}
	for _, m := range core.calls {
		if m == "terminal.send" {
			t.Errorf("calls = %v", core.calls)
		}
	}
}

func TestMCPConnectsAgainAfterTheCoreGoesAway(t *testing.T) {
	dials := 0
	gone := &cliCore{errs: map[string]error{"terminal.send": hc.ErrClosed}}
	fresh := &cliCore{}
	dial := func(context.Context, func(string) string) (agentsCore, error) {
		dials++
		switch dials {
		case 1:
			return nil, errors.New("no hc yet")
		case 2:
			return gone, nil
		default:
			return fresh, nil
		}
	}
	_, r := mcpRun(t, dial, nil,
		callLine(1, "message", map[string]any{"name": "w", "text": "a"}), // dial fails
	)
	if got, isErr := toolText(t, r["1"]); !isErr || !strings.Contains(got, "no hc yet") {
		t.Errorf("dial error = %q, %v", got, isErr)
	}
	// Calls run at the same time, so send them one server run at a time.
	m := &managers{dial: dial, getenv: func(string) string { return "" }}
	tools := agentTools(m, "")
	send := tools[1].Run
	if _, err := send(context.Background(), json.RawMessage(`{"name":"w","text":"b"}`)); !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if !gone.closed {
		t.Error("the ended connection was not closed")
	}
	if out, err := send(context.Background(), json.RawMessage(`{"name":"w","text":"c"}`)); err != nil || out != "sent" {
		t.Errorf("after reconnect: %q, %v", out, err)
	}
	if dials != 3 || len(fresh.calls) != 1 {
		t.Errorf("dials %d, fresh calls %d", dials, len(fresh.calls))
	}
	if _, err := send(context.Background(), json.RawMessage(`not json`)); err == nil {
		t.Error("bad arguments: no error")
	}
}

// A call that fails on an old connection must not close the new one that
// another call made in the meantime.
func TestMCPCheckClosesOnlyTheConnectionItUsed(t *testing.T) {
	old, fresh := &cliCore{}, &cliCore{}
	n := 0
	m := &managers{getenv: func(string) string { return "" }, dial: func(context.Context, func(string) string) (agentsCore, error) {
		n++
		if n == 1 {
			return old, nil
		}
		return fresh, nil
	}}
	_, usedA, _ := m.get(context.Background())
	_, usedB, _ := m.get(context.Background()) // a parallel call on the same old connection
	_ = m.check(usedA, hc.ErrClosed)           // the first failure drops old
	_, usedC, _ := m.get(context.Background()) // a third call connects fresh
	_ = m.check(usedB, hc.ErrClosed)           // the second failure comes late
	if usedC != fresh || fresh.closed || m.core != fresh {
		t.Errorf("the late failure closed the new connection: closed %v, core is fresh %v", fresh.closed, m.core == fresh)
	}
	if !old.closed {
		t.Error("the old connection is not closed")
	}
}

func TestMCPOwnerTools(t *testing.T) {
	file := filepath.Join(t.TempDir(), "owner.json")
	env := map[string]string{"CONTR_OWNER_FILE": file, "CONTR_PROJECT": "contr", "HC_TERMINAL_ID": "t1"}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return &cliCore{}, nil }
	// Calls run at the same time, so each step is its own server run.
	step := func(line string) (string, bool) {
		t.Helper()
		_, r := mcpRun(t, dial, env, line)
		for _, reply := range r {
			return toolText(t, reply)
		}
		t.Fatal("no reply")
		return "", false
	}
	if id, isErr := step(callLine(1, "ask_owner", map[string]any{"question": "Which layout?", "options": []string{"A", "B"}, "recommended": 1, "title": "Layout", "impacts": []string{"quick", "slow"}, "refs": []string{"PR 12"}, "source": "worker report"})); id != "D1" || isErr {
		t.Fatalf("ask = %q, %v", id, isErr)
	}
	if id, _ := step(callLine(2, "pin_for_owner", map[string]any{"text": "Test the build", "from": "#160"})); id != "T1" {
		t.Fatalf("pin = %q", id)
	}
	if got, isErr := step(callLine(3, "pin_for_owner", map[string]any{"text": "x", "kind": "maybe"})); !isErr || !strings.Contains(got, "kind") {
		t.Errorf("bad kind = %q, %v", got, isErr)
	}
	items, _ := step(callLine(4, "owner_items", map[string]any{}))
	var open struct {
		Questions []map[string]any `json:"questions"`
		Todos     []map[string]any `json:"todos"`
	}
	if err := json.Unmarshal([]byte(items), &open); err != nil || len(open.Questions) != 1 || len(open.Todos) != 1 {
		t.Fatalf("items = %s (%v)", items, err)
	}
	q := open.Questions[0]
	if q["source"] != "worker report" || len(q["impacts"].([]any)) != 2 || q["refs"].([]any)[0] != "PR 12" {
		t.Errorf("impacts, refs, source = %v", q)
	}
	if q["coordinator"] != "t1" || q["project"] != "contr" || q["recommended"] != float64(1) {
		t.Errorf("question = %v", q)
	}
	if got, isErr := step(callLine(5, "decided", map[string]any{"ids": []string{"D1"}, "answer": "B", "by": "owner"})); got != "decided" || isErr {
		t.Errorf("decided = %q, %v", got, isErr)
	}
	if got, isErr := step(callLine(6, "decided", map[string]any{"ids": []string{"D1"}, "answer": "A", "by": "owner"})); !isErr {
		t.Errorf("deciding twice = %q", got)
	}
	if got, _ := step(callLine(7, "unpin_for_owner", map[string]any{"ids": []string{"T1"}})); got != "unpinned" {
		t.Errorf("unpin = %q", got)
	}
	items, _ = step(callLine(8, "owner_items", map[string]any{}))
	if !strings.Contains(items, `"questions": null`) || !strings.Contains(items, `"todos": null`) {
		t.Errorf("items after closing = %s", items)
	}
	// Another project's item shows only with all.
	env["CONTR_PROJECT"] = "aerum"
	step(callLine(9, "pin_for_owner", map[string]any{"text": "other"}))
	env["CONTR_PROJECT"] = "contr"
	if items, _ := step(callLine(10, "owner_items", map[string]any{})); strings.Contains(items, "other") {
		t.Errorf("another project's item shows: %s", items)
	}
	if items, _ := step(callLine(11, "owner_items", map[string]any{"all": true})); !strings.Contains(items, "other") {
		t.Errorf("all misses another project's item: %s", items)
	}
}

func TestMCPListsTheOwnerTools(t *testing.T) {
	env := map[string]string{"CONTR_OWNER_FILE": filepath.Join(t.TempDir(), "o.json")}
	_, r := mcpRun(t, func(context.Context, func(string) string) (agentsCore, error) { return &cliCore{}, nil }, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	var names []string
	for _, tool := range r["1"]["result"].(map[string]any)["tools"].([]any) {
		tm := tool.(map[string]any)
		names = append(names, tm["name"].(string))
		if tm["name"] == "ask_owner" {
			opts := tm["inputSchema"].(map[string]any)["properties"].(map[string]any)["options"].(map[string]any)
			if opts["type"] != "array" || opts["items"].(map[string]any)["type"] != "string" {
				t.Errorf("options schema = %v", opts)
			}
		}
	}
	if strings.Join(names, " ") != "spawn message interrupt stop read prompt list clear_ended ask_owner decided pin_for_owner unpin_for_owner owner_items" {
		t.Errorf("tools = %v", names)
	}
}

// TestMCPRetriesACallThatWasNotSent: a call on a connection that had already
// ended reaches no core, so the tool connects again and runs it once more;
// the caller sees no error. A call that ended while it waited for a reply
// (plain ErrClosed) is not retried.
func TestMCPRetriesACallThatWasNotSent(t *testing.T) {
	dials := 0
	cores := []*cliCore{
		{errs: map[string]error{"terminal.send": hc.ErrNotSent}},
		{},
		{errs: map[string]error{"terminal.send": hc.ErrClosed}},
		{},
	}
	dial := func(context.Context, func(string) string) (agentsCore, error) {
		dials++
		return cores[dials-1], nil
	}
	m := &managers{dial: dial, getenv: func(string) string { return "" }}
	send := agentTools(m, "")[1].Run
	if out, err := send(context.Background(), json.RawMessage(`{"name":"w","text":"a"}`)); err != nil || out != "sent" {
		t.Fatalf("not sent: %q, %v; want a retry that works", out, err)
	}
	if dials != 2 || !cores[0].closed || len(cores[1].calls) != 1 {
		t.Errorf("dials %d, first closed %v, second calls %v", dials, cores[0].closed, cores[1].calls)
	}
	// The second connection ends while a send waits: no retry.
	m.close()
	if _, err := send(context.Background(), json.RawMessage(`{"name":"w","text":"b"}`)); !errors.Is(err, hc.ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if dials != 3 || len(cores[3].calls) != 0 {
		t.Errorf("a sent call was retried: dials %d", dials)
	}
}

// TestMCPSpawnReadsProfilesEachTime: a profile added to the profiles file
// while the server runs works on the next spawn (#257).
func TestMCPSpawnReadsProfilesEachTime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agents.json")
	env := map[string]string{"CONTR_AGENTS_CONFIG": file}
	getenv := func(k string) string { return env[k] }
	load := func() (agents.Config, error) { return agents.LoadConfig(configPath(getenv)) }
	cfg, _ := load()
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "p", "live", nil)}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	m := &managers{dial: dial, getenv: getenv, cfg: cfg, load: load}
	spawn := agentTools(m, "")[0].Run
	args := json.RawMessage(`{"name":"p","profile":"planner-high","task":"plan"}`)
	if _, err := spawn(context.Background(), args); err == nil || !strings.Contains(err.Error(), `no profile "planner-high"`) {
		t.Fatalf("before: %v", err)
	}
	if err := os.WriteFile(file, []byte(`{"profiles":{"planner-high":{"argv":["claude","--effort","high"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := spawn(context.Background(), args); err != nil {
		t.Fatalf("after the profile was added: %v", err)
	}
	// A broken file fails the spawn with its error, not the other tools.
	if err := os.WriteFile(file, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := spawn(context.Background(), args); err == nil || !strings.Contains(err.Error(), "agents.json") {
		t.Errorf("broken file: %v", err)
	}
	if _, err := agentTools(m, "")[1].Run(context.Background(), json.RawMessage(`{"name":"w","text":"x"}`)); err != nil {
		t.Errorf("message with a broken file: %v", err)
	}
}

func TestMCPStopKeepAndClearEnded(t *testing.T) {
	done := worker("t4", "done-1", "finished", nil)
	done["parent"] = "t1"
	core := &cliCore{reply: map[string]any{
		"terminal.get":  worker("t4", "done-1", "live", nil),
		"terminal.list": map[string]any{"terminals": []any{done}},
	}}
	dial := func(context.Context, func(string) string) (agentsCore, error) { return core, nil }
	_, r := mcpRun(t, dial, map[string]string{"HC_TERMINAL_ID": "t1"},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		callLine(2, "stop", map[string]any{"name": "done-1"}),
		callLine(3, "stop", map[string]any{"name": "done-1", "keep": true}),
		callLine(4, "clear_ended", map[string]any{}),
	)
	// The tools may run in any order, so count the calls: stop twice kills
	// twice but removes once (keep); clear_ended lists once and removes once.
	count := map[string]int{}
	for _, c := range core.calls {
		count[c]++
	}
	if want := map[string]int{"terminal.get": 2, "terminal.kill": 2, "terminal.rm": 2, "terminal.list": 1}; !reflect.DeepEqual(count, want) {
		t.Errorf("calls = %v, want %v", count, want)
	}
	if got, isErr := toolText(t, r["4"]); isErr || !strings.Contains(got, "done-1") {
		t.Errorf("clear_ended = %q, %v", got, isErr)
	}
}
