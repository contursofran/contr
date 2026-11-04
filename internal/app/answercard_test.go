package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/owner"
)

func recommended(i int) *int { return &i }

func askSource() *source {
	return &source{snap: agentstate.Snapshot{
		Questions: []owner.Question{{ID: "D1", Project: "a", Title: "Which db?", Question: "Which database should it use?",
			Options: []string{"sqlite", "postgres", "files"}, Recommended: recommended(1)}},
		Todos:   []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Test the bag"}},
		Workers: []agents.Worker{blockedWorker("b-2", "a", "Allow rm?")},
	}}
}

type cardRig struct {
	t    *testing.T
	card *FloatCard
	src  *source
	tt   *ui.Tester
}

func newCardRig(t *testing.T, src *source) *cardRig {
	t.Helper()
	r := &cardRig{t: t, src: src}
	r.card = &FloatCard{Source: src, Run: Runner{Go: func(f func()) { f() }}}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { r.card.View(c) })
	}, 900, 400)
	return r
}

func (r *cardRig) open(tab NeedTab) {
	r.card.Open(tab)
	r.tt.Frame()
}

var (
	tabD1 = NeedTab{Kind: NeedQuestion, Key: "D1", Title: "Which db?", Icon: icons.Decide, Project: "a"}
	tabT1 = NeedTab{Kind: NeedTodo, Key: "T1", Title: "Test the bag", Icon: icons.Do, Project: "a"}
)

func TestCardOptionsPutTheRecommendedFirst(t *testing.T) {
	q := askSource().snap.Questions[0]
	opts := CardOptions(q)
	if len(opts) != 3 || opts[0].Text != "postgres" || !opts[0].Recommended || opts[1].Text != "sqlite" || opts[2].Text != "files" {
		t.Errorf("options %+v", opts)
	}
	q.Recommended = nil
	if opts := CardOptions(q); opts[0].Text != "sqlite" || opts[0].Recommended {
		t.Errorf("no recommended: %+v", opts)
	}
}

func TestAnswerCardRecommendedFirst(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	first, ok1 := r.tt.Find("postgres")
	second, ok2 := r.tt.Find("sqlite")
	if !ok1 || !ok2 || first.X >= second.X {
		t.Fatalf("recommended should be first: %v %v; texts %q", first, second, r.tt.Texts())
	}
	if !r.tt.HasText("Which database should it use?") {
		t.Errorf("question text missing: %q", r.tt.Texts())
	}
	r.tt.Key(0, ui.KeyEnter)
	if len(r.src.answers) != 1 || r.src.answers[0] != [2]string{"D1", "postgres"} {
		t.Errorf("Enter sent %v, want the recommended option", r.src.answers)
	}
}

func TestAnswerCardSendsAnswer(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	if err := r.tt.Click("files"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.answers) != 1 || r.src.answers[0] != [2]string{"D1", "files"} {
		t.Fatalf("answers %v", r.src.answers)
	}
	if _, open := r.card.Opened(); open {
		t.Error("the card should close after the answer is sent")
	}
}

func TestAnswerCardDigitKeysPickTheOptionInCardOrder(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	r.tt.Key(0, ui.Key2)
	if len(r.src.answers) != 1 || r.src.answers[0][1] != "sqlite" {
		t.Errorf("key 2 sent %v, want the second option in card order (sqlite)", r.src.answers)
	}
	r2 := newCardRig(t, askSource())
	r2.open(tabD1)
	r2.tt.Key(0, ui.Key4)
	if len(r2.src.answers) != 0 {
		t.Errorf("key 4 with 3 options sent %v", r2.src.answers)
	}
}

func TestAnswerCardFreeTextAnswers(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	if err := r.tt.Click(LabelAnswerInput); err != nil {
		t.Fatal(err)
	}
	r.tt.Type("use duckdb")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.src.answers) != 1 || r.src.answers[0] != [2]string{"D1", "use duckdb"} {
		t.Errorf("answers %v", r.src.answers)
	}
}

