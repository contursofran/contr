package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

type call struct {
	Method string
	Params map[string]any
}

// fakeCore answers calls with handlers and records them.
type fakeCore struct {
	mu       sync.Mutex
	calls    []call
	handle   map[string]func(map[string]any) (any, error)
	stream   *fakeStream
	subErr   error
	filterIn hc.Filter
}

func (f *fakeCore) Call(ctx context.Context, method string, params, result any) error {
	b, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	f.mu.Lock()
	f.calls = append(f.calls, call{method, p})
	h := f.handle[method]
	f.mu.Unlock()
	if h == nil {
		return nil
	}
	out, err := h(p)
	if err != nil || result == nil {
		return err
	}
	b, _ = json.Marshal(out)
	return json.Unmarshal(b, result)
}

func (f *fakeCore) Subscribe(ctx context.Context, flt hc.Filter) (Stream, error) {
	f.filterIn = flt
	if f.subErr != nil {
		return nil, f.subErr
	}
	return f.stream, nil
}

func (f *fakeCore) last() call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

type fakeStream struct {
	snap   hc.Snapshot
	events chan hc.Event
	err    error
	closed bool
}

func (s *fakeStream) Snapshot() hc.Snapshot   { return s.snap }
func (s *fakeStream) Events() <-chan hc.Event { return s.events }
func (s *fakeStream) Err() error              { return s.err }
func (s *fakeStream) Close() error            { s.closed = true; return nil }

