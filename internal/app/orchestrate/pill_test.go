package orchestrate

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
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

// rig is a pill in a title bar row of a window w wide, left of a Search box at the right edge,
// with a fake send and toasts.
type rig struct {
	p        *Pill
	tt       *ui.Tester
	snap     agentstate.Snapshot
	selected string
	compact  bool
	tail     float32 // the width of the item right of the pill; 28 (Search) when 0
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
		Snapshot: func() agentstate.Snapshot { return r.snap },
		Selected: func() string { return r.selected },
		Send: func(addr, text string) error {
			r.sends = append(r.sends, sent{addr, text})
			return r.sendErr
		},
		Env: view.Env{Status: func(s string) { r.toasts = append(r.toasts, s) }},
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Row(c).FillWidth().Height(46).AlignItems(ui.Center).PaddingEnd(8).Children(func() {
				ui.Box(c).Grow(1).FillHeight()
				r.p.View(c, r.compact)
				tail := r.tail
				if tail == 0 {
					tail = 28
				}
				ui.Box(c).Label("search").Size(tail, 28).Shrink(0)
			})
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

// The pill ends space-8 left of the next item of the title bar, 28px high and centered in the bar.
func TestPillSitsLeftOfTheNextTitleBarItem(t *testing.T) {
	for _, s := range []agentstate.Snapshot{snapWith(false, 0, 0), snapWith(true, 2, 0)} {
		for _, compact := range []bool{false, true} {
			r := newRig(t, s, orch, 900)
			r.compact = compact
			r.tt.Frame()
			label := LabelButton
			if s.Orchestrating(s.Orchestrators[0]) {
				label = PillText(2)
			}
			pill, next := apptest.Box(t, r.tt, label), apptest.Box(t, r.tt, "search")
			if pill.X+pill.W+8 != next.X || pill.H != 28 || pill.Y != 9 {
				t.Errorf("%q compact %v: pill %+v, search %+v: want 28px high, top 9, 8 left of search",
					label, compact, pill, next)
			}
		}
	}
}

// Compact (a narrow window, D253): the mascot with the worker count, the full text as the
// tooltip; the button keeps the mascot alone.
func TestCompactPillShowsTheMascotAndTheCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		snap  agentstate.Snapshot
		label string // the element's label: the full text
		count string // the text that shows, "" for none
	}{
		{"three workers", snapWith(false, 3, 1), PillText(3), "3"},
		{"no worker yet", snapWith(true, 0, 0), PillText(0), ""},
		{"button", snapWith(false, 0, 0), LabelButton, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := tc.label // the text of the full pill
			if text == LabelButton {
				text = TextOrchestrate
			}
			r := newRig(t, tc.snap, orch, 900)
			wide, full := apptest.Box(t, r.tt, tc.label), count(r.tt, text)
			r.compact = true
			r.tt.Frame()
			box := apptest.Box(t, r.tt, tc.label)
			if box.W >= wide.W {
				t.Errorf("compact pill %vpx wide, full %vpx: want it narrower", box.W, wide.W)
			}
			if got := count(r.tt, text); got != full-1 {
				t.Errorf("compact pill: %q %d times, want %d (the label only)", text, got, full-1)
			}
			if tc.count != "" && count(r.tt, tc.count) != 1 {
				t.Errorf("texts %v, want the count %q", r.tt.Texts(), tc.count)
			}
			r.tt.Move(box.X+box.W/2, box.Y+box.H/2)
			r.tt.Advance(time.Second)
			if got := count(r.tt, text); got != full {
				t.Errorf("no tooltip %q; texts %v", text, r.tt.Texts())
			}
		})
	}
}

// count is the number of texts and labels of the last frame that are s.
func count(tt *ui.Tester, s string) int {
	n := 0
	for _, t := range tt.Texts() {
		if t == s {
			n++
		}
	}
	return n
}

// The full pill has no tooltip: its text is all there.
func TestFullPillHasNoTooltip(t *testing.T) {
	r := newRig(t, snapWith(false, 3, 0), orch, 900)
	box, full := apptest.Box(t, r.tt, PillText(3)), count(r.tt, PillText(3))
	r.tt.Move(box.X+box.W/2, box.Y+box.H/2)
	r.tt.Advance(time.Second)
	if got := count(r.tt, PillText(3)); got != full {
		t.Errorf("the text shows %d times after a rest, %d before: want no tooltip", got, full)
	}
}

// The popover opens under the pill, space-6 apart, its right edge on the pill's right edge; the
// compact button opens it too. A wide item right of the pill leaves room for a left-aligned
// popover, so the test sees the anchor and not a flip at the window's edge.
func TestPopoverRightEdgeIsOnThePillsRightEdge(t *testing.T) {
	for _, compact := range []bool{false, true} {
		r := newRig(t, snapWith(false, 0, 0), orch, 900)
		r.compact, r.tail = compact, 420
		r.tt.Frame()
		r.open(t)
		pill, pop := apptest.Box(t, r.tt, LabelButton), apptest.Box(t, r.tt, LabelPopover)
		if pill.X+pill.W != pop.X+pop.W {
			t.Errorf("compact %v: popover %+v, pill %+v: right edges differ", compact, pop, pill)
		}
		if pop.Y != pill.Y+pill.H+popoverGap {
			t.Errorf("compact %v: popover top %v, want %v", compact, pop.Y, pill.Y+pill.H+popoverGap)
		}
	}
}

// The pill has no ring at rest, in both states: its edge is the ground under it (D258).
func TestPillHasNoRingAtRest(t *testing.T) {
	for _, tc := range []struct {
		name  string
		snap  agentstate.Snapshot
		label string
	}{
		{"orchestrating", snapWith(false, 1, 0), PillText(1)},
		{"button", snapWith(false, 0, 1), LabelButton},
	} {
		pill := &Pill{Snapshot: func() agentstate.Snapshot { return tc.snap }, Selected: func() string { return orch }}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Row(c).Fill().AlignItems(ui.Center).Justify(ui.Center).Background(theme.Current().Ground).Children(func() {
				pill.View(c, false)
			})
		}, 400, 60)
		tt.Frame()
		b := apptest.Box(t, tt, tc.label)
		img := tt.Image()
		ground := theme.Current().Ground
		// The top edge, away from the round ends.
		y := b.Y + 0.5
		for x := b.X + b.H/2; x < b.X+b.W-b.H/2; x++ {
			if got := apptest.RGB(img, x, y); !apptest.Near(got, ground, 1) {
				t.Errorf("%s: %v at (%v, %v) on the top edge of %+v, want the ground %v", tc.name, got, x, y, b, ground)
				break
			}
		}
	}
}
