package app

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/owner"
	"github.com/contursofran/contr/internal/usage"
)

// notchRig is a notch in a terminal-sized window. The project follows project.
type notchRig struct {
	n       *Notch
	src     *source
	tt      *ui.Tester
	project string
	opened  []NeedTab
	closes  int
	isOpen  bool
}

func newNotchRig(t *testing.T, src *source, project string) *notchRig {
	t.Helper()
	r := &notchRig{src: src, project: project}
	r.n = &Notch{
		Snapshot: src.Snapshot,
		Project:  func() string { return r.project },
		Open:     func(tab NeedTab) { r.opened, r.isOpen = append(r.opened, tab), true },
		Close:    func() { r.closes++; r.isOpen = false },
		Opened:   func() bool { return r.isOpen },
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		if c.Shortcut(ui.Cmd, ui.KeyJ) {
			r.n.Toggle()
		}
		ui.Box(c).Fill().Children(func() { r.n.View(c) })
	}, 900, 400)
	return r
}

func qs(project string, n int) []owner.Question {
	var out []owner.Question
	for i := 0; i < n; i++ {
		out = append(out, owner.Question{ID: "D" + string(rune('1'+i)), Project: project, Title: "b-250 checks"})
	}
	return out
}

func TestNotchLabelCountsInWords(t *testing.T) {
	q := NeedTab{Kind: NeedQuestion}
	todo := NeedTab{Kind: NeedTodo}
	blocked := NeedTab{Kind: NeedBlocked}
	for _, tc := range []struct {
		items []NeedTab
		want  string
	}{
		{nil, ""},
		{[]NeedTab{q}, "1 question"},
		{[]NeedTab{q, q}, "2 questions"},
		{[]NeedTab{q, q, todo}, "3 items"},
		{[]NeedTab{todo}, "1 item"},
		{[]NeedTab{q, blocked}, "2 items"},
	} {
		if got := NotchLabel(tc.items); got != tc.want {
			t.Errorf("NotchLabel(%d items) = %q, want %q", len(tc.items), got, tc.want)
		}
	}
}

func TestNotchShowsAndHidesWithTheData(t *testing.T) {
	src := &source{}
	r := newNotchRig(t, src, "a")
	if len(r.tt.Texts()) != 0 {
		t.Fatalf("notch with no items shows %q", r.tt.Texts())
	}
	src.snap.Questions = qs("a", 1)
	r.tt.Frame()
	if !r.tt.HasText("1 question") {
		t.Fatalf("texts %q, want 1 question", r.tt.Texts())
	}
	if _, ok := r.tt.Find("1 question. Open"); !ok {
		t.Error("no element labelled \"1 question. Open\"")
	}
	src.snap.Questions = qs("a", 2)
	r.tt.Frame()
	if _, ok := r.tt.Find("2 questions. Open"); !ok || !r.tt.HasText("2 questions") {
		t.Errorf("texts %q, want 2 questions", r.tt.Texts())
	}
	src.snap.Questions = nil
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("notch stays after the data went: %q", r.tt.Texts())
	}
}

func TestNotchCountsOnlyItsProject(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{
		Questions: append(qs("a", 1), owner.Question{ID: "D9", Project: "b", Title: "other"}, owner.Question{ID: "D8", Project: "b", Title: "other"}),
		Todos:     []owner.Todo{{ID: "T9", Project: "b", Kind: "do", Text: "other"}},
		Workers:   []agents.Worker{blockedWorker("b-9", "b", "Allow rm?")},
	}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("1 question") {
		t.Errorf("project a: texts %q, want 1 question", r.tt.Texts())
	}
	r.project = "b"
	r.tt.Frame()
	if !r.tt.HasText("4 items") {
		t.Errorf("project b: texts %q, want 4 items", r.tt.Texts())
	}
	r.project = "c"
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("project c shows %q", r.tt.Texts())
	}
	r.project = ""
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("no project shows %q", r.tt.Texts())
	}
}

