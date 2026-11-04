package agentstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

type call struct {
	Method string
	Params map[string]any
}

type fakeCore struct {
	mu      sync.Mutex
	calls   []call
	sendErr error
	snap    hc.Snapshot
	events  chan hc.Event
}

func (f *fakeCore) Call(_ context.Context, method string, params, _ any) error {
	b, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{method, p})
	if method == "terminal.send" {
		return f.sendErr
	}
	return nil
}

func (f *fakeCore) sends() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == "terminal.send" {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeCore) Subscribe(context.Context, hc.Filter) (agents.Stream, error) {
	return &stream{f}, nil
}

type stream struct{ f *fakeCore }

func (s *stream) Snapshot() hc.Snapshot   { return s.f.snap }
func (s *stream) Events() <-chan hc.Event { return s.f.events }
func (s *stream) Err() error              { return nil }
func (s *stream) Close() error            { return nil }

func term(addr, name, role, project, status string) json.RawMessage {
	t := map[string]any{
		"address": addr, "name": name, "state": "running", "turn": 1,
		"labels": map[string]string{"contr": "agent", "role": role, "project": project},
	}
	if status != "" {
		t["status"] = map[string]any{"state": status}
	}
	b, _ := json.Marshal(t)
	return b
}

// rig is a model on a fake core and a store in a temp folder.
type rig struct {
	core  *fakeCore
	store *owner.Store
	m     *Model
	pings atomic.Int32
}

func newRig(t *testing.T, terms ...json.RawMessage) *rig {
	t.Helper()
	r := &rig{
		core:  &fakeCore{snap: hc.Snapshot{Terminals: terms}, events: make(chan hc.Event)},
		store: &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")},
	}
	r.m = New(agents.New(r.core, agents.DefaultConfig()), r.store, func() { r.pings.Add(1) })
	return r
}

