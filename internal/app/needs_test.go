package app

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/owner"
)

// needsCore is a fake hc core: a fixed snapshot, no events.
type needsCore struct {
	snap  hc.Snapshot
	sends []string
}

func (f *needsCore) Call(_ context.Context, method string, params, _ any) error {
	if method == "terminal.send" {
		b, _ := json.Marshal(params)
		f.sends = append(f.sends, string(b))
	}
	return nil
}

func (f *needsCore) Subscribe(context.Context, hc.Filter) (agents.Stream, error) {
	return &needsStream{f.snap}, nil
}

type needsStream struct{ snap hc.Snapshot }

func (s *needsStream) Snapshot() hc.Snapshot   { return s.snap }
func (s *needsStream) Events() <-chan hc.Event { return nil }
func (s *needsStream) Err() error              { return nil }
func (s *needsStream) Close() error            { return nil }

func needsTerm(addr, name, project, status, msg string) json.RawMessage {
	t := map[string]any{
		"address": addr, "name": name, "state": "running", "turn": 1,
		"labels": map[string]string{"contr": "agent", "role": "builder", "project": project},
	}
	if status != "" {
		t["status"] = map[string]any{"state": status, "msg": msg}
	}
	b, _ := json.Marshal(t)
	return b
}

// source is a stub AgentSource that lets a test set the snapshot and records
// the actions.
type source struct {
	snap      agentstate.Snapshot
	cleared   int
	answers   [][2]string // id, answer
	dones     []string
	actionErr error
	told      [][2]string // name, text of Send
	tellErr   error
	text      string // what Read returns
	readErr   error
	read      []string // names read
	texts     []string // what the next Reads return, before text
	sent      [][]byte // keys sent
	sentTo    []string
	sendErr   error
	signalled []string // names interrupted
	stopped   []string
	closed    []string
	// mu guards stopped and closed: the frame calls Stop and Close on a
	// goroutine.
	mu sync.Mutex
}

// calls returns copies of the stopped and closed names.
func (s *source) calls() (stopped, closed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.stopped), slices.Clone(s.closed)
}

func (s *source) Snapshot() agentstate.Snapshot { return s.snap }
func (s *source) Answer(id, a string) error {
	s.answers = append(s.answers, [2]string{id, a})
	return s.actionErr
}
func (s *source) Done(id string) error {
	s.dones = append(s.dones, id)
	return s.actionErr
}
func (s *source) ClearDone() { s.cleared++ }
func (s *source) Read(name string, _ int) (string, error) {
	s.read = append(s.read, name)
	if len(s.texts) > 0 {
		t := s.texts[0]
		s.texts = s.texts[1:]
		return t, s.readErr
	}
	return s.text, s.readErr
}
func (s *source) Send(name, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.told = append(s.told, [2]string{name, text})
	return s.tellErr
}
func (s *source) SendKeys(name string, keys []byte) error {
	s.sentTo = append(s.sentTo, name)
	s.sent = append(s.sent, keys)
	return s.sendErr
}
func (s *source) Interrupt(name string) error {
	s.signalled = append(s.signalled, name)
	return s.actionErr
}
func (s *source) Stop(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, name)
	return s.actionErr
}
func (s *source) Close(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, name)
	return s.actionErr
}

func blockedWorker(name, project, msg string) agents.Worker {
	return agents.Worker{Name: name, Project: project, Status: "blocked", Msg: msg}
}

func keys(tabs []NeedTab) []string {
	var out []string
	for _, t := range tabs {
		out = append(out, t.Key)
	}
	return out
}

func sampleSource() *source {
	return &source{snap: agentstate.Snapshot{
		Workers: []agents.Worker{
			{Name: "b-1", Project: "a", Status: "working"},
			blockedWorker("b-2", "a", "Allow rm?"),
			blockedWorker("b-3", "b", "Allow git push?"),
		},
		Questions: []owner.Question{
			{ID: "D1", Project: "a", Title: "Which db?"},
			{ID: "D2", Project: "b", Question: "Ship\nit?"},
		},
		Todos: []owner.Todo{
			{ID: "T1", Project: "a", Kind: "do", Text: "Test the bag"},
			{ID: "T2", Project: "b", Kind: "know", Text: "CI is slow"},
		},
	}}
}

func TestNeedItemsOrderBlockedThenQuestionsThenTodos(t *testing.T) {
	tabs := NeedItems(sampleSource().Snapshot, nil)()
	got := keys(tabs)
	want := []string{"b-2", "b-3", "D1", "D2", "T1", "T2"}
	if !slices.Equal(got, want) {
		t.Fatalf("tabs %v, want %v", got, want)
	}
	if tabs[0].Icon != icons.Blocked || tabs[2].Icon != icons.Decide || tabs[4].Icon != icons.Do || tabs[5].Icon != icons.Fyi {
		t.Errorf("icons wrong: %+v", tabs)
	}
	if tabs[3].Title != "Ship it?" {
		t.Errorf("title %q, want one line", tabs[3].Title)
	}
}

func TestNeedItemsProjectFilter(t *testing.T) {
	proj := "a"
	items := NeedItems(sampleSource().Snapshot, func() string { return proj })
	if got, want := keys(items()), []string{"b-2", "D1", "T1"}; !slices.Equal(got, want) {
		t.Fatalf("project a: %v, want %v", got, want)
	}
	proj = ""
	if len(items()) != 6 {
		t.Errorf("no scope should show all, got %v", keys(items()))
	}
}

func TestNeedItemsWithoutASourceListNothing(t *testing.T) {
	if got := NeedItems(nil, nil)(); len(got) != 0 {
		t.Fatalf("nil source gave %v", keys(got))
	}
	if got := NeedItems((&source{}).Snapshot, nil)(); len(got) != 0 {
		t.Fatalf("empty state gave %v", keys(got))
	}
}

func TestNeedItemsMarkResolvedItems(t *testing.T) {
	s := sampleSource()
	s.snap.Resolved = []string{"D1"}
	for _, tab := range NeedItems(s.Snapshot, nil)() {
		if tab.Resolved != (tab.Key == "D1") {
			t.Errorf("%s resolved = %v", tab.Key, tab.Resolved)
		}
	}
}

func TestNeedsSectionLongNameStaysOnOneLine(t *testing.T) {
	name := "b148-s3-card-with-a-very-long-worker-name"
	tabs := []NeedTab{{Kind: NeedBlocked, Key: name, Icon: icons.Blocked}}
	tt := ui.NewTester(func(c *ui.Context) { NeedsSection(c, tabs, nil) }, 400, 200)
	row, ok := tt.Find(name + " , blocked")
	if !ok {
		t.Fatal("no needs line")
	}
	if row.H != theme.BoardLineHeight {
		t.Fatalf("line height %v, want %v", row.H, theme.BoardLineHeight)
	}
	key, ok := tt.Find(name)
	if !ok {
		t.Fatal("no name text")
	}
	if key.H > theme.BoardLineHeight {
		t.Errorf("name height %v: it wrapped", key.H)
	}
	if key.W < 100 {
		t.Errorf("name width %v: it is squeezed", key.W)
	}
}