// term is an hc terminal as JSON.
func term(addr, name, state string, labels map[string]string, status map[string]any) map[string]any {
	t := map[string]any{"address": addr, "name": name, "state": state, "labels": labels, "turn": 2, "title": "t"}
	if status != nil {
		t["status"] = status
	}
	return t
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

var builderLabels = map[string]string{LabelApp: AppAgent, LabelRole: "builder", LabelProfile: "builder", LabelWorktree: "/w/a", LabelIssue: "158"}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadConfig(filepath.Join(dir, "missing.json"))
	if err != nil || !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Fatalf("missing file: %v, %+v", err, cfg)
	}

	path := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(path, []byte(`{"profiles":{"builder":{"argv":["opencode"]},"luna":{"argv":["codex","-m","luna"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles["builder"].Argv; !reflect.DeepEqual(got, []string{"opencode"}) {
		t.Errorf("builder = %v", got)
	}
	if _, ok := cfg.Profiles["luna"]; !ok {
		t.Error("luna missing")
	}
	if _, ok := cfg.Profiles["reviewer"]; !ok {
		t.Error("default reviewer dropped")
	}

	for _, bad := range []string{`{"profiles":{"x":{"argv":[]}}}`, `{not json`} {
		if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

func TestSpawn(t *testing.T) {
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(p map[string]any) (any, error) {
			labels := map[string]string{}
			for k, v := range p["labels"].(map[string]any) {
				labels[k] = v.(string)
			}
			return term("t7", p["name"].(string), "live", labels, map[string]any{"state": "blocked", "kind": "permission", "msg": "allow?"}), nil
		},
	}}
	m := New(f, DefaultConfig())
	wt := t.TempDir()
	w, err := m.Spawn(context.Background(), Spec{Name: "b-158", Profile: "builder", Issue: "158", Worktree: wt, Task: "read TASK.md"})
	if err != nil {
		t.Fatal(err)
	}
	want := Worker{Name: "b-158", Address: "t7", Role: "builder", Issue: "158", Profile: "builder", Worktree: wt, Live: true, Status: "blocked", Kind: "permission", Msg: "allow?", Turn: 2, Title: "t"}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("worker\n got %+v\nwant %+v", w, want)
	}
	p := f.last().Params
	if got := p["argv"]; !reflect.DeepEqual(got, []any{"claude", "--model", "sonnet", "--effort", "medium", "--permission-mode", "auto", "read TASK.md"}) {
		t.Errorf("argv = %v", got)
	}
	if p["cwd"] != wt || p["env"] != nil || p["no_parent"] != true || p["outlive_parent"] != true || p["wait"] != false || p["parent"] != nil {
		t.Errorf("params = %v", p)
	}

	if _, err := m.Spawn(context.Background(), Spec{Name: "r", Profile: "reviewer", Parent: "t1"}); err != nil {
		t.Fatal(err)
	}
	p = f.last().Params
	if p["parent"] != "t1" || p["no_parent"] != nil || p["cwd"] != nil || !reflect.DeepEqual(p["env"], map[string]any{EnvCoordinator: "t1"}) {
		t.Errorf("parent params = %v", p)
	}
	if l := p["labels"].(map[string]any); l[LabelRole] != "reviewer" || l[LabelIssue] != nil {
		t.Errorf("labels = %v", l)
	}
	if got := len(p["argv"].([]any)); got != 7 {
		t.Errorf("argv without a task has %d words", got)
	}

	if _, err := m.Spawn(context.Background(), Spec{Name: "x", Profile: "nope"}); err == nil {
		t.Error("unknown profile: no error")
	}
	if _, err := m.Spawn(context.Background(), Spec{Profile: "builder"}); err == nil {
		t.Error("no name: no error")
	}
	f.handle["terminal.run"] = func(map[string]any) (any, error) { return nil, &hc.Error{Kind: "name_taken"} }
	if _, err := m.Spawn(context.Background(), Spec{Name: "b", Profile: "builder"}); !hc.IsKind(err, "name_taken") {
		t.Errorf("err = %v, want name_taken", err)
	}
}

func TestListSendInterruptStopRead(t *testing.T) {
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.list": func(map[string]any) (any, error) {
			return map[string]any{"terminals": []any{
				term("t9", "zeta", "finished", builderLabels, nil),
				term("t3", "alpha", "live", builderLabels, map[string]any{"state": "working"}),
			}}, nil
		},
		"terminal.interrupt": func(map[string]any) (any, error) { return map[string]any{"outcome": "stopped", "key": "esc"}, nil },
		"terminal.read":      func(map[string]any) (any, error) { return map[string]any{"text": "screen"}, nil },
		"terminal.send":      func(map[string]any) (any, error) { return nil, &hc.Error{Kind: "blocked"} },
	}}
	m := New(f, DefaultConfig())
	ctx := context.Background()

	ws, err := m.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 || ws[0].Name != "alpha" || ws[0].Status != "working" || ws[1].Live {
		t.Errorf("list = %+v", ws)
	}
	if p := f.last().Params; p["all"] != true || !reflect.DeepEqual(p["labels"], map[string]any{LabelApp: AppAgent}) {
		t.Errorf("list params = %v", p)
	}

	if err := m.Send(ctx, "alpha", "hi"); !hc.IsKind(err, "blocked") {
		t.Errorf("send err = %v", err)
	}
	if p := f.last().Params; p["terminal"] != "alpha" || p["text"] != "hi" {
		t.Errorf("send params = %v", p)
	}
	if out, err := m.Interrupt(ctx, "alpha"); err != nil || out != "stopped" {
		t.Errorf("interrupt = %q, %v", out, err)
	}
	if err := m.Stop(ctx, "alpha"); err != nil || f.last().Method != "terminal.kill" {
		t.Errorf("stop: %v, %s", err, f.last().Method)
	}
	if err := m.Remove(ctx, "alpha"); err != nil || f.last().Method != "terminal.rm" || f.last().Params["terminal"] != "alpha" {
		t.Errorf("remove: %v, %+v", err, f.last())
	}
	if txt, err := m.Read(ctx, "alpha", 40); err != nil || txt != "screen" || f.last().Params["lines"] != float64(40) {
		t.Errorf("read = %q, %v, %v", txt, err, f.last().Params)
	}
	if _, err := m.Read(ctx, "alpha", 0); err != nil || f.last().Params["lines"] != nil {
		t.Errorf("read all: %v, %v", err, f.last().Params)
	}
}

// TestControlCallsNameTheCaller: inside hc, stop, read and interrupt name
// this terminal as the caller, so hc counts a child's news as seen by its
// parent and types no exit note after a stop. Outside hc they name nobody.
func TestControlCallsNameTheCaller(t *testing.T) {
	for _, id := range []string{"t4", ""} {
		f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
			"terminal.interrupt": func(map[string]any) (any, error) { return map[string]any{"outcome": "stopped"}, nil },
			"terminal.read":      func(map[string]any) (any, error) { return map[string]any{"text": "screen"}, nil },
		}}
		m := New(f, DefaultConfig())
		m.getenv = func(k string) string {
			if k == "HC_TERMINAL_ID" {
				return id
			}
			return ""
		}
		ctx := context.Background()
		calls := []func() error{
			func() error { return m.Stop(ctx, "alpha") },
			func() error { _, err := m.Read(ctx, "alpha", 0); return err },
			func() error { _, err := m.Interrupt(ctx, "alpha"); return err },
		}
		for _, call := range calls {
			if err := call(); err != nil {
				t.Fatal(err)
			}
			c := f.last()
			got, has := c.Params["caller"]
			if id != "" && got != id {
				t.Errorf("HC_TERMINAL_ID %q: %s caller = %v, want %q", id, c.Method, got, id)
			}
			if id == "" && has {
				t.Errorf("outside hc: %s has caller %v", c.Method, got)
			}
			if c.Params["terminal"] != "alpha" {
				t.Errorf("%s params = %v", c.Method, c.Params)
			}
		}
	}
}

func TestWatchFollowsEvents(t *testing.T) {
	gets := map[string]any{
		"t2": term("t2", "beta", "live", builderLabels, map[string]any{"state": "working"}),
	}
	s := &fakeStream{
		snap:   hc.Snapshot{Seq: 1, Terminals: []json.RawMessage{raw(term("t1", "alpha", "live", builderLabels, nil))}},
		events: make(chan hc.Event, 16),
	}
	f := &fakeCore{stream: s, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(p map[string]any) (any, error) {
			if t, ok := gets[p["terminal"].(string)]; ok {
				return t, nil
			}
			return nil, &hc.Error{Kind: "not_found"}
		},
	}}
	m := New(f, DefaultConfig())

	var lists [][]Worker
	names := func(ws []Worker) []string {
		out := []string{}
		for _, w := range ws {
			out = append(out, w.Name+":"+w.Status)
		}
		return out
	}
	s.events <- hc.Event{Method: "event", Terminal: "t2", Type: "created"} // added
	s.events <- hc.Event{Method: "heartbeat"}                              // nothing
	s.events <- hc.Event{Method: "event", Terminal: "t2", Type: "turn"}    // same data: no call to fn
	s.events <- hc.Event{Method: "event", Terminal: "t1", Type: "status"}  // get: not_found, dropped
	s.events <- hc.Event{Method: "missed", Data: raw(hc.Snapshot{Seq: 9, Terminals: []json.RawMessage{raw(term("t5", "gamma", "live", builderLabels, nil))}})}
	s.events <- hc.Event{Method: "event", Terminal: "t5", Type: "removed"} // dropped
	s.err = errors.New("core gone")
	close(s.events)

	err := m.Watch(context.Background(), func(ws []Worker) { lists = append(lists, ws) })
	if err == nil || err.Error() != "core gone" {
		t.Fatalf("err = %v, want core gone", err)
	}
	if !s.closed {
		t.Error("stream not closed")
	}
	if !reflect.DeepEqual(f.filterIn.Labels, map[string]string{LabelApp: AppAgent}) {
		t.Errorf("filter = %+v", f.filterIn)
	}
	var got [][]string
	for _, l := range lists {
		got = append(got, names(l))
	}
	want := [][]string{
		{"alpha:"},
		{"alpha:", "beta:working"},
		{"beta:working"},
		{"gamma:"},
		{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lists\n got %v\nwant %v", got, want)
	}
}

func TestWatchStopsOnContextAndReportsErrors(t *testing.T) {
	s := &fakeStream{events: make(chan hc.Event)}
	m := New(&fakeCore{stream: s}, DefaultConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	if err := m.Watch(ctx, func([]Worker) { calls++ }); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
	if calls != 1 {
		t.Errorf("fn ran %d times, want 1 (the snapshot)", calls)
	}

	want := errors.New("no core")
	if err := New(&fakeCore{subErr: want}, DefaultConfig()).Watch(context.Background(), func([]Worker) {}); !errors.Is(err, want) {
		t.Errorf("subscribe err = %v", err)
	}

	ended := &fakeStream{events: make(chan hc.Event)}
	close(ended.events)
	if err := New(&fakeCore{stream: ended}, DefaultConfig()).Watch(context.Background(), func([]Worker) {}); !errors.Is(err, hc.ErrClosed) {
		t.Errorf("clean end err = %v, want ErrClosed", err)
	}

	bad := &fakeStream{events: make(chan hc.Event, 1)}
	bad.events <- hc.Event{Method: "event", Terminal: "t1", Type: "status"}
	fc := &fakeCore{stream: bad, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(map[string]any) (any, error) { return nil, &hc.Error{Kind: "unavailable"} },
	}}
	if err := New(fc, DefaultConfig()).Watch(context.Background(), func([]Worker) {}); !hc.IsKind(err, "unavailable") {
		t.Errorf("get err = %v, want unavailable", err)
	}
}

func TestWatchDropsATerminalThatIsNoLongerAWorker(t *testing.T) {
	agentLabels := true
	s := &fakeStream{
		snap:   hc.Snapshot{Terminals: []json.RawMessage{raw(term("t1", "alpha", "live", builderLabels, nil))}},
		events: make(chan hc.Event, 4),
	}
	f := &fakeCore{stream: s, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(p map[string]any) (any, error) {
			if p["terminal"] == "t3" || !agentLabels {
				return term(p["terminal"].(string), "other", "live", map[string]string{LabelRole: "builder"}, nil), nil
			}
			return term("t1", "alpha", "live", builderLabels, nil), nil
		},
	}}
	s.events <- hc.Event{Method: "event", Terminal: "t3", Type: "created"} // not a worker: no change
	s.events <- hc.Event{Method: "event", Terminal: "t1", Type: "labels"}  // contr=agent removed
	s.events <- hc.Event{Method: "missed", Data: json.RawMessage(`{"seq":"x"}`)}
	agentLabels = false

	var lists []int
	err := New(f, DefaultConfig()).Watch(context.Background(), func(ws []Worker) { lists = append(lists, len(ws)) })
	if err == nil || !strings.Contains(err.Error(), "agents: bad missed snapshot") {
		t.Fatalf("err = %v, want a wrapped snapshot error", err)
	}
	if !reflect.DeepEqual(lists, []int{1, 0}) {
		t.Errorf("list sizes = %v, want [1 0]", lists)
	}
}

func TestSpawnStartsTheKitAgent(t *testing.T) {
	kit := t.TempDir()
	if err := os.MkdirAll(filepath.Join(kit, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kit, "agents", "builder.md"), []byte("---\nname: builder\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(map[string]any) (any, error) { return term("t1", "w", "live", builderLabels, nil), nil },
	}}
	cfg := DefaultConfig()
	cfg.Kit = kit
	cfg.Profiles["shell"] = Profile{Argv: []string{"sh"}}
	m := New(f, cfg)
	argvOf := func(profile string) []any {
		t.Helper()
		if _, err := m.Spawn(context.Background(), Spec{Name: "w", Profile: profile}); err != nil {
			t.Fatal(err)
		}
		return f.last().Params["argv"].([]any)
	}
	want := []any{"claude", "--plugin-dir", kit, "--agent", "contr:builder", "--model", "sonnet", "--effort", "medium", "--permission-mode", "auto"}
	if got := argvOf("builder"); !reflect.DeepEqual(got, want) {
		t.Errorf("builder argv\n got %v\nwant %v", got, want)
	}
	if got := argvOf("reviewer"); got[1] != "--model" {
		t.Errorf("a profile with no agent file gets the kit: %v", got)
	}
	if got := argvOf("shell"); !reflect.DeepEqual(got, []any{"sh"}) {
		t.Errorf("a non-claude profile changed: %v", got)
	}
	if got := New(f, DefaultConfig()).agentArgv("builder", []string{"claude"}); !reflect.DeepEqual(got, []string{"claude"}) {
		t.Errorf("no kit: %v", got)
	}
}

func TestLoadConfigReadsTheKit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`{"kit":"/k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || cfg.Kit != "/k" || len(cfg.Profiles) != 4 {
		t.Errorf("cfg = %+v, %v", cfg, err)
	}
}

func TestNewMakesARelativeKitAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join("kit", "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("kit", "agents", "builder.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Kit = "kit"
	got := New(&fakeCore{}, cfg).agentArgv("builder", []string{"claude"})
	abs, _ := filepath.Abs("kit")
	if len(got) < 3 || got[2] != abs || !filepath.IsAbs(got[2]) {
		t.Errorf("argv = %v, want the kit as %s", got, abs)
	}
}

func TestWorkerDecodesProjectLabel(t *testing.T) {
	labels := map[string]string{LabelApp: AppAgent, LabelProject: "x"}
	w, err := decodeWorker(raw(term("t1", "a", "live", labels, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if w.Project != "x" {
		t.Errorf("Project = %q, want x", w.Project)
	}
}

func TestSpawnSetsProjectLabel(t *testing.T) {
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(p map[string]any) (any, error) { return term("t1", "a", "live", nil, nil), nil },
	}}
	m := New(f, DefaultConfig())
	if _, err := m.Spawn(context.Background(), Spec{Name: "a", Profile: "builder", Project: "contr"}); err != nil {
		t.Fatal(err)
	}
	if l := f.last().Params["labels"].(map[string]any); l[LabelProject] != "contr" {
		t.Errorf("labels = %v", l)
	}
	if _, err := m.Spawn(context.Background(), Spec{Name: "b", Profile: "builder"}); err != nil {
		t.Fatal(err)
	}
	if l := f.last().Params["labels"].(map[string]any); l[LabelProject] != nil {
		t.Errorf("empty project sets a label: %v", l)
	}
}

func TestSpawnRejectsBadProject(t *testing.T) {
	f := &fakeCore{}
	m := New(f, DefaultConfig())
	if _, err := m.Spawn(context.Background(), Spec{Name: "a", Profile: "builder", Project: "a\nb"}); err == nil {
		t.Fatal("no error for a project with a newline")
	}
	if len(f.calls) != 0 {
		t.Errorf("terminal.run called: %v", f.calls)
	}
}

func TestOrchestratorByRole(t *testing.T) {
	for role, want := range map[string]bool{RoleOrchestrator: true, "builder": false, "": false} {
		if got := (Worker{Role: role}).Orchestrator(); got != want {
			t.Errorf("role %q: Orchestrator() = %v, want %v", role, got, want)
		}
	}
}

func TestWorkerCommand(t *testing.T) {
	labels := map[string]string{LabelApp: AppAgent}
	cmd := func(extra map[string]any) string {
		tm := term("t1", "a", "live", labels, nil)
		for k, v := range extra {
			tm[k] = v
		}
		w, err := decodeWorker(raw(tm))
		if err != nil {
			t.Fatal(err)
		}
		return w.Command
	}
	argv := map[string]any{"argv": []string{"/usr/bin/claude", "--model", "x"}}
	if got := cmd(argv); got != "claude" {
		t.Errorf("argv fallback = %q, want claude", got)
	}
	withFg := map[string]any{"argv": argv["argv"], "foreground": map[string]any{"name": "node"}}
	if got := cmd(withFg); got != "node" {
		t.Errorf("foreground = %q, want node", got)
	}
	if got := cmd(map[string]any{"foreground": nil}); got != "" {
		t.Errorf("nothing known = %q, want empty", got)
	}
}

func TestWatchStampsLastOutput(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	gets := map[string]any{
		"t1": term("t1", "a", "live", builderLabels, nil),
		"t2": term("t2", "b", "live", builderLabels, nil),
	}
	s := &fakeStream{
		snap:   hc.Snapshot{Seq: 1, Terminals: []json.RawMessage{raw(gets["t1"]), raw(gets["t2"])}},
		events: make(chan hc.Event, 4),
	}
	f := &fakeCore{stream: s, handle: map[string]func(map[string]any) (any, error){
		"terminal.get": func(p map[string]any) (any, error) { return gets[p["terminal"].(string)], nil },
	}}
	m := New(f, DefaultConfig())
	ticks := 0
	m.now = func() time.Time { ticks++; return t0.Add(time.Duration(ticks-1) * time.Minute) } // one minute per event

	s.events <- hc.Event{Method: "event", Terminal: "t1", Type: "output"}
	gets["t2"] = term("t2", "b", "live", builderLabels, map[string]any{"state": "working"})
	s.events <- hc.Event{Method: "event", Terminal: "t2", Type: "status"}
	close(s.events)

	var last []Worker
	_ = m.Watch(context.Background(), func(ws []Worker) { last = ws })
	if len(last) != 2 {
		t.Fatalf("workers = %v", last)
	}
	if !last[0].LastOutput.Equal(t0) {
		t.Errorf("a.LastOutput = %v, want %v", last[0].LastOutput, t0)
	}
	if !last[1].LastOutput.Equal(t0.Add(time.Minute)) {
		t.Errorf("b.LastOutput = %v, want %v", last[1].LastOutput, t0.Add(time.Minute))
	}
}

func TestSendKeysParams(t *testing.T) {
	f := &fakeCore{}
	m := New(f, DefaultConfig())
	if err := m.SendKeys(context.Background(), "w1", []byte("1")); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0].Method != "terminal.keys" {
		t.Fatalf("calls = %+v, want one terminal.keys", f.calls)
	}
	want := map[string]any{"terminal": "w1", "keys": []any{"1"}}
	if !reflect.DeepEqual(f.calls[0].Params, want) {
		t.Errorf("params = %v, want %v", f.calls[0].Params, want)
	}
}

func TestSendKeysNames(t *testing.T) {
	f := &fakeCore{}
	m := New(f, DefaultConfig())
	in := "2 \r\t\x1b\x1b[B\x1bOA\x7f\x03A"
	if err := m.SendKeys(context.Background(), "w1", []byte(in)); err != nil {
		t.Fatal(err)
	}
	want := []any{"2", "space", "enter", "tab", "esc", "down", "up", "backspace", "ctrl+c", "A"}
	if got := f.calls[0].Params["keys"]; !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
}

func TestSendKeysEmpty(t *testing.T) {
	f := &fakeCore{}
	if err := New(f, DefaultConfig()).SendKeys(context.Background(), "w1", nil); err == nil {
		t.Error("no error for empty keys")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %+v, want none", f.calls)
	}
}

func TestSendKeysUnnamedByte(t *testing.T) {
	f := &fakeCore{}
	if err := New(f, DefaultConfig()).SendKeys(context.Background(), "w1", []byte("1\x00")); err == nil {
		t.Error("no error for a byte with no name")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %+v, want none: nothing is sent when a byte is bad", f.calls)
	}
}

func TestSendKeysClosed(t *testing.T) {
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.keys": func(map[string]any) (any, error) { return nil, hc.ErrClosed },
	}}
	if err := New(f, DefaultConfig()).SendKeys(context.Background(), "w1", []byte("1")); !errors.Is(err, hc.ErrClosed) {
		t.Errorf("err = %v, want ErrClosed", err)
	}
}