func (r *rig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.m.Start(ctx)
	t.Cleanup(func() { cancel(); r.m.Wait() })
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSnapshotSplitsOrchestratorsFirst(t *testing.T) {
	r := newRig(t,
		term("t1", "worker-a", "builder", "contr", "working"),
		term("t2", "orch-z", "orchestrator", "zed", "done"),
		term("t3", "orch-b", "orchestrator", "aerum", "idle"),
		term("t4", "orch-a", "orchestrator", "aerum", ""),
	)
	r.start(t)
	waitFor(t, "workers", func() bool { return len(r.m.Snapshot().Orchestrators) == 3 })
	s := r.m.Snapshot()
	var names []string
	for _, w := range s.Orchestrators {
		names = append(names, w.Name)
	}
	if got := strings.Join(names, ","); got != "orch-a,orch-b,orch-z" {
		t.Errorf("orchestrators = %s", got)
	}
	if len(s.Workers) != 1 || s.Workers[0].Name != "worker-a" {
		t.Errorf("workers = %+v", s.Workers)
	}
}

func TestSnapshotSplitsTerminals(t *testing.T) {
	r := newRig(t,
		term("t1", "worker-a", "builder", "contr", "working"),
		term("t2", "term-2", "terminal", "zed", ""),
		term("t3", "term-1", "terminal", "contr", ""),
		term("t4", "term-0", "terminal", "contr", ""),
		term("t5", "orch", "orchestrator", "contr", "idle"),
	)
	r.start(t)
	waitFor(t, "terminals", func() bool { return len(r.m.Snapshot().Terminals) == 3 })
	s := r.m.Snapshot()
	var names []string
	for _, w := range s.Terminals {
		names = append(names, w.Name)
	}
	if got := strings.Join(names, ","); got != "term-0,term-1,term-2" {
		t.Errorf("terminals = %s, want by project then name", got)
	}
	if len(s.Workers) != 1 || s.Workers[0].Name != "worker-a" {
		t.Errorf("workers = %+v, want only worker-a", s.Workers)
	}
	if len(s.Orchestrators) != 1 {
		t.Errorf("orchestrators = %+v", s.Orchestrators)
	}
}

func writeAtomic(t *testing.T, path, data string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerFileRenameIsSeen(t *testing.T) {
	r := newRig(t)
	r.start(t)
	waitFor(t, "start", func() bool { return r.pings.Load() >= 1 })
	before := r.pings.Load()
	writeAtomic(t, r.store.Path, `{"questions":[{"id":"D1","question":"Ship?"}],"next_question":2}`)
	waitFor(t, "question", func() bool { return len(r.m.Snapshot().Questions) == 1 })
	if r.pings.Load() <= before {
		t.Error("notify not called")
	}
}

func TestOwnerFileBadJSONKeepsItems(t *testing.T) {
	r := newRig(t)
	r.start(t)
	writeAtomic(t, r.store.Path, `{"questions":[{"id":"D1","question":"Ship?"}]}`)
	waitFor(t, "question", func() bool { return len(r.m.Snapshot().Questions) == 1 })
	writeAtomic(t, r.store.Path, `{garbage`)
	waitFor(t, "problem", func() bool { return r.m.Snapshot().Problem != "" })
	if n := len(r.m.Snapshot().Questions); n != 1 {
		t.Errorf("items after bad file = %d", n)
	}
	writeAtomic(t, r.store.Path, `{"questions":[{"id":"D1","question":"Ship?"}]}`)
	waitFor(t, "problem cleared", func() bool { return r.m.Snapshot().Problem == "" })
}

func ask(t *testing.T, r *rig, coordinator string) {
	t.Helper()
	if _, err := r.store.Ask(owner.Question{Project: "contr", Coordinator: coordinator, Question: "Ship?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Ask(owner.Question{Project: "contr", Coordinator: coordinator, Question: "Ship 2?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Ask(owner.Question{Project: "contr", Coordinator: coordinator, Question: "Ship 3?"}); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerWritesThenSends(t *testing.T) {
	r := newRig(t)
	ask(t, r, "t9")
	if err := r.m.Answer("D3", "yes"); err != nil {
		t.Fatal(err)
	}
	sends := r.core.sends()
	if len(sends) != 1 || sends[0].Params["terminal"] != "t9" || sends[0].Params["text"] != "Owner answered D3: yes\n" {
		t.Fatalf("sends = %+v", sends)
	}
	st, _ := r.store.Read()
	if st.Questions[2].AnsweredBy != "owner" || st.Questions[2].Answer != "yes" {
		t.Errorf("question = %+v", st.Questions[2])
	}
	s := r.m.Snapshot()
	if len(s.Resolved) != 1 || s.Resolved[0] != "D3" || len(s.Questions) != 3 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestAnswerWithoutCoordinatorOnlyWrites(t *testing.T) {
	r := newRig(t)
	ask(t, r, "")
	if err := r.m.Answer("D1", "yes"); err != nil {
		t.Fatal(err)
	}
	if n := len(r.core.sends()); n != 0 {
		t.Errorf("sends = %d", n)
	}
	if st, _ := r.store.Read(); st.Questions[0].Answer != "yes" {
		t.Error("answer not saved")
	}
}

func TestAnswerSendFailsKeepsAnswer(t *testing.T) {
	r := newRig(t)
	r.core.sendErr = errors.New("gone")
	ask(t, r, "t9")
	if err := r.m.Answer("D1", "yes"); err == nil {
		t.Error("no error")
	}
	if st, _ := r.store.Read(); st.Questions[0].Answer != "yes" {
		t.Error("answer lost")
	}
	if p := r.m.Snapshot().Problem; !strings.HasPrefix(p, "answer saved;") {
		t.Errorf("problem = %q", p)
	}
}

func TestAnswerUnknownIDIsError(t *testing.T) {
	r := newRig(t)
	ask(t, r, "t9")
	if err := r.m.Answer("D9", "yes"); err == nil {
		t.Error("no error")
	}
	if n := len(r.core.sends()); n != 0 {
		t.Errorf("sends = %d", n)
	}
}

func TestDoneUnpinsAndSends(t *testing.T) {
	r := newRig(t)
	if _, err := r.store.Pin(owner.Todo{Project: "contr", Coordinator: "t9", Text: "Test the build"}); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Done("T1"); err != nil {
		t.Fatal(err)
	}
	sends := r.core.sends()
	if len(sends) != 1 || sends[0].Params["text"] != "Owner did T1: Test the build\n" {
		t.Fatalf("sends = %+v", sends)
	}
	if st, _ := r.store.Read(); st.Todos[0].Done.IsZero() {
		t.Error("not done")
	}
	if err := r.m.Done("T1"); err == nil {
		t.Error("second done: no error")
	}
}

func TestClearDoneDropsResolved(t *testing.T) {
	r := newRig(t)
	ask(t, r, "")
	if err := r.m.Answer("D1", "yes"); err != nil {
		t.Fatal(err)
	}
	if n := len(r.m.Snapshot().Questions); n != 3 {
		t.Fatalf("before clear = %d", n)
	}
	r.m.ClearDone()
	s := r.m.Snapshot()
	if len(s.Questions) != 2 || len(s.Resolved) != 0 {
		t.Errorf("after clear = %+v", s)
	}
}

func TestStopEndsGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.m.Start(ctx)
	waitFor(t, "start", func() bool { return r.pings.Load() >= 1 })
	cancel()
	done := make(chan struct{})
	go func() { r.m.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutines did not end")
	}
	waitFor(t, "goroutines", func() bool { return runtime.NumGoroutine() <= before })
}