func TestAnswerCardEmptyFreeTextSendsNothing(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	if err := r.tt.Click(CardSend); err != nil {
		t.Fatal(err)
	}
	if len(r.src.answers) != 0 {
		t.Errorf("an empty answer was sent: %v", r.src.answers)
	}
}

func TestAnswerCardSendFailureShowsProblem(t *testing.T) {
	src := askSource()
	src.actionErr = errors.New("answer saved; coordinator not reached: boom")
	r := newCardRig(t, src)
	r.open(tabD1)
	if err := r.tt.Click("sqlite"); err != nil {
		t.Fatal(err)
	}
	if _, open := r.card.Opened(); !open {
		t.Fatal("the card should stay open after a failure")
	}
	r.tt.Frame()
	if !r.tt.HasText("answer saved; coordinator not reached: boom") {
		t.Errorf("the failure is not shown: %q", r.tt.Texts())
	}
}

func TestAnswerCardShowsTheStateProblem(t *testing.T) {
	src := askSource()
	src.snap.Problem = "hc: down"
	r := newCardRig(t, src)
	r.open(tabD1)
	if !r.tt.HasText("hc: down") {
		t.Errorf("problem missing: %q", r.tt.Texts())
	}
}

func TestAnswerCardEscapeAndCloseButtonClose(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabD1)
	r.tt.Key(0, ui.KeyEscape)
	if _, open := r.card.Opened(); open {
		t.Error("Escape should close the card")
	}
	r.open(tabD1)
	if err := r.tt.Click(CardClose); err != nil {
		t.Fatal(err)
	}
	if _, open := r.card.Opened(); open {
		t.Error("Close should close the card")
	}
}

func TestAnswerCardResolvedQuestionHasNoOptions(t *testing.T) {
	src := askSource()
	src.snap.Resolved = []string{"D1"}
	r := newCardRig(t, src)
	r.open(tabD1)
	if r.tt.HasText("sqlite") || r.tt.HasText(CardSend) {
		t.Errorf("a resolved question should offer no answer: %q", r.tt.Texts())
	}
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 0 {
		t.Errorf("Enter answered a resolved question: %v", src.answers)
	}
}

func TestTodoCardDone(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabT1)
	if !r.tt.HasText("Test the bag") {
		t.Errorf("to-do text missing: %q", r.tt.Texts())
	}
	if err := r.tt.Click(CardDoneButton); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.src.dones, []string{"T1"}) {
		t.Errorf("dones %v", r.src.dones)
	}
	if _, open := r.card.Opened(); open {
		t.Error("the card should close after Done")
	}
}

func TestTodoCardEnterIsDone(t *testing.T) {
	r := newCardRig(t, askSource())
	r.open(tabT1)
	r.tt.Key(0, ui.KeyEnter)
	if !slices.Equal(r.src.dones, []string{"T1"}) {
		t.Errorf("dones %v", r.src.dones)
	}
}

func TestBlockedCardShowsMessageAndSelectsTheWorker(t *testing.T) {
	r := newCardRig(t, askSource())
	var got string
	var orch bool
	r.card.Select = func(a string, o bool) { got, orch = a, o }
	r.open(NeedTab{Kind: NeedBlocked, Key: "b-2", Title: "Allow rm?", Icon: icons.Blocked, Address: "t9"})
	if !r.tt.HasText("Allow rm?") {
		t.Errorf("message missing: %q", r.tt.Texts())
	}
	if err := r.tt.Click(CardFocusWorker); err != nil {
		t.Fatal(err)
	}
	if got != "t9" || orch {
		t.Errorf("selected %q orchestrator=%v", got, orch)
	}
	if _, open := r.card.Opened(); open {
		t.Error("the card should close after Focus")
	}
}

