package app

// The tests of the frame that drive the questions notch, the card and the outline of package
// questions.

import (
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/owner"
	"github.com/contursofran/contr/internal/usage"
	"github.com/egoist/mygo/ui"
)

func TestNotchLayerLeavesKeysToTheTerminal(t *testing.T) {
	f, w, tt, src := keyFrame(t)
	notch := &questions.Notch{
		Snapshot: (&source{Snap: agentstate.Snapshot{Questions: apptest.Questions("a", 2)}}).Snapshot,
		Project:  func() string { return "a" },
		Open:     func(agentstate.NeedTab) { t.Error("a key opened the card") },
	}
	f.Overlay.Add(questions.LabelNotchLayer, notch.View)
	tt.Frame()
	if !tt.HasText("2 need you") {
		t.Fatalf("the notch did not show in the terminal overlay: %q", tt.Texts())
	}
	tt.Type("a")
	tt.Key(0, ui.KeyEnter)
	if n := attachesAfter(w, tt, src, 2); n < 2 {
		t.Errorf("attaches = %d: a key did not reach the terminal under the notch", n)
	}
}

// notchFrame is an attached frame whose source has an orchestrator and a
// worker in project a, and one in project b.
func notchFrame(t *testing.T) (*Frame, *ui.Tester, *source) {
	t.Helper()
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &source{Snap: agentstate.Snapshot{
		Orchestrators: []agents.Worker{
			{Name: "lead-a", Address: "oa", Project: "a", Role: agents.RoleOrchestrator, Live: true},
			{Name: "lead-b", Address: "ob", Project: "b", Role: agents.RoleOrchestrator, Live: true},
		},
		Workers:   []agents.Worker{{Name: "b-250", Address: "wa", Project: "a", Status: "working", Live: true}},
		Questions: append(apptest.Questions("a", 2), owner.Question{ID: "D7", Project: "b", Title: "b-251 checks"}),
	}}
	f.deps.StartAgents = func(func()) (agentstate.AgentSource, func()) { return src, func() {} }
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
		{state.KindOrchestrator, "oa", "2 need you"},
		{state.KindWorker, "wa", "2 need you"},
		{state.KindOrchestrator, "ob", "1 needs you"},
	} {
		f.Selection.Select(state.Selected{Kind: tc.kind, ID: tc.id})
		tt.Frame()
		if !tt.HasText(tc.want) {
			t.Errorf("%s: texts %q, want %q", tc.id, tt.Texts(), tc.want)
		}
	}
}

func TestFrameCmdJOpensTheQuestionsCard(t *testing.T) {
	f, tt, _ := notchFrame(t)
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "wa"})
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyJ)
	if !f.Card.IsOpen() || f.Card.Key() != "D1" {
		t.Fatalf("⌘J: card key %q open=%v, want D1", f.Card.Key(), f.Card.IsOpen())
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	if f.Card.IsOpen() {
		t.Error("the second ⌘J left the card open")
	}
	if err := tt.Click("2 need you. Open"); err != nil {
		t.Fatal(err)
	}
	if !f.Card.IsOpen() {
		t.Error("a click on the notch did not open the card")
	}
}

// The frame tells the outline which terminal has the focus: the selected one
// while the window has the focus.
func TestFrameTellsTheOutlineWhichTerminalHasTheFocus(t *testing.T) {
	f, w, _, _ := terminalFrame(t, nil)
	f.Outline = questions.NewOutline(func() agentstate.Snapshot { return agentstate.Snapshot{} }, f.Selection, view.Env{Invalidate: w.Invalidate})
	w.blur()
	f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: apptest.OrchAddr})
	if f.Outline.Focused() != "" {
		t.Errorf("a blurred window gives the focus to %q", f.Outline.Focused())
	}
	w.focus()
	if f.Outline.Focused() != apptest.OrchAddr {
		t.Errorf("focus: the outline has %q, want %q", f.Outline.Focused(), apptest.OrchAddr)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: apptest.WorkAddr})
	if f.Outline.Focused() != apptest.WorkAddr {
		t.Errorf("another terminal: the outline has %q, want %q", f.Outline.Focused(), apptest.WorkAddr)
	}
	w.blur()
	if f.Outline.Focused() != "" {
		t.Errorf("blur: the outline has %q", f.Outline.Focused())
	}
}

// The chat's team row (D259, D260): the team of a running orchestrator by the state of its live
// workers (an ended one is out, as in the pill),
// with the badge of its sidebar row; a click on "N needs you" opens the card as ⌘J and leaves an
// open card open.
func TestFrameChatTeam(t *testing.T) {
	f, tt, src := notchFrame(t)
	if _, ok := f.chatTeam("oa"); ok {
		t.Error("an orchestrator with no worker and no /contr:orchestrate has a team")
	}
	src.Snap.Workers = append(src.Snap.Workers,
		agents.Worker{Name: "b-1", Address: "w1", Parent: "oa", Status: "working", Live: true},
		agents.Worker{Name: "b-2", Address: "w2", Parent: "oa", Status: "blocked", Live: true},
		agents.Worker{Name: "b-3", Address: "w3", Parent: "oa", Status: "idle", Live: true},
		agents.Worker{Name: "b-4", Address: "w4", Parent: "oa", Status: "error", Live: true},
		agents.Worker{Name: "b-5", Address: "w5", Parent: "oa"},
		agents.Worker{Name: "b-6", Address: "w6", Parent: "ob", Status: "working", Live: true})
	f.syncAgents()
	got, ok := f.chatTeam("oa")
	// NeedsYou is the badge of the sidebar row (TestTheOrchestratorRowCountsItsItemsAndThoseOfItsWorkers
	// checks ItemsOf); this frame scans no repository, so no row lists oa and the badge is 0.
	want := chatview.Team{Agents: 4, Working: 1, Error: 1, Idle: 1, NeedsYou: f.SpacesList.ItemsOf("oa")}
	if !ok || got != want {
		t.Errorf("chatTeam(oa) = %+v, %v; want %+v", got, ok, want)
	}
	if pill := f.snapshot().LiveWorkersOf("oa"); got.Agents != pill {
		t.Errorf("the row counts %d agents, the pill %d", got.Agents, pill)
	}
	for _, addr := range []string{"", "wa", "w1", "nobody"} {
		if _, ok := f.chatTeam(addr); ok {
			t.Errorf("%q is no orchestrator, yet it has a team", addr)
		}
	}
	src.Snap.Orchestrators[0].Live = false
	f.syncAgents()
	if _, ok := f.chatTeam("oa"); ok {
		t.Error("an orchestrator that ended has a team")
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "wa"})
	tt.Frame()
	f.openNeeds()
	if !f.Card.IsOpen() || f.Card.Key() != "D1" {
		t.Fatalf("openNeeds: card key %q open=%v, want D1", f.Card.Key(), f.Card.IsOpen())
	}
	f.openNeeds()
	if !f.Card.IsOpen() {
		t.Error("a second click closed the card")
	}
}
