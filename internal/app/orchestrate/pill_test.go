package orchestrate

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/view"
)

// The addresses of the tests.
const (
	orch    = apptest.OrchAddr
	worker  = apptest.WorkAddr
	plain   = "w1:t3"
	session = "w1:t4"
)

// sent is one call of Pill.Send.
type sent struct{ addr, text string }

// rig is a pill in a terminal-sized window, with a fake send and toasts.
type rig struct {
	p        *Pill
	tt       *ui.Tester
	snap     agentstate.Snapshot
	selected string
	notch    bool
	sends    []sent
	sendErr  error
	toasts   []string
}

// snapWith is a project with the orchestrator lead, a Claude session in a plain terminal, a plain
// terminal and the builder of lead, plus extra workers of lead (live or not).
func snapWith(orchestrating bool, live, ended int) agentstate.Snapshot {
	s := agentstate.Snapshot{
		Orchestrators: []agents.Worker{
			{Name: "lead", Address: orch, Role: agents.RoleOrchestrator, Project: "demo", Live: true, Orchestrating: orchestrating},
			{Name: "claude", Address: session, Role: agents.RoleOrchestrator, Project: "demo", Live: true, Session: true},
		},
		Terminals: []agents.Worker{{Name: "zsh", Address: plain, Role: agents.RoleTerminal, Project: "demo", Live: true}},
		Workers:   []agents.Worker{{Name: "b1", Address: worker, Role: "builder", Project: "demo", Live: true}},
	}
	for i := range live {
		s.Workers = append(s.Workers, agents.Worker{Name: "l", Address: "w2:l" + string(rune('a'+i)), Role: "builder", Live: true, Parent: orch})
	}
	for i := range ended {
		s.Workers = append(s.Workers, agents.Worker{Name: "e", Address: "w2:e" + string(rune('a'+i)), Role: "builder", Parent: orch})
	}
	return s
}

func newRig(t *testing.T, snap agentstate.Snapshot, selected string, w float32) *rig {
	t.Helper()
	r := &rig{snap: snap, selected: selected}
	r.p = &Pill{
		Snapshot:   func() agentstate.Snapshot { return r.snap },
		Selected:   func() string { return r.selected },
		NotchShown: func() bool { return r.notch },
		Send: func(addr, text string) error {
			r.sends = append(r.sends, sent{addr, text})
			return r.sendErr
		},
		Env: view.Env{Status: func(s string) { r.toasts = append(r.toasts, s) }},
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			// A notch of the reserve's width at the top center, as the questions notch.
			if r.notch {
				ui.Row(c).Absolute().Top(0).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
					ui.Box(c).Label("notch").Size(notchReserve, 30)
				})
			}
			r.p.View(c)
		})
	}, int(w), 400)
	return r
}

func TestPillText(t *testing.T) {
	for n, want := range map[int]string{0: "Orchestrating", 1: "Orchestrating · 1 worker", 3: "Orchestrating · 3 workers", 12: "Orchestrating · 12 workers"} {
		if got := PillText(n); got != want {
			t.Errorf("PillText(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCommandText(t *testing.T) {
	for goal, want := range map[string]string{"": Command, "   ": Command, " ship #619 ": Command + " ship #619"} {
		if got := CommandText(goal); got != want {
			t.Errorf("CommandText(%q) = %q, want %q", goal, got, want)
		}
	}
}

func TestPillShowsOrchestratingWithLiveWorkers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hook        bool
		live, ended int
		want        string
	}{
		{"hook label, no worker yet", true, 0, 0, "Orchestrating"},
		{"one live worker", false, 1, 0, "Orchestrating · 1 worker"},
		{"three live workers, one ended", false, 3, 1, "Orchestrating · 3 workers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, snapWith(tc.hook, tc.live, tc.ended), orch, 900)
			if !r.tt.HasText(tc.want) {
				t.Errorf("texts %v, want %q", r.tt.Texts(), tc.want)
			}
			if !apptest.Absent(r.tt, LabelButton) {
				t.Error("an orchestrating session shows the Orchestrate button")
			}
		})
	}
}

func TestPillIsAButtonForAClaudeSessionThatDoesNotOrchestrate(t *testing.T) {
	for _, addr := range []string{orch, session} {
		r := newRig(t, snapWith(false, 0, 1), addr, 900)
		if apptest.Absent(r.tt, LabelButton) || !r.tt.HasText(TextOrchestrate) {
			t.Errorf("%s: no Orchestrate button; texts %v", addr, r.tt.Texts())
		}
		if r.tt.HasText(PillText(0)) {
			t.Errorf("%s: a session that does not orchestrate says Orchestrating", addr)
		}
	}
}

func TestNoPillOnWorkerPlainOrNoTerminal(t *testing.T) {
	for _, addr := range []string{worker, plain, "", "w9:gone"} {
		r := newRig(t, snapWith(true, 2, 0), addr, 900)
		if !apptest.Absent(r.tt, LabelButton) || r.tt.HasText(PillText(2)) || r.tt.HasText(TextOrchestrate) {
			t.Errorf("%q: the pill shows: %v", addr, r.tt.Texts())
		}
	}
	// A session whose program ended has no pill: the text would go to a shell.
	s := snapWith(false, 0, 0)
	s.Orchestrators[0].Live = false
	if r := newRig(t, s, orch, 900); !apptest.Absent(r.tt, LabelButton) {
		t.Error("an ended session shows the button")
	}
}