func TestAnswerCardWorksOffTheUIThread(t *testing.T) {
	src := askSource()
	var queued []func()
	r := newCardRig(t, src)
	r.card.Run = Runner{Go: func(f func()) { queued = append(queued, f) }}
	r.open(tabD1)
	if err := r.tt.Click("sqlite"); err != nil {
		t.Fatal(err)
	}
	if len(src.answers) != 0 {
		t.Fatal("Answer ran on the UI thread")
	}
	// A second click while the first runs sends nothing more.
	if err := r.tt.Click("files"); err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 {
		t.Fatalf("%d jobs queued, want 1", len(queued))
	}
	queued[0]()
	if len(src.answers) != 1 || src.answers[0][1] != "sqlite" {
		t.Errorf("answers %v", src.answers)
	}
}

func TestFrameOpensTheCardFromTheNeedsRow(t *testing.T) {
	src := askSource()
	f := NewFrame(nil)
	f.Selection = stateSelection()
	f.Card = &FloatCard{Source: src, Run: Runner{Go: func(f func()) { f() }}}
	f.Needs = &NeedsRow{Snapshot: src.Snapshot, Open: f.Card.Open}
	f.Slots.Needs, f.Slots.Card = f.Needs.View, f.Card.View
	tt := ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	if err := tt.Click("D1 Which db?"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find(LabelAnswerCard); !ok {
		t.Fatalf("the card did not open; texts %q", tt.Texts())
	}
	card, _ := tt.Find(LabelAnswerCard)
	needs, _ := tt.Find(LabelNeedsRow)
	if card.Y+card.H > needs.Y {
		t.Errorf("the card (%v) overlaps the needs row (%v)", card, needs)
	}
}

const claudeMenu = `
 Bash command
   rm -rf build

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again
   3. No, and tell Claude what to do differently
`

const claudeMenuOther = `
 Do you want to proceed?
 ❯ 1. Yes
   2. No
`

func menuRig(t *testing.T, texts ...string) *cardRig {
	src := askSource()
	src.snap.Workers[0].Turn = 1
	src.text = claudeMenu
	src.texts = texts
	r := newCardRig(t, src)
	return r
}

var tabMenu = NeedTab{Kind: NeedBlocked, Key: "b-2", Title: "Allow rm?", Icon: icons.Blocked, Address: "t9", Command: "claude", BlockKind: "permission"}

func TestBlockedCardShowsChoices(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	y, ok1 := r.tt.Find("Yes")
	n, ok2 := r.tt.Find("Yes, and don't ask again")
	f, ok3 := r.tt.Find(CardFocusWorker)
	if !ok1 || !ok2 || !ok3 || y.X >= n.X || n.X >= f.X {
		t.Fatalf("choices then Focus worker expected: %q", r.tt.Texts())
	}
	if !r.tt.HasText("Do you want to proceed?") {
		t.Errorf("question missing: %q", r.tt.Texts())
	}
}

func TestBlockedCardClickSendsKeys(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.sent) != 1 || string(r.src.sent[0]) != "1" || r.src.sentTo[0] != "t9" {
		t.Errorf("sent %q to %v", r.src.sent, r.src.sentTo)
	}
	if _, open := r.card.Opened(); !open {
		t.Error("the card stays open until the worker leaves blocked")
	}
}

func TestBlockedCardDigitKey(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	r.tt.Key(0, ui.Key2)
	if len(r.src.sent) != 1 || string(r.src.sent[0]) != "2" {
		t.Errorf("sent %q", r.src.sent)
	}
}

func TestBlockedCardEnterDoesNothing(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	r.tt.Key(0, ui.KeyEnter)
	if len(r.src.sent) != 0 {
		t.Errorf("Enter sent %q", r.src.sent)
	}
}