func openTermCore() *fakeCore {
	return &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(p map[string]any) (any, error) {
			return term("t9", "term-1", "live", map[string]string{LabelApp: AppAgent, LabelRole: RoleTerminal}, nil), nil
		},
	}}
}

func TestOpenTerminalParams(t *testing.T) {
	f := openTermCore()
	m := New(f, DefaultConfig())
	m.getenv = func(string) string { return "/bin/fish" }
	w, err := m.OpenTerminal(context.Background(), TerminalSpec{Name: "term-1", Project: "contr", Dir: "/w/a"})
	if err != nil {
		t.Fatal(err)
	}
	if !w.Plain() || w.Address != "t9" {
		t.Errorf("worker = %+v", w)
	}
	c := f.last()
	if c.Method != "terminal.run" {
		t.Fatalf("method = %q", c.Method)
	}
	want := map[string]any{
		"argv":           []any{"/bin/fish", "-l"},
		"name":           "term-1",
		"labels":         map[string]any{"contr": "agent", "role": "terminal", "project": "contr", "worktree": "/w/a"},
		"cwd":            "/w/a",
		"wait":           false,
		"outlive_parent": true,
		"no_parent":      true,
	}
	if !reflect.DeepEqual(c.Params, want) {
		t.Errorf("params = %v\nwant     %v", c.Params, want)
	}
}

