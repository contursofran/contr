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
	f.Needs = &NeedsRow{Source: src, Open: f.Card.Open}
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
