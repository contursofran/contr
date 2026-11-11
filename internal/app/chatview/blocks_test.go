package chatview

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// saidAt is a text of Claude sent at the RFC 3339 time at.
func saidAt(id, at, text string) string {
	return `{"type":"assistant","uuid":"` + id + `","timestamp":"` + at + `","message":{"content":[{"type":"text","text":` +
		strconv.Quote(text) + `}]}}`
}

const (
	oldBoard = "Four builders are at work.\n\n```contr-status\n" +
		"b-649 | #649 | Rows and badge\nb-665 | #665 | Fewer lines\nb-658 | #658 | First-run texts\nb-659 | #659 | Short transitions\n```\n"
	newBoard = "M1 it is. Four builders work with the M1 rules.\n\n```contr-status\n" +
		"b-649 | #649 | Rows and badge\nb-665 | #665 | Fewer lines\nb-658 | #658 | First-run texts\nb-659 | #659 | Short transitions\n" +
		"b-640 | #640 | Old settings tab\n```\n\n" +
		"### Merge order\n\nAll four change the sidebar, so I merge them one at a time.\n\n```contr-queue\n" +
		"#667 First-run texts\n#656 Rows and badge\n#665 Fewer lines -- waits for #656, then merges main\n#659 Transitions -- waits for #656\n```\n"
	blockedQueue = "The queue stops at #656.\n\n```contr-queue\n" +
		"#667 First-run texts\n#656 Rows and badge\n#665 Fewer lines -- waits for #656\n```\n"
)

// blockChips knows the workers and numbers of the boards above: b-640 is not listed.
func blockChips() *fakeChips {
	return &fakeChips{known: map[string]Chip{},
		workers: map[string]WorkerInfo{
			"b-649": {Chip: Chip{ChipWorker, StateWorking}, PR: 656},
			"b-665": {Chip: Chip{ChipWorker, StateWorking}},
			"b-658": {Chip: Chip{ChipWorker, StateDone}, PR: 667},
			"b-659": {Chip: Chip{ChipWorker, StateWaiting}},
		},
		refs: map[int]RefInfo{
			656: {Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 2, Pending: 1}},
			667: {Chip: Chip{ChipPR, StateMerged}, Done: true, Checks: github.CheckSummary{Pass: 3}},
			665: {Chip: Chip{Kind: ChipIssue}},
			659: {Chip: Chip{Kind: ChipIssue}},
		}}
}

// today is today at hh:mm in the local zone, in RFC 3339.
func today(hh, mm int) string {
	y, m, d := time.Now().Date()
	return time.Date(y, m, d, hh, mm, 0, 0, time.Local).Format(time.RFC3339Nano)
}

// keepBoard gives the board of row id the values a live board kept at the RFC 3339 time when:
// the values of blockChips, b-649's checks one less.
func keepBoard(ch *Chat, id, when string) {
	at, _ := time.Parse(time.RFC3339Nano, when)
	fc := blockChips()
	fc.refs[656] = RefInfo{Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 1, Pending: 2}}
	lines, _ := ParseStatus(Parts(oldBoard)[1].Text)
	rows := make([]BoardRow, len(lines))
	for k, l := range lines {
		w, ok := fc.workers[l.Worker]
		r, known := fc.refs[w.PR]
		rows[k] = BoardRowOf(w, ok, r, known)
	}
	ch.boards[boardKey(id, 1)] = &blockSnap{rows: rows, at: at}
}

