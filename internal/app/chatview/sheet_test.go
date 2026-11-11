package chatview

// The ChatView part of the spec sheet (D179, D246): a terminal-sized panel in Chat mode with a
// prompt, an answer with a table, open work groups with a background task and a message of another
// session, a failed task outside the fold, the live line of a working agent and the composer
// (D252). ChatViewWide shows a wide panel with a table wider than the column, ChatViewTeam the
// team row of an orchestrating session (D259, D260), ChatViewChips the chips of each kind and
// state in Claude's text (D262), ChatContent one answer with each kind of content (D261), and
// ChatViewScrolled a chat scrolled up with the pinned prompt and New messages (D266).

import (
	"strconv"
	"strings"
	"testing"
	"time"

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
	team := sheetChat(t, "working", lPrompt, lSaid, lCall1, lNote1, lNote2, lRes1, lSaid2, lTaskBad, lCall2)
	teamOf(team, Team{Agents: 4, NeedsYou: 2, Error: 1, Working: 2, Idle: 0})
	wide := sheetChat(t, "idle", lPrompt, `{"type":"assistant","uuid":"w1","message":{"content":[{"type":"text","text":`+
		strconv.Quote("Prose stays in the column.\n\n"+wideTable)+`}]}}`)
	content := sheetChat(t, "idle", `{"type":"assistant","uuid":"c1","message":{"content":[{"type":"text","text":`+
		strconv.Quote(contentAnswer)+`}]}}`)
	chips := sheetChat(t, "idle", lPrompt, said("k1", chipsText))
	chips.s.ch.Chips = sheetChips
	scrolled := sheetScrolled(t)
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "ChatView", W: sheetW, H: sheetH, View: working.view},
		{Name: "ChatViewWide", W: 1500, H: 420, View: wide.view},
		{Name: "ChatViewTeam", W: sheetW, H: 420, View: team.view},
		{Name: "ChatViewChips", W: sheetW, H: 420, View: chips.view},
		{Name: "ChatContent", W: sheetW, H: 2300, View: content.view},
		{Name: "ChatViewScrolled", W: sheetW, H: scrolledH, View: scrolled.view},
	}, []string{"ChatView", "ChatViewWide", "ChatViewTeam", "ChatViewChips", "ChatContent", "ChatViewScrolled"})
}

// contentAnswer has each kind of content: headings, lists, a quote, the five remarks, a table
// with numbers and status words, inline code, a Go code block and a diff block.
const contentAnswer = "Four builders work with the M1 rules: red never fills or rings anything.\n\n" +
	"| Worker | Builds | State | Checks | Added | Removed |\n|---|---|---|---|---|---|\n" +
	"| b-649 | Rows and badge | CI running | 2 / 3 | +212 | −148 |\n" +
	"| b-665 | Fewer lines | building | — | +64 | −131 |\n" +
	"| b-658 | First-run texts | merged | 3 / 3 | +84 | −12 |\n\n" +
	"# Heading one\n\nText under it.\n\n## Heading two\n\n### Merge order\n\n" +
	"1. PR #667 first-run texts: passed CI.\n2. PR #656 rows and badge, after `main`:\n   - its checks pass\n   - the owner reads it\n3. #659 transitions: last.\n\n" +
	"- A bullet\n  - a nested bullet\n\n" +
	"- [x] `go test ./...` passes (412 tests).\n- [ ] The e2e suite did not run.\n\n" +
	"> In Light, red words measured **4.49:1**, just under the limit.\n\n" +
	"> [!NOTE]\n> The pill shows only in Terminal mode now.\n\n" +
	"> [!DONE]\n> PR #667 is merged.\n\n" +
	"> [!DECISION]\n> Light needs a darker red for text. Which one?\n\n" +
	"> [!WARNING]\n> `agentrow.go` changes in two PRs.\n\n" +
	"> [!UNVERIFIED]\n> I did not run the e2e suite.\n\n" +
	"> [!FOO]\n> An unknown tag stays a quote.\n\n" +
	"```go\n// Sync follows a terminal.\nfunc (s *Source) Sync(id string) error {\n\tif id == \"\" {\n\t\treturn errors.New(\"no terminal\")\n\t}\n\ts.limit = 920\n\treturn nil\n}\n```\n\n" +
	"```diff\n--- a/kit/agentrow.go\n+++ b/kit/agentrow.go\n@@ -10,3 +10,3 @@\n if row.Selected {\n-\tb.Background(t.Selected)\n+\tb.Background(t.SelectedQuiet)\n }\n```\n"

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

// sheetScrolled is a chat scrolled up into its second turn, with content that came after the owner
// left the end: the pinned prompt and Jump to latest (D266).
func sheetScrolled(t *testing.T) sheetView {
	lines := []string{lPrompt, lSaid, lCall1, lRes1, said("x1", scrolledAnswer),
		`{"type":"user","uuid":"q2","timestamp":"2026-11-10T10:04:00Z","message":{"content":"merge #611 first, then tell b-632 to merge main and run the review on its exact head"}}`,
		said("x2", scrolledAnswer), lCall2, lRes2Err, said("x3", scrolledAnswer), lCall1, lRes1, said("x4", scrolledAnswer), lCall2, said("x6", scrolledAnswer),
		said("x7", "b-632 merged `main`. Its checks run again.")}
	v := sheetChat(t, "working", lines...)
	tt := ui.NewTester(v.view, sheetW, scrolledH)
	tt.Frame()
	v.s.ch.list.ScrollTo(6, ui.Start)
	tt.Frame()
	tt.Advance(time.Second)
	// Content that came after the owner left the end.
	v.s.write(append(lines, said("x5", "b-631 is done: PR #633, checks passed."))...)
	tt.Frame()
	return v
}

const scrolledH = 460

const scrolledAnswer = "I merge #611 first: it and #613 both change `internal/app/panel.go`. Then b-632 merges `main` " +
	"before its review, and each PR gets a review on its exact head. I tell you when a worker needs you, " +
	"and the team row counts the workers that work."

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
			s.ch.View(c)
		})
	}}
}
