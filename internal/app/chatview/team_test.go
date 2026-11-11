package chatview

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

// mixed is a team of 4 live agents: one blocked (it needs you), one in error, one working, one
// idle.
var mixed = Team{Agents: 4, NeedsYou: 1, Error: 1, Working: 1, Idle: 1}

func TestTeamText(t *testing.T) {
	for _, tc := range []struct {
		t    Team
		want string
	}{
		{mixed, "Orchestrating · 4 agents · 1 needs you · 1 error · 1 working · 1 idle"},
		{Team{Agents: 3, Working: 2, Idle: 1}, "Orchestrating · 3 agents · 2 working · 1 idle"},
		{Team{Agents: 9, NeedsYou: 3, Error: 2, Working: 3, Idle: 1}, "Orchestrating · 9 agents · 3 need you · 2 error · 3 working · 1 idle"},
		{Team{Agents: 2, Error: 1, Idle: 1}, "Orchestrating · 2 agents · 1 error · 1 idle"},
		{Team{Agents: 1, Working: 1}, "Orchestrating · 1 agent · 1 working"},
		{Team{Agents: 1, NeedsYou: 1}, "Orchestrating · 1 agent · 1 needs you"},
		{Team{}, "Orchestrating"},
		{Team{NeedsYou: 2}, "Orchestrating · 2 need you"},
	} {
		if got := TeamText(tc.t); got != tc.want {
			t.Errorf("TeamText(%+v) = %q, want %q", tc.t, got, tc.want)
		}
	}
}

func TestFitParts(t *testing.T) {
	w := []float32{50, 40, 30}
	// Each part after the first takes the dot (4) and two gaps (6).
	for room, want := range map[float32]int{0: 0, -5: 0, 49: 0, 50: 1, 105: 1, 106: 2, 151: 2, 152: 3, 1000: 3} {
		if got := fitParts(w, 4, room); got != want {
			t.Errorf("fitParts(room %v) = %d, want %d", room, got, want)
		}
	}
}

// teamSession is a session in Chat mode whose team is *team; ok false: it does not orchestrate.
func teamSession(t *testing.T, status string, team *Team, ok *bool) *sess {
	t.Helper()
	s := newSession(t, status)
	s.ch.Team = func(addr string) (Team, bool) { return *team, *ok && addr == term }
	s.tt.Frame()
	return s
}

// The row says how the session orchestrates, in one row with the live text; not orchestrating,
// the live line shows alone as before.
func TestTeamRowShowsWhileTheSessionOrchestrates(t *testing.T) {
	team, ok := mixed, true
	s := teamSession(t, "idle", &team, &ok)
	s.write(lTurn)
	for _, tc := range []struct {
		t     Team
		texts []string
	}{
		{mixed, []string{"Orchestrating · 4 agents", "1 needs you", "1 error", "1 working", "1 idle"}},
		{Team{Agents: 1, Working: 1}, []string{"Orchestrating · 1 agent", "1 working"}},
		{Team{}, []string{"Orchestrating"}},
	} {
		team = tc.t
		s.tt.Frame()
		if !s.has(LabelTeam) {
			t.Errorf("%+v: no team row; texts %v", tc.t, s.tt.Texts())
		}
		for _, x := range tc.texts {
			if !shows(s, x) {
				t.Errorf("%+v: no %q in %v", tc.t, x, s.tt.Texts())
			}
		}
		if tc.t.NeedsYou == 0 && shows(s, "1 needs you") {
			t.Errorf("%+v: a needs-you part with no item", tc.t)
		}
	}
	if shows(s, "0 error") || shows(s, "0 working") || shows(s, "0 idle") || shows(s, "done") {
		t.Error("a part with no agent shows")
	}
	ok = false
	s.status = "working"
	s.write(lTurn, lRunning)
	if s.has(LabelTeam) {
		t.Error("a team row while the session does not orchestrate")
	}
	if !s.footerWorks() {
		t.Errorf("no working status in the footer: %v", s.tt.Texts())
	}
}

// "N needs you" is the one red of the row and opens the notch's card; the rest takes no click.
func TestTeamRowNeedsYouOpensTheNotch(t *testing.T) {
	team, ok := mixed, true
	s := teamSession(t, "idle", &team, &ok)
	opened := 0
	s.ch.OpenNeeds = func() { opened++ }
	s.tt.Frame()
	if err := s.tt.Click("1 needs you"); err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Errorf("a click on needs you opened the card %d times, want 1", opened)
	}
	if n := reddish(s.tt.Image(), apptest.Box(t, s.tt, "1 needs you")); n == 0 {
		t.Error("needs you is not in the red text token")
	}
	if err := s.tt.Click("1 working"); err == nil && opened != 1 {
		t.Error("a click on the count opened the card")
	}
	// Without the frame's func, the click does nothing and nothing breaks.
	s.ch.OpenNeeds = nil
	s.tt.Frame()
	if err := s.tt.Click("1 needs you"); err != nil {
		t.Fatal(err)
	}
}