func TestNotchMixOfQuestionsTodosAndBlockedMenus(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{
		Questions: qs("a", 2),
		Todos:     []owner.Todo{{ID: "T1", Project: "a", Kind: "know", Text: "CI is slow"}},
	}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("3 items") {
		t.Fatalf("texts %q, want 3 items", r.tt.Texts())
	}
	if _, ok := r.tt.Find("3 items. Open"); !ok {
		t.Error("no element labelled \"3 items. Open\"")
	}
	src.snap.Todos = nil
	src.snap.Workers = []agents.Worker{blockedWorker("b-250", "a", "Allow rm?")}
	r.tt.Frame()
	if !r.tt.HasText("3 items") {
		t.Errorf("a blocked menu does not count: texts %q", r.tt.Texts())
	}
}

func TestNotchIgnoresResolvedItemsAndABrokenFile(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{Questions: qs("a", 2), Resolved: []string{"D1"}}}
	r := newNotchRig(t, src, "a")
	if !r.tt.HasText("1 question") {
		t.Errorf("a resolved question counts: texts %q", r.tt.Texts())
	}
	src.snap.OwnerProblem = "owner: owner.json: invalid character"
	r.tt.Frame()
	if len(r.tt.Texts()) != 0 {
		t.Errorf("notch shows with a broken questions file: %q", r.tt.Texts())
	}
	// A problem of hc alone leaves the notch.
	src.snap.OwnerProblem, src.snap.Problem = "", "hc: gone"
	r.tt.Frame()
	if !r.tt.HasText("1 question") {
		t.Errorf("hc problem hides the notch: %q", r.tt.Texts())
	}
}

func TestNotchClickOpensTheCardForTheFirstItem(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{
		Questions: qs("a", 1),
		Workers:   []agents.Worker{blockedWorker("b-250", "a", "Allow rm?")},
	}}
	r := newNotchRig(t, src, "a")
	if err := r.tt.Click("2 items. Open"); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 || r.opened[0].Key != "b-250" {
		t.Fatalf("opened %+v, want the blocked worker b-250 first (the order of the needs row)", r.opened)
	}
	// A second click closes the card.
	if err := r.tt.Click("2 items. Open"); err != nil {
		t.Fatal(err)
	}
	if r.closes != 1 || len(r.opened) != 1 {
		t.Errorf("second click: closes %d, opens %d; want 1 and 1", r.closes, len(r.opened))
	}
}

func TestNotchCmdJOpensAndClosesTheCard(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{Questions: qs("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if len(r.opened) != 1 || r.opened[0].Key != "D1" {
		t.Fatalf("⌘J opened %+v, want D1", r.opened)
	}
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if r.closes != 1 {
		t.Errorf("second ⌘J closes = %d, want 1", r.closes)
	}
}

func TestNotchCmdJWithNothingWaitingDoesNothing(t *testing.T) {
	r := newNotchRig(t, &source{}, "a")
	r.tt.Key(ui.Cmd, ui.KeyJ)
	if len(r.opened) != 0 || r.closes != 0 {
		t.Errorf("⌘J with no items: opens %d, closes %d", len(r.opened), r.closes)
	}
}

func TestNotchKeysOpenItOnlyWhileFocused(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{Questions: qs("a", 1)}}
	r := newNotchRig(t, src, "a")
	r.tt.SetFocused(true)
	r.tt.Type(" ")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.opened) != 0 {
		t.Fatalf("an unfocused notch took a key: opened %+v", r.opened)
	}
	// A click gives it the focus; then Enter presses it.
	if err := r.tt.Click("1 question. Open"); err != nil {
		t.Fatal(err)
	}
	r.isOpen, r.opened = false, nil
	r.tt.Key(0, ui.KeyEnter)
	if len(r.opened) != 1 {
		t.Errorf("a focused notch ignores Enter: opened %d", len(r.opened))
	}
}

func TestNotchLayerLeavesKeysToTheTerminal(t *testing.T) {
	f, w, tt, src := keyFrame(t)
	notch := &Notch{
		Snapshot: (&source{snap: agentstate.Snapshot{Questions: qs("a", 2)}}).Snapshot,
		Project:  func() string { return "a" },
		Open:     func(NeedTab) { t.Error("a key opened the card") },
	}
	f.Overlay.Add(LabelNotchLayer, notch.View)
	tt.Frame()
	if !tt.HasText("2 questions") {
		t.Fatalf("the notch did not show in the terminal overlay: %q", tt.Texts())
	}
	tt.Type("a")
	tt.Key(0, ui.KeyEnter)
	if n := attachesAfter(w, tt, src, 2); n < 2 {
		t.Errorf("attaches = %d: a key did not reach the terminal under the notch", n)
	}
}

