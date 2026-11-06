package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// cliCore is a fake core for the command.
type cliCore struct {
	mu     sync.Mutex
	calls  []string
	params []map[string]any
	reply  map[string]any
	errs   map[string]error
	stream *cliStream
	closed bool
}

func (c *cliCore) Call(ctx context.Context, method string, params, result any) error {
	b, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, method)
	c.params = append(c.params, p)
	if err := c.errs[method]; err != nil {
		return err
	}
	if out, ok := c.reply[method]; ok && result != nil {
		b, _ := json.Marshal(out)
		return json.Unmarshal(b, result)
	}
	return nil
}

func (c *cliCore) Subscribe(ctx context.Context, f hc.Filter) (agents.Stream, error) {
	return c.stream, nil
}

func (c *cliCore) Close() error { c.mu.Lock(); defer c.mu.Unlock(); c.closed = true; return nil }

type cliStream struct {
	snap   hc.Snapshot
	events chan hc.Event
}

func (s *cliStream) Snapshot() hc.Snapshot   { return s.snap }
func (s *cliStream) Events() <-chan hc.Event { return s.events }
func (s *cliStream) Err() error              { return nil }
func (s *cliStream) Close() error            { return nil }

var workerLabels = map[string]any{"contr": "agent", "role": "builder", "profile": "builder", "worktree": "/w/a", "issue": "158"}

func worker(addr, name, state string, status any) map[string]any {
	return map[string]any{"address": addr, "name": name, "state": state, "labels": workerLabels, "status": status, "exit_code": 0}
}

// run runs `contr agents args…` with core and env.
func run(t *testing.T, core *cliCore, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["CONTR_AGENTS_CONFIG"]; !ok {
		env["CONTR_AGENTS_CONFIG"] = filepath.Join(t.TempDir(), "none.json")
	}
	var out, errb bytes.Buffer
	dial := func(context.Context, func(string) string) (agentsCore, error) {
		if core == nil {
			return nil, errors.New("no hc")
		}
		return core, nil
	}
	code := runAgents(args, &out, &errb, dial, func(k string) string { return env[k] })
	return code, out.String(), errb.String()
}

func TestAgentsParsesAsASubcommand(t *testing.T) {
	same(t, parse("contr", "agents", "ls", "--all"), Command{Kind: kindAgents, Then: []string{"ls", "--all"}})
	same(t, parse("contr", "--icons", "agents"), Command{Kind: kindUnknown, Unknown: "agents"})
}

func TestAgentsUsageAndArgumentErrors(t *testing.T) {
	if code, out, _ := run(t, &cliCore{}, nil, "help"); code != 0 || !strings.Contains(out, "contr agents spawn") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, _, errs := run(t, &cliCore{}, nil); code != 2 || !strings.Contains(errs, "Usage") {
		t.Errorf("no args: %d %q", code, errs)
	}
	for _, args := range [][]string{{"nope"}, {"send", "w1"}, {"stop"}, {"stop", "a", "b"}, {"ls", "x"}, {"ls", "--bad"}, {"stop", "x", "--profile", "y"}, {"ls", "--name", "z"}} {
		if code, _, _ := run(t, &cliCore{}, nil, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func TestAgentsLs(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.list": map[string]any{"terminals": []any{
		worker("t2", "b-158", "live", map[string]any{"state": "blocked", "kind": "permission"}),
		worker("t3", "r-158", "finished", nil),
	}}}}
	code, out, errs := run(t, core, nil, "ls", "--all")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	for _, want := range []string{"NAME", "b-158", "blocked: permission", "r-158", "ended (0)", "/w/a"} {
		if !strings.Contains(out, want) {
			t.Errorf("table has no %q:\n%s", want, out)
		}
	}
	if core.params[0]["all"] != true || !core.closed {
		t.Errorf("params %v, closed %v", core.params[0], core.closed)
	}

	_, out, _ = run(t, core, nil, "ls", "--json")
	var ws []agents.Worker
	if err := json.Unmarshal([]byte(out), &ws); err != nil || len(ws) != 2 || ws[0].Status != "blocked" {
		t.Errorf("json = %s (%v)", out, err)
	}
}