// "N error" is in ink-100: not red, and no button (D260).
func TestTeamRowErrorIsNotRed(t *testing.T) {
	team, ok := Team{Agents: 2, Error: 2}, true
	s := teamSession(t, "idle", &team, &ok)
	opened := 0
	s.ch.OpenNeeds = func() { opened++ }
	s.tt.Frame()
	if !shows(s, "2 error") {
		t.Fatalf("no error part: %v", s.tt.Texts())
	}
	red := reddish(s.tt.Image(), apptest.Box(t, s.tt, LabelTeam))
	_ = s.tt.Click("2 error")
	// The same row with two idle agents: only the mascot is red in both.
	team = Team{Agents: 2, Idle: 2}
	s.tt.Frame()
	if mascot := reddish(s.tt.Image(), apptest.Box(t, s.tt, LabelTeam)); red != mascot {
		t.Errorf("the row with errors has %d red pixels, %d with none: error is red", red, mascot)
	}
	if opened != 0 {
		t.Error("a click on error opened the card")
	}
}

// While the session works, its live text sits at the right end of the team row, and no live line
// shows apart: the dock keeps its height. Its time ticks; at rest nothing asks for a redraw.
func TestTeamRowCarriesTheLiveText(t *testing.T) {
	team, ok := mixed, true
	s := teamSession(t, "working", &team, &ok)
	turn := s.tt.Now().Add(-51 * time.Second).UTC().Format(time.RFC3339Nano)
	s.write(strings.Replace(lTurn, "2026-11-10T10:00:00Z", turn, 1), lRunning)
	live := LabelLive + " Bash · go test ./..."
	row := apptest.Box(t, s.tt, LabelTeam)
	text := apptest.Box(t, s.tt, live)
	if text.Y < row.Y || text.Y+text.H > row.Y+row.H+0.5 || row.X+row.W-(text.X+text.W) > 1 {
		t.Errorf("live text %+v is not at the right end of the team row %+v", text, row)
	}
	if row.H != lineHeight {
		t.Errorf("team row is %vpx high, want the live line's %v", row.H, lineHeight)
	}
	if !s.tt.HasText("51s") {
		t.Errorf("no time: %v", s.tt.Texts())
	}
	if !s.ch.ticks {
		t.Error("a working session asks for no redraw of its time")
	}
	working := s.dockTop(t)
	s.status = "idle"
	s.write(lTurn, lRunning, lDone, lSaid2)
	if s.has(LabelLive) {
		t.Errorf("a live text at rest: %v", s.tt.Texts())
	}
	s.tt.Advance(2 * theme.MotionSelect) // past the end of the fade of the Chat | Terminal switch (D256)
	if s.ch.ticks || s.tt.FramePending() {
		t.Error("the team row asks for a redraw at rest")
	}
	if idle := s.dockTop(t); idle != working {
		t.Errorf("the dock moved from %v to %v when work stopped", working, idle)
	}
}

// shows tells that the text shows: the count's parts read as one text, "1 needs you · 1 working".
func shows(s *sess, text string) bool { return s.tt.HasText(text) }

// dockTop is the top of the team row.
func (s *sess) dockTop(t *testing.T) float32 {
	t.Helper()
	return apptest.Box(t, s.tt, LabelTeam).Y
}

// In a narrow column the live text shortens first, then the count drops its last parts; the head
// stays.
func TestTeamRowClipsFromTheRight(t *testing.T) {
	team, ok := mixed, true
	s := teamSession(t, "working", &team, &ok)
	turn := s.tt.Now().Add(-51 * time.Second).UTC().Format(time.RFC3339Nano)
	s.write(strings.Replace(lTurn, "2026-11-10T10:00:00Z", turn, 1), lRunning)
	s.tt.SetSize(450, 900)
	s.tt.Frame()
	s.tt.Frame() // the content width of the last frame sets the room
	if !shows(s, "Orchestrating · 4 agents") || !shows(s, "1 needs you") {
		t.Fatalf("narrow: the head or the first part went: %v", s.tt.Texts())
	}
	if shows(s, "1 idle") {
		t.Errorf("narrow: the last part stays: %v", s.tt.Texts())
	}
	s.tt.SetSize(340, 900)
	s.tt.Frame()
	s.tt.Frame()
	if !shows(s, "Orchestrating · 4 agents") || shows(s, "1 needs you") {
		t.Errorf("narrower: want the head alone: %v", s.tt.Texts())
	}
}

// The last entry clears the dock: the list has room under it as tall as the fade, and the fade
// sits on the list's bottom edge, over no entry.
func TestLastEntryIsNotUnderTheDock(t *testing.T) {
	team, ok := mixed, true
	s := teamSession(t, "idle", &team, &ok)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf(`{"type":"assistant","uuid":"a%d","message":{"content":[{"type":"text","text":"line %d"}]}}`, i, i))
	}
	s.write(lines...)
	s.tt.Frame()
	last, ok2 := s.tt.Find("line 39")
	if !ok2 {
		t.Fatalf("the list does not follow its end: %v", s.tt.Texts())
	}
	fade := apptest.Box(t, s.tt, LabelFade)
	dock := apptest.Box(t, s.tt, LabelTeam)
	if fade.H != fadeHeight || fade.Y+fade.H > dock.Y {
		t.Errorf("fade %+v is not above the dock %+v", fade, dock)
	}
	if last.Y+last.H > fade.Y+0.5 {
		t.Errorf("the last entry %+v is under the fade %+v", last, fade)
	}
}