func TestOpenTerminalNoProject(t *testing.T) {
	f := openTermCore()
	m := New(f, DefaultConfig())
	if _, err := m.OpenTerminal(context.Background(), TerminalSpec{Name: "term-1", Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	p := f.last().Params
	if _, ok := p["cwd"]; ok {
		t.Errorf("empty dir sets cwd: %v", p)
	}
	if l := p["labels"].(map[string]any); l[LabelProject] != nil {
		t.Errorf("empty project sets a label: %v", l)
	}
	if got := p["argv"].([]any)[0]; got != "/bin/sh" {
		t.Errorf("shell = %v, want /bin/sh", got)
	}
}

func TestOpenTerminalShellFallback(t *testing.T) {
	f := openTermCore()
	m := New(f, DefaultConfig())
	m.getenv = func(string) string { return "" }
	if _, err := m.OpenTerminal(context.Background(), TerminalSpec{Name: "term-1"}); err != nil {
		t.Fatal(err)
	}
	if got := f.last().Params["argv"].([]any); !reflect.DeepEqual(got, []any{"/bin/zsh", "-l"}) {
		t.Errorf("argv = %v", got)
	}
}

func TestOpenTerminalErrors(t *testing.T) {
	f := openTermCore()
	m := New(f, DefaultConfig())
	if _, err := m.OpenTerminal(context.Background(), TerminalSpec{}); err == nil || !strings.Contains(err.Error(), "needs a name") {
		t.Errorf("empty name: %v", err)
	}
	if _, err := m.OpenTerminal(context.Background(), TerminalSpec{Name: "a", Project: "a\nb"}); err == nil {
		t.Error("no error for a project with a newline")
	}
	if len(f.calls) != 0 {
		t.Errorf("terminal.run called: %v", f.calls)
	}
	boom := errors.New("hc down")
	f.handle["terminal.run"] = func(map[string]any) (any, error) { return nil, boom }
	if _, err := m.OpenTerminal(context.Background(), TerminalSpec{Name: "a"}); !errors.Is(err, boom) {
		t.Errorf("core error = %v", err)
	}
}

func TestWorkerPlain(t *testing.T) {
	for role, want := range map[string]bool{RoleTerminal: true, RoleOrchestrator: false, "builder": false, "": false} {
		if got := (Worker{Role: role}).Plain(); got != want {
			t.Errorf("role %q: Plain() = %v, want %v", role, got, want)
		}
	}
}

// TestProfileAgentPicksTheKitAgent: a profile with an agent field starts as
// that kit agent; LoadConfig reads the field.
func TestProfileAgentPicksTheKitAgent(t *testing.T) {
	dir := t.TempDir()
	kit := filepath.Join(dir, "kit")
	if err := os.MkdirAll(filepath.Join(kit, "agents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kit, "agents", "planner.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "agents.json")
	data := `{"kit":"` + kit + `","profiles":{"planner-high":{"argv":["claude","--effort","high"],"agent":"planner"},"odd":{"argv":["claude"]}}}`
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(file)
	if err != nil || cfg.Profiles["planner-high"].Agent != "planner" {
		t.Fatalf("cfg = %+v, %v", cfg.Profiles["planner-high"], err)
	}
	f := &fakeCore{handle: map[string]func(map[string]any) (any, error){
		"terminal.run": func(map[string]any) (any, error) { return term("t1", "p", "live", nil, nil), nil },
	}}
	m := New(f, cfg)
	if _, err := m.Spawn(context.Background(), Spec{Name: "p", Profile: "planner-high"}); err != nil {
		t.Fatal(err)
	}
	argv := f.last().Params["argv"].([]any)
	want := []any{"claude", "--plugin-dir", kit, "--agent", "contr:planner", "--effort", "high"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
	// No agent field and no kit file of that name: the argv as written.
	if _, err := m.Spawn(context.Background(), Spec{Name: "o", Profile: "odd"}); err != nil {
		t.Fatal(err)
	}
	if argv := f.last().Params["argv"].([]any); !reflect.DeepEqual(argv, []any{"claude"}) {
		t.Errorf("odd argv = %v", argv)
	}
}