func TestBlockedCardPromptChanged(t *testing.T) {
	r := menuRig(t, claudeMenu, claudeMenuOther)
	r.open(tabMenu)
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.sent) != 0 {
		t.Fatalf("sent %q on a changed prompt", r.src.sent)
	}
	r.tt.Frame()
	if !r.tt.HasText(CardPromptChanged) {
		t.Errorf("notice missing: %q", r.tt.Texts())
	}
	if !r.tt.HasText("No") {
		t.Errorf("the new menu should show: %q", r.tt.Texts())
	}
}

func TestBlockedCardUnknownForm(t *testing.T) {
	r := menuRig(t)
	r.src.text = "nothing to see"
	r.open(tabMenu)
	if !r.tt.HasText("Allow rm?") || !r.tt.HasText(CardFocusWorker) || !r.tt.HasText(CardClose) || r.tt.HasText("Yes") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestBlockedCardAuthShowsNoChoices(t *testing.T) {
	r := menuRig(t)
	tab := tabMenu
	tab.BlockKind = "auth"
	r.open(tab)
	if r.tt.HasText("Yes") || !r.tt.HasText(CardFocusWorker) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestBlockedCardReadError(t *testing.T) {
	r := menuRig(t)
	r.src.readErr = errors.New("core gone")
	r.open(tabMenu)
	if !r.tt.HasText("core gone") || r.tt.HasText("Yes") || !r.tt.HasText(CardFocusWorker) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestBlockedCardSendError(t *testing.T) {
	r := menuRig(t)
	r.src.sendErr = errors.New("keys failed")
	r.open(tabMenu)
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if _, open := r.card.Opened(); !open || !r.tt.HasText("keys failed") {
		t.Errorf("open=%v texts %q", open, r.tt.Texts())
	}
}

func TestBlockedCardBusyIgnoresClicks(t *testing.T) {
	r := menuRig(t)
	var queued []func()
	r.card.Run = Runner{Go: func(f func()) { queued = append(queued, f) }}
	r.open(tabMenu)
	queued[0]() // the first read
	r.tt.Frame()
	_ = r.tt.Click("Yes")
	_ = r.tt.Click("Yes")
	if len(queued) != 2 {
		t.Fatalf("%d jobs queued, want read + one send", len(queued))
	}
	queued[1]()
	if len(r.src.sent) != 1 {
		t.Errorf("sent %d times", len(r.src.sent))
	}
}

func TestBlockedCardTextChoiceFocusesWorker(t *testing.T) {
	r := menuRig(t)
	var got string
	r.card.Select = func(a string, _ bool) { got = a }
	r.open(tabMenu)
	if err := r.tt.Click("No, and tell Claude what to do differently"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.sent) != 0 || got != "t9" {
		t.Errorf("sent %q, selected %q", r.src.sent, got)
	}
}

func TestBlockedCardRereadOnTurn(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	r.tt.Frame()
	n := len(r.src.read)
	if n != 1 {
		t.Fatalf("%d reads after open, want 1", n)
	}
	r.src.snap.Workers[0].Turn = 2
	r.tt.Frame()
	if len(r.src.read) != n+1 {
		t.Errorf("%d reads after a new turn, want %d", len(r.src.read), n+1)
	}
}

func TestBlockedCardClosesWhenNoLongerBlocked(t *testing.T) {
	r := menuRig(t)
	r.open(tabMenu)
	r.src.snap.Workers[0].Status = "working"
	r.tt.Frame()
	if _, open := r.card.Opened(); open {
		t.Error("the card should close")
	}
}

// TestBlockedCardShowsTheCommand: a boxed permission menu shows its detail
// (the tool and the command) above the question, so the owner can answer
// without the terminal.
func TestBlockedCardShowsTheCommand(t *testing.T) {
	r := menuRig(t)
	r.src.text = "────────\n" + claudeMenu
	r.open(tabMenu)
	if !r.tt.HasText("rm -rf build") || !r.tt.HasText("Do you want to proceed?") {
		t.Errorf("command or question missing: %q", r.tt.Texts())
	}
}