// The notch reads the agent state, which reads owner.json on a goroutine. A
// store whose path is a pipe blocks any read forever, so a notch that reads
// the file on the UI thread would hang here.
func TestNotchReadsNoFileOnTheUIThread(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("no pipe: %v", err)
	}
	m := agentstate.New(agents.New(&needsCore{snap: hc.Snapshot{}}, agents.DefaultConfig()), &owner.Store{Path: path}, nil)
	n := &Notch{Snapshot: m.Snapshot, Project: func() string { return "a" }}
	done := make(chan int)
	go func() {
		tt := ui.NewTester(n.View, 400, 100)
		tt.Frame()
		done <- len(n.Items())
	}()
	select {
	case got := <-done:
		if got != 0 {
			t.Errorf("items = %d, want 0 before the model reads", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the notch blocked on the questions file")
	}
}

func TestNotchFollowsTheFileThroughTheModel(t *testing.T) {
	store := &owner.Store{Path: filepath.Join(t.TempDir(), "owner.json")}
	for i := 0; i < 2; i++ {
		if _, err := store.Ask(owner.Question{Project: "a", Title: "b-250 checks", Question: "Merge?"}); err != nil {
			t.Fatal(err)
		}
	}
	m := agentstate.New(agents.New(&needsCore{snap: hc.Snapshot{}}, agents.DefaultConfig()), store, nil)
	n := &Notch{Snapshot: m.Snapshot, Project: func() string { return "a" }}
	if got := len(n.Items()); got != 0 {
		t.Fatalf("items before the model starts = %d, want 0: the UI thread read the file", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	defer func() { cancel(); m.Wait() }()
	if got := NotchLabel(n.Items()); got != "2 questions" {
		t.Errorf("label after the read = %q, want 2 questions", got)
	}
}

// notchFrame is an attached frame whose source has an orchestrator and a
// worker in project a, and one in project b.
func notchFrame(t *testing.T) (*Frame, *ui.Tester, *source) {
	t.Helper()
	w := &fakeWindow{visible: true}
	f := NewFrame()
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &source{snap: agentstate.Snapshot{
		Orchestrators: []agents.Worker{
			{Name: "lead-a", Address: "oa", Project: "a", Role: agents.RoleOrchestrator, Live: true},
			{Name: "lead-b", Address: "ob", Project: "b", Role: agents.RoleOrchestrator, Live: true},
		},
		Workers:   []agents.Worker{{Name: "b-250", Address: "wa", Project: "a", Status: "working", Live: true}},
		Questions: append(qs("a", 2), owner.Question{ID: "D7", Project: "b", Title: "b-251 checks"}),
	}}
	f.StartAgents = func(func()) (AgentSource, func()) { return src, func() {} }
	stop := f.Attach(w)
	t.Cleanup(func() { w.flush(); stop() })
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	return f, tt, src
}

func TestFrameShowsTheNotchInEveryTerminalOfTheProject(t *testing.T) {
	f, tt, _ := notchFrame(t)
	for _, tc := range []struct {
		kind state.SelectedKind
		id   string
		want string
	}{
		{state.KindOrchestrator, "oa", "2 questions"},
		{state.KindWorker, "wa", "2 questions"},
		{state.KindOrchestrator, "ob", "1 question"},
	} {
		f.Selection.Select(state.Selected{Kind: tc.kind, ID: tc.id})
		tt.Frame()
		if !tt.HasText(tc.want) {
			t.Errorf("%s: texts %q, want %q", tc.id, tt.Texts(), tc.want)
		}
	}
}

func TestFrameCmdJOpensTheQuestionsCardNotTheSheet(t *testing.T) {
	f, tt, _ := notchFrame(t)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "wa"})
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	if !f.Card.IsOpen() || f.Card.key != "D1" {
		t.Fatalf("⌘J: card key %q open=%v, want D1", f.Card.key, f.Card.IsOpen())
	}
	if f.Sheet.Opened() {
		t.Error("⌘J opened the context sheet (D125)")
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	if f.Card.IsOpen() {
		t.Error("the second ⌘J left the card open")
	}
	if err := tt.Click("2 questions. Open"); err != nil {
		t.Fatal(err)
	}
	if !f.Card.IsOpen() {
		t.Error("a click on the notch did not open the card")
	}
}