// open clicks the button and checks that the popover shows with the focus in the goal field.
func (r *rig) open(t *testing.T) {
	t.Helper()
	if err := r.tt.Click(LabelButton); err != nil {
		t.Fatal(err)
	}
	if apptest.Absent(r.tt, LabelPopover) || !r.p.IsOpen() {
		t.Fatal("the click did not open the popover")
	}
	if !r.tt.Focused(LabelGoal) {
		t.Error("the goal field does not have the focus")
	}
}

func TestEnterSendsTheGoalToTheSelectedSession(t *testing.T) {
	r := newRig(t, snapWith(false, 0, 0), session, 900)
	r.open(t)
	r.tt.Type("ship #619")
	r.tt.Key(0, ui.KeyEnter)
	want := []sent{{session, "/contr:orchestrate ship #619"}}
	if len(r.sends) != 1 || r.sends[0] != want[0] {
		t.Fatalf("sends %v, want %v", r.sends, want)
	}
	if r.p.IsOpen() || !apptest.Absent(r.tt, LabelPopover) || r.p.goal != "" {
		t.Error("after the send the popover stays open or keeps the goal")
	}
}

func TestEmptyGoalSendsTheCommandAlone(t *testing.T) {
	r := newRig(t, snapWith(false, 0, 0), orch, 900)
	r.open(t)
	// The send button of the popover: its bottom right corner.
	box := apptest.Box(t, r.tt, LabelPopover)
	r.tt.ClickAt(box.X+box.W-40, box.Y+box.H-24)
	if len(r.sends) != 1 || r.sends[0] != (sent{orch, Command}) {
		t.Fatalf("sends %v, want one %q to %s", r.sends, Command, orch)
	}
}

func TestCancelAndEscCloseWithoutSending(t *testing.T) {
	r := newRig(t, snapWith(false, 0, 0), orch, 900)
	r.open(t)
	r.tt.Type("draft")
	if err := r.tt.Click(TextCancel); err != nil {
		t.Fatal(err)
	}
	if r.p.IsOpen() || !apptest.Absent(r.tt, LabelPopover) {
		t.Error("Cancel did not close the popover")
	}
	r.open(t)
	r.tt.Key(0, ui.KeyEscape)
	if r.p.IsOpen() || !apptest.Absent(r.tt, LabelPopover) {
		t.Error("Esc did not close the popover")
	}
	if len(r.sends) != 0 {
		t.Errorf("Cancel or Esc sent %v", r.sends)
	}
	if r.p.goal != "draft" {
		t.Errorf("goal %q after a close, want it kept", r.p.goal)
	}
}

func TestSendErrorShowsAToastAndKeepsThePopover(t *testing.T) {
	r := newRig(t, snapWith(false, 0, 0), orch, 900)
	r.sendErr = errors.New("hc offline")
	r.open(t)
	r.tt.Type("x")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.toasts) != 1 || r.toasts[0] != "hc offline" {
		t.Errorf("toasts %v, want the error", r.toasts)
	}
	if !r.p.IsOpen() || r.p.goal != "x" {
		t.Error("a failed send closed the popover or cleared the goal")
	}
}

func TestSelectionChangeClosesThePopover(t *testing.T) {
	r := newRig(t, snapWith(false, 0, 0), orch, 900)
	r.open(t)
	r.selected = session
	r.tt.Frame()
	if r.p.IsOpen() || !apptest.Absent(r.tt, LabelPopover) {
		t.Error("the popover stays open for another session")
	}
}

// overlaps tells whether two boxes share an area.
func overlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func TestPillKeepsClearOfTheOutlineAndTheNotch(t *testing.T) {
	// The outline column: inset 10px from the right side, 20px wide, full height.
	for _, tc := range []struct {
		name  string
		width float32
		notch bool
		down  bool // the pill moves under the notch
	}{
		{"wide, no notch", 900, false, false},
		{"wide, notch", 900, true, false},
		{"narrow, no notch", 420, false, false},
		{"narrow, notch", 420, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, snapWith(false, 0, 0), orch, tc.width)
			r.notch = tc.notch
			r.tt.Frame()
			pill := apptest.Box(t, r.tt, LabelButton)
			outline := ui.Rect{X: tc.width - 10 - 20, Y: 0, W: 20, H: 400}
			if overlaps(pill, outline) || pill.X+pill.W > outline.X-10+0.5 {
				t.Errorf("pill %+v reaches the outline column %+v", pill, outline)
			}
			if tc.notch {
				notch := apptest.Box(t, r.tt, "notch")
				if overlaps(pill, notch) {
					t.Errorf("pill %+v overlaps the notch %+v", pill, notch)
				}
			}
			wantY := float32(8)
			if tc.down {
				wantY = 30 + 8
			}
			if pill.Y != wantY {
				t.Errorf("pill top %v, want %v", pill.Y, wantY)
			}
		})
	}
}
