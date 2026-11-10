package chatview

// The ChatView part of the spec sheet (D179, D246): a terminal-sized panel in Chat mode with a
// prompt, an answer with a table, open work groups with a background task and a message of another
// session, a failed task outside the fold, the live line of a working agent and the composer
// (D252). ChatViewNeedsYou shows the needs-you line with a menu, ChatViewWide a wide panel with a
// table wider than the column, ChatViewTeam the team row of an orchestrating session (D259), and
// ChatViewChips the chips of each kind and state in Claude's text (D262).

import (
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

const (
	sheetW = 1000 - 2*apptest.SheetPad
	sheetH = 720
)

// TestSpecSheet draws the parts of package chatview (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	working := sheetChat(t, "working", lPrompt, lHook, lSaid, lCall1, lNote1, lCall2, lRes1, lNote2, lSaid2, lCall1, lRes1, lTaskOK, lAgent, lNudge, lCall2, lTaskBad)
	blocked := sheetChat(t, "blocked", lPrompt, lSaid2)
	team := sheetChat(t, "working", lPrompt, lSaid, lCall1, lNote1, lNote2, lRes1, lSaid2, lTaskBad, lCall2)
	teamOf(team, Team{Agents: 4, NeedsYou: 1, Working: 2, Done: 1})
	wide := sheetChat(t, "idle", lPrompt, `{"type":"assistant","uuid":"w1","message":{"content":[{"type":"text","text":`+
		strconv.Quote("Prose stays in the column.\n\n"+wideTable)+`}]}}`)
	chips := sheetChat(t, "idle", lPrompt, said("k1", chipsText))
	chips.s.ch.Chips = sheetChips
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "ChatView", W: sheetW, H: sheetH, View: working.view},
		{Name: "ChatViewNeedsYou", W: sheetW, H: 360, View: blocked.view},
		{Name: "ChatViewWide", W: 1500, H: 420, View: wide.view},
		{Name: "ChatViewTeam", W: sheetW, H: 420, View: team.view},
		{Name: "ChatViewChips", W: sheetW, H: 420, View: chips.view},
	}, []string{"ChatView", "ChatViewNeedsYou", "ChatViewWide", "ChatViewTeam", "ChatViewChips"})
}

// chipsText has each kind of chip in each state (D262), and names that stay text: an unknown
// number, a name in code and a path that does not exist.
const chipsText = "PRs: #668 open, #656 checks running, #667 passed, #664 failed, #663 merged, #660 closed. " +
	"Issue #649. Workers: b-649 needs you, b-665 works, b-658 is done, b-659 waits. " +
	"The conflict is in internal/app/kit/agentrow.go:42.\n\n" +
	"Stays text: #3, `b-649` and nope/missing.go."

// sheetChips knows the names of chipsText.
var sheetChips = &fakeChips{known: map[string]Chip{
	"668": {ChipPR, StateOpen}, "656": {ChipPR, StateRunning}, "667": {ChipPR, StatePassed},
	"664": {ChipPR, StateFailed}, "663": {ChipPR, StateMerged}, "660": {ChipPR, StateClosed},
	"649": {Kind: ChipIssue}, "b-649": {ChipWorker, StateNeedsYou}, "b-665": {ChipWorker, StateWorking},
	"b-658": {ChipWorker, StateDone}, "b-659": {ChipWorker, StateWaiting},
	"internal/app/kit/agentrow.go": {Kind: ChipFile},
}}

var wideTable = "| one | two | three | four |\n|---|---|---|---|\n| " + strings.Repeat(strings.Repeat("a long cell text ", 6)+" | ", 4)

// sheetView is a chat of the sheet: its session and its view.
type sheetView struct {
	s    *sess
	view func(c *ui.Context)
}

// teamOf makes the session of v orchestrate with team t (D259).
func teamOf(v sheetView, t Team) {
	v.s.ch.Team = func(string) (Team, bool) { return t, true }
}

// sheetChat is the view of a chat whose agent is in state status, with the transcript lines.
func sheetChat(t *testing.T, status string, lines ...string) sheetView {
	s := newSession(t, status)
	s.write(lines...)
	// Open the work groups.
	for _, row := range s.ch.model.Rows() {
		if row.Kind == RowGroup {
			open := true
			s.ch.folds[row.ID] = &open
		}
	}
	return sheetView{s: s, view: func(c *ui.Context) {
		pal := theme.Current()
		ui.Box(c).Fill().Background(pal.Stage).Border(1, pal.PanelLine).
			Radius(theme.Radius12).Children(func() {
			ui.Row(c).Absolute().Top(theme.Space8).Right(10).Children(func() { s.ch.Switch(c, term) })
			s.ch.View(c, func() {})
		})
	}}
}