func TestParseStatus(t *testing.T) {
	got, ok := ParseStatus("```contr-status\nb-649 | #649 | Rows and badge\n\n  b-665|#665|Fewer lines  \n```\n")
	want := []BoardLine{{"b-649", 649, "Rows and badge"}, {"b-665", 665, "Fewer lines"}}
	if !ok || len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ParseStatus = %v %v, want %v", got, ok, want)
	}
	for _, src := range []string{
		"```contr-status\nb-649 | 649 | Rows\n```",   // no "#"
		"```contr-status\nb-649 | #649\n```",         // two fields
		"```contr-status\n\n```",                     // no line
		"```contr-status\nb-649 | #649 | Rows\n",     // no closing fence
		"```go\nb-649 | #649 | Rows\n```",            // another info string
		"~~~contr-status\nb-649 | #649 | Rows\n```",  // another fence
		"````contr-status\nb-649 | #649 | Rows\n```", // a shorter fence
	} {
		if got, ok := ParseStatus(src); ok {
			t.Errorf("ParseStatus(%q) = %v, want no board", src, got)
		}
	}
	if got, ok := ParseStatus("~~~~ contr-status\nb-649 | #649 | Rows | extra\n~~~~~\n"); !ok || got[0].Builds != "Rows | extra" {
		t.Errorf("a tilde fence with a | in the label: %v %v", got, ok)
	}
}

func TestParseQueue(t *testing.T) {
	got, ok := ParseQueue("```contr-queue\n#667 First-run texts\n#665 Fewer lines -- waits for #656, then merges main\n```")
	want := []QueueStep{{667, "First-run texts", ""}, {665, "Fewer lines", "waits for #656, then merges main"}}
	if !ok || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ParseQueue = %v %v, want %v", got, ok, want)
	}
	for _, src := range []string{
		"```contr-queue\n667 First-run texts\n```", // no "#"
		"```contr-queue\n#667\n```",                // no label
		"```contr-queue\n#667 a\nsome words\n```",  // a line that is no step
		"```contr-status\n#667 a\n```",             // a board's info string
	} {
		if got, ok := ParseQueue(src); ok {
			t.Errorf("ParseQueue(%q) = %v, want no queue", src, got)
		}
	}
}

func TestPartsMarkTheBlocks(t *testing.T) {
	parts := Parts(newBoard)
	var boards, queues int
	for _, p := range parts {
		if p.Board != nil {
			boards++
		}
		if p.Queue != nil {
			queues++
		}
	}
	if boards != 1 || queues != 1 {
		t.Errorf("%d boards and %d queues in %v, want one of each", boards, queues, parts)
	}
	// A block in a list item is no part of its own: it stays a code block.
	for _, p := range Parts("- one\n\n  ```contr-queue\n  #667 a\n  ```\n") {
		if p.Queue != nil {
			t.Errorf("a queue in a list item: %v", p)
		}
	}
}

