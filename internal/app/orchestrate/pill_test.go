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

// outlineAt makes the rig's outline column start at top; ok false is no column.
func (r *rig) outlineAt(top float32, ok bool) {
	r.p.OutlineTop = func() (float32, bool) { return top, ok }
}

func TestPillPlaceFollowsTheOutlineColumnAndKeepsClearOfTheNotch(t *testing.T) {
	// The pill's rows end at 34px (top 8, height 26), or at 64px under the notch (top 38). The
	// outline column is inset 10px from the right side and 20px wide.
	for _, tc := range []struct {
		name    string
		width   float32
		notch   bool
		top     float32 // the outline column's top
		column  bool    // the outline column shows
		right   float32 // the pill's space to the right side
		down    bool    // the pill moves under the notch
		noField bool    // OutlineTop is nil
	}{
		{"no outline source", 900, false, 0, false, 10, false, true},
		{"no column", 900, false, 0, false, 10, false, false},
		{"short column", 900, false, 180, true, 10, false, false},
		{"column at the pill's bottom", 900, false, 34, true, 10, false, false},
		{"column reaches the pill", 900, false, 33, true, 40, false, false},
		{"tall column", 900, false, -20, true, 40, false, false},
		{"wide, notch, short column", 900, true, 180, true, 10, false, false},
		{"wide, notch, tall column", 900, true, 50, true, 40, false, false},
		{"narrow, no notch, short column", 420, false, 50, true, 10, false, false},
		{"narrow, notch, column reaches the second row", 420, true, 50, true, 40, true, false},
		{"narrow, notch, short column", 420, true, 180, true, 10, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, snapWith(false, 0, 0), orch, tc.width)
			if !tc.noField {
				r.outlineAt(tc.top, tc.column)
			}
			r.notch = tc.notch
			r.tt.Frame()
			pill := apptest.Box(t, r.tt, LabelButton)
			if got := tc.width - (pill.X + pill.W); got != tc.right {
				t.Errorf("pill %+v is %vpx from the right side, want %vpx", pill, got, tc.right)
			}
			if tc.column {
				outline := ui.Rect{X: tc.width - 10 - 20, Y: tc.top, W: 20, H: 400}
				if overlaps(pill, outline) {
					t.Errorf("pill %+v overlaps the outline column %+v", pill, outline)
				}
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

// The popover's right edge is on the pill's right edge, at 10px and at 40px.
func TestPopoverRightEdgeIsOnThePillsRightEdge(t *testing.T) {
	for _, tc := range []struct{ top, right float32 }{{200, 10}, {0, 40}} {
		top := tc.top
		r := newRig(t, snapWith(false, 0, 0), orch, 900)
		r.outlineAt(top, true)
		r.open(t)
		pill, pop := apptest.Box(t, r.tt, LabelButton), apptest.Box(t, r.tt, LabelPopover)
		if got := 900 - (pill.X + pill.W); got != tc.right {
			t.Errorf("column top %v: pill %vpx from the right side, want %vpx", top, got, tc.right)
		}
		if pill.X+pill.W != pop.X+pop.W {
			t.Errorf("column top %v: popover %+v, pill %+v: right edges differ", top, pop, pill)
		}
		if pop.Y != pill.Y+pill.H+popoverGap {
			t.Errorf("column top %v: popover top %v, want %v", top, pop.Y, pill.Y+pill.H+popoverGap)
		}
	}
}

// The lead (the Chat | Terminal switch, D246) shows left of the pill, space-8 apart, and alone
// for a terminal without a pill.
func TestLeadShowsLeftOfThePill(t *testing.T) {
	lead := func() func(c *ui.Context) {
		return func(c *ui.Context) { ui.Box(c).Label("lead").Size(80, pillHeight) }
	}
	r := newRig(t, snapWith(false, 0, 0), orch, 900)
	r.p.Lead = lead
	r.tt.Frame()
	l, b := apptest.Box(t, r.tt, "lead"), apptest.Box(t, r.tt, LabelButton)
	if l.X+l.W+8 != b.X || l.Y != b.Y {
		t.Errorf("lead %+v, button %+v: want the lead 8 left of the button", l, b)
	}
	r = newRig(t, snapWith(false, 0, 0), worker, 900)
	r.p.Lead = lead
	r.tt.Frame()
	if apptest.Absent(r.tt, "lead") || !apptest.Absent(r.tt, LabelButton) {
		t.Error("a terminal without a pill does not show the lead alone")
	}
	r.p.Lead = func() func(c *ui.Context) { return nil }
	r.tt.Frame()
	if !apptest.Absent(r.tt, "lead") {
		t.Error("a nil lead shows")
	}
}