func TestAgentsSpawn(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b-158", "live", nil)}}
	env := map[string]string{"HC_TERMINAL_ID": "t1"}
	wt := t.TempDir()
	code, out, errs := run(t, core, env, "spawn", "--name", "b-158", "--profile", "builder", "--issue", "158", "--worktree", wt, "read", "TASK.md")
	if code != 0 || out != "b-158 t4\n" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	p := core.params[0]
	argv := p["argv"].([]any)
	if argv[len(argv)-1] != "read TASK.md" || p["parent"] != "t1" || p["cwd"] != wt {
		t.Errorf("params = %v", p)
	}

	core.params = nil
	if code, _, errs := run(t, core, nil, "spawn", "--name", "o", "--profile", "builder", "--role", "orchestrator", "--project", "contr"); code != 0 {
		t.Fatalf("--project: %d %q", code, errs)
	}
	if l := core.params[0]["labels"].(map[string]any); l["project"] != "contr" || l["role"] != "orchestrator" {
		t.Errorf("labels = %v", l)
	}

	core.params = nil
	if code, _, _ := run(t, core, env, "spawn", "--no-parent", "--name", "x", "--profile", "builder"); code != 0 || core.params[0]["no_parent"] != true {
		t.Errorf("--no-parent: %d %v", code, core.params)
	}
	if code, _, errs := run(t, core, nil, "spawn", "--name", "x", "--profile", "nope"); code != 1 || !strings.Contains(errs, "no profile") {
		t.Errorf("unknown profile: %d %q", code, errs)
	}
}

func TestAgentsSendInterruptStopRead(t *testing.T) {
	core := &cliCore{
		reply: map[string]any{"terminal.interrupt": map[string]any{"outcome": "stopped"}, "terminal.read": map[string]any{"text": "line\n\n"}},
		errs:  map[string]error{"terminal.get": &hc.Error{Kind: "not_found", Message: `no terminal "zz"`}},
	}
	if code, _, _ := run(t, core, nil, "send", "w1", "fix", "the", "test"); code != 0 || core.params[0]["text"] != "fix the test" {
		t.Errorf("send: %d %v", code, core.params)
	}
	if code, out, _ := run(t, core, nil, "interrupt", "w1"); code != 0 || out != "stopped\n" {
		t.Errorf("interrupt: %d %q", code, out)
	}
	if code, out, _ := run(t, core, nil, "read", "--lines", "5", "w1"); code != 0 || out != "line\n" {
		t.Errorf("read: %d %q", code, out)
	}
	if code, out, _ := run(t, core, nil, "read", "w1", "--lines", "5"); code != 0 || out != "line\n" || core.params[len(core.params)-1]["lines"] != float64(5) {
		t.Errorf("read with the flag after the name: %d %q", code, out)
	}
	if code, _, errs := run(t, core, nil, "stop", "zz"); code != 1 || !strings.Contains(errs, "not_found") {
		t.Errorf("stop: %d %q", code, errs)
	}
}

func TestAgentsWatchPrintsJSONLines(t *testing.T) {
	s := &cliStream{snap: hc.Snapshot{}, events: make(chan hc.Event, 1)}
	s.events <- hc.Event{Method: "event", Terminal: "t2", Type: "created"}
	close(s.events)
	core := &cliCore{stream: s, reply: map[string]any{"terminal.get": worker("t2", "b-158", "live", map[string]any{"state": "working"})}}
	code, out, errs := run(t, core, nil, "watch")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "[]" || !strings.Contains(lines[1], `"status":"working"`) {
		t.Errorf("out = %q", out)
	}
	// The stream ended without Close, so the command reports it.
	if code != 1 || !strings.Contains(errs, "connection closed") {
		t.Errorf("code %d err %q", code, errs)
	}
}

func TestAgentsReportsConfigAndDialErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, &cliCore{}, map[string]string{"CONTR_AGENTS_CONFIG": path}, "spawn", "--name", "b", "--profile", "builder"); code != 1 || !strings.Contains(errs, "agents.json") {
		t.Errorf("bad config: %d %q", code, errs)
	}
	if code, _, errs := run(t, nil, nil, "ls"); code != 1 || !strings.Contains(errs, "no hc") {
		t.Errorf("dial error: %d %q", code, errs)
	}
}

func TestStateText(t *testing.T) {
	one := 1
	cases := map[string]agents.Worker{
		"ended (1)":     {ExitCode: &one},
		"ended":         {},
		"blocked: auth": {Live: true, Status: "blocked", Kind: "auth"},
		"working":       {Live: true, Status: "working"},
		"-":             {Live: true},
	}
	for want, w := range cases {
		if got := stateText(w); got != want {
			t.Errorf("stateText(%+v) = %q, want %q", w, got, want)
		}
	}
	if !reflect.DeepEqual(stateText(agents.Worker{Live: true, Status: "blocked"}), "blocked") {
		t.Error("blocked without a kind")
	}
}

func TestAgentsSpawnFlagsAfterTheTaskAndDoubleDash(t *testing.T) {
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	if code, out, _ := run(t, core, nil, "spawn", "--name", "b", "fix", "it", "--profile", "builder", "--json"); code != 0 || !strings.Contains(out, `"address": "t4"`) {
		t.Fatalf("flags after the task: %d %q", code, out)
	}
	argv := core.params[0]["argv"].([]any)
	if argv[len(argv)-1] != "fix it" {
		t.Errorf("task = %v", argv[len(argv)-1])
	}
	core.params = nil
	if code, _, _ := run(t, core, nil, "spawn", "--name", "b", "--profile", "builder", "--", "run", "--all", "tests"); code != 0 {
		t.Fatalf("double dash: %d", code)
	}
	argv = core.params[0]["argv"].([]any)
	if argv[len(argv)-1] != "run --all tests" {
		t.Errorf("task after -- = %v", argv[len(argv)-1])
	}
}

func TestAgentsBrokenConfigBlocksOnlySpawn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CONTR_AGENTS_CONFIG": path}
	if code, _, errs := run(t, &cliCore{reply: map[string]any{"terminal.get": worker("t1", "w1", "live", nil)}}, env, "stop", "w1"); code != 0 {
		t.Errorf("stop with a broken config: %d %q", code, errs)
	}
	if code, _, _ := run(t, &cliCore{}, env, "spawn", "--name", "b", "--profile", "builder"); code != 1 {
		t.Errorf("spawn with a broken config: %d, want 1", code)
	}
}

func TestAgentsKitFromTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	for _, kit := range []string{"file-kit", "env-kit"} {
		if err := os.MkdirAll(filepath.Join(dir, kit, "agents"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, kit, "agents", "builder.md"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(path, []byte(`{"kit":"`+filepath.Join(dir, "file-kit")+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	core := &cliCore{reply: map[string]any{"terminal.run": worker("t4", "b", "live", nil)}}
	env := map[string]string{"CONTR_AGENTS_CONFIG": path, "CONTR_KIT": filepath.Join(dir, "env-kit")}
	if code, _, errs := run(t, core, env, "spawn", "--name", "b", "--profile", "builder"); code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if argv := core.params[0]["argv"].([]any); argv[2] != filepath.Join(dir, "env-kit") {
		t.Errorf("argv = %v, want the CONTR_KIT folder", argv)
	}
}

func TestListShowsPlainTerminal(t *testing.T) {
	labels := map[string]any{"contr": "agent", "role": "terminal", "worktree": "/w/a"}
	core := &cliCore{reply: map[string]any{"terminal.list": map[string]any{"terminals": []any{
		map[string]any{"address": "t5", "name": "term-0101", "state": "live", "labels": labels},
	}}}}
	code, out, errs := run(t, core, nil, "ls")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	for _, want := range []string{"term-0101", "terminal", "/w/a"} {
		if !strings.Contains(out, want) {
			t.Errorf("table has no %q:\n%s", want, out)
		}
	}
}

// --review-pr needs a repository; outside one, spawn fails before hc.
func TestAgentsSpawnReviewPROutsideARepository(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	core := &cliCore{}
	code, _, errs := run(t, core, nil, "spawn", "--name", "r", "--profile", "reviewer", "--review-pr", "3", "--worktree", t.TempDir())
	if code != 1 || !strings.Contains(errs, "git rev-parse") || len(core.calls) != 0 {
		t.Errorf("code %d err %q calls %v", code, errs, core.calls)
	}
}

func TestAgentsStopRemovesUnlessKept(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		state string
		want  []string
	}{
		{[]string{"stop", "w1"}, "live", []string{"terminal.get", "terminal.kill", "terminal.rm"}},
		{[]string{"stop", "w1", "--keep"}, "live", []string{"terminal.get", "terminal.kill"}},
		{[]string{"stop", "w1"}, "finished", []string{"terminal.get", "terminal.rm"}},
		{[]string{"rm", "w1"}, "finished", []string{"terminal.rm"}},
	} {
		core := &cliCore{reply: map[string]any{"terminal.get": worker("t1", "w1", tc.state, nil)}}
		if code, _, errs := run(t, core, nil, tc.args...); code != 0 || !slices.Equal(core.calls, tc.want) {
			t.Errorf("%v (%s): %d %q calls %v, want %v", tc.args, tc.state, code, errs, core.calls, tc.want)
		}
	}
}

func TestAgentsRmEnded(t *testing.T) {
	mine := func(addr, name, state string) map[string]any {
		w := worker(addr, name, state, nil)
		w["parent"] = "t1"
		return w
	}
	other := worker("t8", "theirs", "finished", nil)
	other["parent"] = "t2"
	core := &cliCore{reply: map[string]any{"terminal.list": map[string]any{"terminals": []any{
		mine("t4", "done-1", "finished"), mine("t5", "busy", "live"), other,
	}}}}
	code, out, errs := run(t, core, map[string]string{"HC_TERMINAL_ID": "t1"}, "rm", "--ended")
	if code != 0 || out != "done-1\n" || !slices.Equal(core.calls, []string{"terminal.list", "terminal.rm"}) || core.params[1]["terminal"] != "t4" {
		t.Errorf("rm --ended: %d %q %q calls %v", code, out, errs, core.calls)
	}
	// Outside hc nothing is removed.
	core = &cliCore{reply: core.reply}
	if code, _, _ := run(t, core, nil, "rm", "--ended"); code != 0 || slices.Contains(core.calls, "terminal.rm") {
		t.Errorf("rm --ended outside hc: %d %v", code, core.calls)
	}
	for _, args := range [][]string{{"rm"}, {"rm", "a", "--ended"}, {"rm", "a", "b"}, {"stop", "a", "--ended"}, {"rm", "a", "--keep"}} {
		if code, _, _ := run(t, &cliCore{}, nil, args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
}

func TestAgentsFlowPrintsTheFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"flow":{"plan":"always","merge":"auto"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CONTR_AGENTS_CONFIG": path}
	code, out, _ := run(t, &cliCore{}, env, "flow")
	if code != 0 || !strings.Contains(out, "The flow:\n- Plan first: always.") || !strings.Contains(out, "Merge: merge yourself") {
		t.Fatalf("code %d out %q", code, out)
	}
	if code, out, _ := run(t, &cliCore{}, env, "help"); code != 0 || !strings.Contains(out, "Plan first: always.") {
		t.Fatalf("help: code %d out %q", code, out)
	}
	if code, _, _ := run(t, &cliCore{}, env, "flow", "x"); code != 2 {
		t.Fatalf("flow x: code %d", code)
	}
	if err := os.WriteFile(path, []byte(`{"flow":{"plan":"sometimes"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, &cliCore{}, env, "flow"); code != 1 || !strings.Contains(errs, "sometimes") {
		t.Fatalf("bad flow: code %d err %q", code, errs)
	}
	if code, out, _ := run(t, &cliCore{}, env, "help"); code != 0 || !strings.Contains(out, "could not be read") || !strings.Contains(out, "Plan first: when needed.") {
		t.Fatalf("help with a bad flow: code %d out %q", code, out)
	}
}