func TestBoardRowOf(t *testing.T) {
	running := RefInfo{Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 2, Pending: 1, Skip: 4}}
	for _, tc := range []struct {
		name  string
		w     WorkerInfo
		found bool
		pr    RefInfo
		known bool
		want  BoardRow
	}{
		{"unknown", WorkerInfo{}, false, RefInfo{}, false, BoardRow{State: TextNotFound, Checks: "—"}},
		{"building", WorkerInfo{Chip: Chip{ChipWorker, StateWorking}}, true, RefInfo{}, false,
			BoardRow{Found: true, Worker: StateWorking, State: "building", Checks: "—"}},
		{"waiting", WorkerInfo{Chip: Chip{ChipWorker, StateWaiting}}, true, RefInfo{}, false,
			BoardRow{Found: true, Worker: StateWaiting, State: "waiting", Checks: "—"}},
		{"ci running", WorkerInfo{Chip: Chip{ChipWorker, StateWorking}, PR: 656}, true, running, true,
			BoardRow{Found: true, Worker: StateWorking, State: "CI running", PR: 656, PRState: StateRunning, Checks: "2 of 3"}},
		{"pr not read yet", WorkerInfo{Chip: Chip{ChipWorker, StateWaiting}, PR: 656}, true, RefInfo{}, false,
			BoardRow{Found: true, Worker: StateWaiting, State: "PR open", PR: 656, PRState: StateOpen, Checks: "—"}},
		{"needs you wins", WorkerInfo{Chip: Chip{ChipWorker, StateNeedsYou}, PR: 656}, true, running, true,
			BoardRow{Found: true, Worker: StateNeedsYou, State: "needs you", PR: 656, PRState: StateRunning, Checks: "2 of 3"}},
		{"merged", WorkerInfo{Chip: Chip{ChipWorker, StateDone}, PR: 667}, true, RefInfo{Chip: Chip{ChipPR, StateMerged}, Done: true, Checks: github.CheckSummary{Pass: 3}}, true,
			BoardRow{Found: true, Worker: StateDone, State: "merged", PR: 667, PRState: StateMerged, Checks: "3 of 3"}},
	} {
		if got := BoardRowOf(tc.w, tc.found, tc.pr, tc.known); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestStepViewOf(t *testing.T) {
	step := QueueStep{Ref: 656, Label: "Rows"}
	note := QueueStep{Ref: 665, Label: "Fewer lines", Note: "waits for #656"}
	for _, tc := range []struct {
		name  string
		s     QueueStep
		r     RefInfo
		known bool
		want  StepView
	}{
		{"unknown", step, RefInfo{}, false, StepView{}},
		{"note alone", note, RefInfo{}, false, StepView{Tone: StateWaiting, Words: "waits for #656"}},
		{"open issue and note", note, RefInfo{Chip: Chip{Kind: ChipIssue}}, true, StepView{Tone: StateWaiting, Words: "waits for #656"}},
		{"merged", note, RefInfo{Chip: Chip{ChipPR, StateMerged}, Done: true}, true, StepView{Done: true, Merged: true, Tone: StateMerged, Words: "merged"}},
		{"closed issue", step, RefInfo{Chip: Chip{Kind: ChipIssue}, Done: true}, true, StepView{Done: true, Words: "closed"}},
		{"one failed", step, RefInfo{Chip: Chip{ChipPR, StateFailed}, Checks: github.CheckSummary{Pass: 2, Fail: 1}}, true,
			StepView{Blocked: true, Words: "blocked: 1 check failed"}},
		{"two failed and note", note, RefInfo{Chip: Chip{ChipPR, StateFailed}, Checks: github.CheckSummary{Fail: 2}}, true,
			StepView{Blocked: true, Words: "blocked: 2 checks failed · waits for #656"}},
		{"failed, no counts", step, RefInfo{Chip: Chip{ChipPR, StateFailed}}, true, StepView{Blocked: true, Words: "blocked: checks failed"}},
		{"running", step, RefInfo{Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 2, Pending: 1}}, true,
			StepView{Tone: StateRunning, Words: "CI running · 2 of 3"}},
		{"passed", step, RefInfo{Chip: Chip{ChipPR, StatePassed}, Checks: github.CheckSummary{Pass: 3}}, true, StepView{Tone: StatePassed, Words: "CI passed · 3 of 3"}},
		{"open", step, RefInfo{Chip: Chip{ChipPR, StateOpen}}, true, StepView{Tone: StateOpen, Words: "open"}},
	} {
		if got := StepViewOf(tc.s, tc.r, tc.known); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// inside tells that the text s shows inside the element named name.
func inside(t *testing.T, tt *ui.Tester, name, s string) bool {
	t.Helper()
	a, ok := tt.Access(name)
	if !ok {
		t.Fatalf("no %q", name)
	}
	r, ok := tt.Find(s)
	return ok && r.X >= a.Bounds.X && r.Y >= a.Bounds.Y && r.X+r.W <= a.Bounds.X+a.Bounds.W+1 && r.Y+r.H <= a.Bounds.Y+a.Bounds.H+1
}

// hasPrefix is the first text that starts with p, "" for none.
func hasPrefix(tt *ui.Tester, p string) string {
	for _, s := range tt.Texts() {
		if strings.HasPrefix(s, p) {
			return s
		}
	}
	return ""
}

// The newest board is live: state, PR and checks from contr, and its time. When a newer board
// comes, the older one keeps the values it had and says its time; the live one follows the data.
func TestBoardIsLiveThenFrozen(t *testing.T) {
	s := newSession(t, "idle")
	fc := blockChips()
	fc.refs[656] = RefInfo{Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 1, Pending: 2}}
	s.ch.Chips = fc
	first := saidAt("o1", today(18, 45), oldBoard)
	s.write(lPrompt, first)
	if _, ok := s.tt.Access(LabelBoard + " live"); !ok {
		t.Fatalf("no live board in %v", s.tt.Texts())
	}
	for _, want := range []string{"CI running", "1 of 3", "#656", "building", "merged", "3 of 3", "waiting", "Rows and badge", "#649"} {
		if !s.tt.HasText(want) {
			t.Errorf("no %q in %v", want, s.tt.Texts())
		}
	}
	kept := TimeText(s.tt.Now(), s.tt.Now())
	if got := hasPrefix(s.tt, "live"); got != "live · updated "+kept {
		t.Errorf("the live line is %q, want %q", got, "live · updated "+kept)
	}
	// The data change and a newer board comes.
	s.tt.Advance(3 * time.Minute)
	fc.refs[656] = RefInfo{Chip: Chip{ChipPR, StateRunning}, Checks: github.CheckSummary{Pass: 2, Pending: 1}}
	s.write(lPrompt, first, saidAt("n1", today(18, 49), newBoard))
	if !inside(t, s.tt, LabelBoard+" frozen", "1 of 3") || inside(t, s.tt, LabelBoard+" frozen", "2 of 3") {
		t.Errorf("the older board does not keep its values: %v", s.tt.Texts())
	}
	if !inside(t, s.tt, LabelBoard+" live", "2 of 3") {
		t.Errorf("the newer board does not follow the data: %v", s.tt.Texts())
	}
	if want := "as of " + kept + " · " + TextNewer; !s.tt.HasText(want) {
		t.Errorf("no %q in %v", want, s.tt.Texts())
	}
	if want := "live · updated " + TimeText(s.tt.Now(), s.tt.Now()); hasPrefix(s.tt, "live") != want {
		t.Errorf("the live line is %q, want %q", hasPrefix(s.tt, "live"), want)
	}
	// A worker contr does not list shows its name and "not found".
	if !inside(t, s.tt, LabelBoard+" live", "b-640") || !inside(t, s.tt, LabelBoard+" live", TextNotFound) {
		t.Errorf("no unknown worker in the live board: %v", s.tt.Texts())
	}
}

// An older board that never showed live shows only what the agent wrote, with the time of its
// answer: no state newer than its answer.
func TestFrozenBoardWithoutValuesShowsTheAgentsLines(t *testing.T) {
	s := newSession(t, "idle")
	s.ch.Chips = blockChips()
	s.write(lPrompt, saidAt("o1", today(18, 45), oldBoard), saidAt("n1", today(18, 49), newBoard))
	if !inside(t, s.tt, LabelBoard+" frozen", "Short transitions") || inside(t, s.tt, LabelBoard+" frozen", "State") {
		t.Errorf("the frozen board shows more than the agent wrote: %v", s.tt.Texts())
	}
	if !inside(t, s.tt, LabelBoard+" live", "State") {
		t.Errorf("the live board has no State column: %v", s.tt.Texts())
	}
	if want := "as of 18:45 · " + TextNewer; !s.tt.HasText(want) {
		t.Errorf("no %q in %v", want, s.tt.Texts())
	}
}

// The PR of a board row opens as a chip does.
func TestBoardPROpens(t *testing.T) {
	s := newSession(t, "idle")
	fc := blockChips()
	s.ch.Chips = fc
	s.write(lPrompt, saidAt("o1", today(18, 45), oldBoard))
	if err := s.tt.Click("PR #656"); err != nil {
		t.Fatal(err)
	}
	if len(fc.opened) != 1 || fc.opened[0] != term+" 1 656" {
		t.Errorf("opened %v, want PR 656 of %s", fc.opened, term)
	}
}

// A queue shows its steps in order, the note in words; a merged step dims and a step whose
// checks failed is blocked and says so, in blocked-text.
func TestQueueSteps(t *testing.T) {
	s := newSession(t, "idle")
	fc := blockChips()
	fc.refs[656] = RefInfo{Chip: Chip{ChipPR, StateFailed}, Checks: github.CheckSummary{Pass: 2, Fail: 1}}
	s.ch.Chips = fc
	s.write(lPrompt, saidAt("q1", today(18, 52), blockedQueue))
	for k, want := range []string{"#667 First-run texts", "#656 Rows and badge", "#665 Fewer lines"} {
		if !inside(t, s.tt, LabelStep+" "+strconv.Itoa(k+1), want) {
			t.Errorf("step %d is not %q: %v", k+1, want, s.tt.Texts())
		}
	}
	for _, want := range []string{"merged", "blocked: 1 check failed", "waits for #656"} {
		if !s.tt.HasText(want) {
			t.Errorf("no %q in %v", want, s.tt.Texts())
		}
	}
	pal := theme.Current()
	if r := find(t, s.tt, "blocked: 1 check failed"); !hasInk(s.tt, r, rgba(pal.BlockedText)) {
		t.Error("the blocked words are not in blocked-text")
	}
	// The tester draws on a white ground, where white inks at an alpha read alike: test the ink.
	if stepLabelInk(pal, StepView{Done: true}) != pal.Ink64 || stepLabelInk(pal, StepView{}) != pal.Ink88 {
		t.Error("the merged step does not dim")
	}
}

// A block that does not parse shows as a code block.
func TestBadBlockIsACodeBlock(t *testing.T) {
	s := newSession(t, "idle")
	s.ch.Chips = blockChips()
	s.write(lPrompt, saidAt("b1", today(18, 45), "```contr-status\nb-649 working on rows\n```\n"))
	if _, ok := s.tt.Access(LabelBoard + " live"); ok {
		t.Error("a block that does not parse is a board")
	}
	if !s.tt.HasText("b-649 working on rows") {
		t.Errorf("the block's text does not show: %v", s.tt.Texts())
	}
}

// State words are grey (D267): only the dots have the content colors. A worker that needs the
// owner says so in blocked-text; a step's words are grey but for a blocked step.
func TestStateWordsAreGrey(t *testing.T) {
	s := newSession(t, "idle")
	fc := blockChips()
	fc.workers["b-665"] = WorkerInfo{Chip: Chip{ChipWorker, StateNeedsYou}}
	s.ch.Chips = fc
	s.write(lPrompt, saidAt("n1", today(18, 49), newBoard))
	pal := theme.Current()
	for _, w := range []string{"CI running", "merged", "waiting"} {
		r := find(t, s.tt, w)
		if !hasInk(s.tt, r, rgba(pal.Ink64)) {
			t.Errorf("the board's %q is not in ink-64", w)
		}
	}
	for _, c := range []ui.Color{pal.ContentRunning, pal.ContentDone, pal.ContentWaiting} {
		if r := find(t, s.tt, "CI running"); hasInk(s.tt, r, rgba(c)) {
			t.Errorf("the board's state word has the content color %v", c)
		}
	}
	if r := find(t, s.tt, "needs you"); !hasInk(s.tt, r, rgba(pal.BlockedText)) {
		t.Error("needs you is not in blocked-text")
	}
	for _, w := range []string{"CI running · 2 of 3", "waits for #656"} {
		r := find(t, s.tt, w)
		if !hasInk(s.tt, r, rgba(pal.Ink64)) || hasInk(s.tt, r, rgba(pal.ContentRunning)) || hasInk(s.tt, r, rgba(pal.ContentWaiting)) {
			t.Errorf("the queue's %q is not grey", w)
		}
	}
}
