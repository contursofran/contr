package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	w, err := m.Spawn(context.Background(), Spec{Name: "b-158", Profile: "builder", Issue: "158", Worktree: "/w/a", Task: "read TASK.md"})
	if err != nil {
		t.Fatal(err)
	}
	want := Worker{Name: "b-158", Address: "t7", Role: "builder", Issue: "158", Profile: "builder", Worktree: "/w/a", Live: true, Status: "blocked", Kind: "permission", Msg: "allow?", Turn: 2, Title: "t"}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("worker\n got %+v\nwant %+v", w, want)
	}
	p := f.last().Params
	if got := p["argv"]; !reflect.DeepEqual(got, []any{"claude", "--model", "sonnet", "--effort", "medium", "read TASK.md"}) {
		t.Errorf("argv = %v", got)
	}
	if p["cwd"] != "/w/a" || p["no_parent"] != true || p["outlive_parent"] != true || p["wait"] != false || p["parent"] != nil {
		t.Errorf("params = %v", p)
	}

	if _, err := m.Spawn(context.Background(), Spec{Name: "r", Profile: "reviewer", Parent: "t1"}); err != nil {
		t.Fatal(err)
	}
	p = f.last().Params
	if p["parent"] != "t1" || p["no_parent"] != nil || p["cwd"] != nil {
		t.Errorf("parent params = %v", p)
	}
	if l := p["labels"].(map[string]any); l[LabelRole] != "reviewer" || l[LabelIssue] != nil {
		t.Errorf("labels = %v", l)
	}
	if got := len(p["argv"].([]any)); got != 5 {
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
	if txt, err := m.Read(ctx, "alpha", 40); err != nil || txt != "screen" || f.last().Params["lines"] != float64(40) {
		t.Errorf("read = %q, %v, %v", txt, err, f.last().Params)
	}
	if _, err := m.Read(ctx, "alpha", 0); err != nil || f.last().Params["lines"] != nil {
		t.Errorf("read all: %v, %v", err, f.last().Params)
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
