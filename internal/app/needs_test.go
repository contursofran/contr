package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
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
	s.stopped = append(s.stopped, name)
	return s.actionErr
}
func (s *source) Close(name string) error {
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

func TestNeedsOrderBlockedThenQuestionsThenTodos(t *testing.T) {
	n := &NeedsRow{Snapshot: sampleSource().Snapshot}
	got := keys(n.Tabs())
	want := []string{"b-2", "b-3", "D1", "D2", "T1", "T2"}
	if !slices.Equal(got, want) {
		t.Fatalf("tabs %v, want %v", got, want)
	}
	tabs := n.Tabs()
	if tabs[0].Icon != icons.Blocked || tabs[2].Icon != icons.Decide || tabs[4].Icon != icons.Do || tabs[5].Icon != icons.Fyi {
		t.Errorf("icons wrong: %+v", tabs)
	}
	if tabs[3].Title != "Ship it?" {
		t.Errorf("title %q, want one line", tabs[3].Title)
	}
}

func TestNeedsProjectFilter(t *testing.T) {
	proj := "a"
	n := &NeedsRow{Snapshot: sampleSource().Snapshot, Scope: func() string { return proj }}
	if got, want := keys(n.Tabs()), []string{"b-2", "D1", "T1"}; !slices.Equal(got, want) {
		t.Fatalf("project a: %v, want %v", got, want)
	}
	proj = ""
	if len(n.Tabs()) != 6 {
		t.Errorf("no scope should show all, got %v", keys(n.Tabs()))
	}
}

func TestNeedsEmptyRowIsHidden(t *testing.T) {
	n := &NeedsRow{Snapshot: (&source{}).Snapshot}
	tt := ui.NewTester(n.View, 800, 200)
	if _, ok := tt.Find(LabelNeedsRow); ok {
		t.Error("an empty row must not show")
	}
	var nilSrc NeedsRow
	if len(nilSrc.Tabs()) != 0 {
		t.Error("nil source must give no tabs")
	}
}

func TestNeedsClickOpensTab(t *testing.T) {
	var opened []NeedTab
	n := &NeedsRow{Snapshot: sampleSource().Snapshot, Open: func(t NeedTab) { opened = append(opened, t) }}
	tt := ui.NewTester(n.View, 1200, 200)
	if err := tt.Click("D1 Which db?"); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0].Key != "D1" || opened[0].Kind != NeedQuestion {
		t.Fatalf("opened %+v", opened)
	}
}

func TestNeedsRowSitsInOneLine(t *testing.T) {
	n := &NeedsRow{Snapshot: sampleSource().Snapshot}
	tt := ui.NewTester(n.View, 1200, 200)
	r, ok := tt.Find(LabelNeedsRow)
	if !ok {
		t.Fatal("no row")
	}
	if r.H != theme.BoardLineHeight {
		t.Errorf("row height %v, want %v", r.H, theme.BoardLineHeight)
	}
}

func TestNeedsStrikeThroughUntilClearDone(t *testing.T) {
	core := &needsCore{snap: hc.Snapshot{}}
	store := &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")}
	if _, err := store.Ask(owner.Question{Project: "a", Question: "Which db?", Options: []string{"pg"}}); err != nil {
		t.Fatal(err)
	}
	m := agentstate.New(agents.New(core, agents.DefaultConfig()), store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	defer func() { cancel(); m.Wait() }()
	n := &NeedsRow{Snapshot: m.Snapshot, ClearDone: m.ClearDone}
	if tabs := n.Tabs(); len(tabs) != 1 || tabs[0].Resolved {
		t.Fatalf("before: %+v", tabs)
	}
	if err := m.Answer("D1", "pg"); err != nil {
		t.Fatal(err)
	}
	if tabs := n.Tabs(); len(tabs) != 1 || !tabs[0].Resolved {
		t.Fatalf("after answer the tab stays, resolved: %+v", tabs)
	}
	tt := ui.NewTester(n.View, 1200, 200)
	if err := tt.Click(LabelNeedsClearDone); err != nil {
		t.Fatal(err)
	}
	if tabs := n.Tabs(); len(tabs) != 0 {
		t.Fatalf("after clear done: %+v", tabs)
	}
}

func TestNeedsClearDoneOnlyWithResolved(t *testing.T) {
	s := sampleSource()
	n := &NeedsRow{Snapshot: s.Snapshot, ClearDone: s.ClearDone}
	tt := ui.NewTester(n.View, 1200, 200)
	if _, ok := tt.Find(LabelNeedsClearDone); ok {
		t.Error("clear done shows with nothing resolved")
	}
	s.snap.Resolved = []string{"D1"}
	tt.Frame()
	if err := tt.Click(LabelNeedsClearDone); err != nil || s.cleared != 1 {
		t.Errorf("click: %v, cleared %d", err, s.cleared)
	}
}

func TestNeedsRowSitsUnderTheTerminal(t *testing.T) {
	f := NewFrame(nil)
	f.Needs = &NeedsRow{Snapshot: sampleSource().Snapshot}
	f.Slots.Needs = f.Needs.View
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	term, row := box(t, tt, LabelTerminal), box(t, tt, LabelNeedsRow)
	if row.Y < term.Y+term.H-1 {
		t.Errorf("row %+v is not under the terminal %+v", row, term)
	}
	if row.X != term.X || row.W != term.W {
		t.Errorf("row %+v is not as wide as the terminal %+v", row, term)
	}
}

func TestFrameWithoutNeedsSlotHasNoRow(t *testing.T) {
	tt := ui.NewTester(NewFrame(nil).View, theme.WindowWidth, theme.WindowHeight)
	if _, ok := tt.Find(LabelNeedsRow); ok {
		t.Error("row shows without a slot")
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
