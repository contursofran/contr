package app

import (
	"errors"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/owner"
)

func recommended(i int) *int { return &i }

// resolver is a source that, like the agent state, marks an item resolved once
// the owner answered it or did it.
type resolver struct{ *source }

func (r resolver) Answer(id, a string) error {
	err := r.source.Answer(id, a)
	if err == nil {
		r.source.snap.Resolved = append(r.source.snap.Resolved, id)
	}
	return err
}

func (r resolver) Done(id string) error {
	err := r.source.Done(id)
	if err == nil {
		r.source.snap.Resolved = append(r.source.snap.Resolved, id)
	}
	return err
}

const (
	qSource = "Section 4 of docs/design.md: \"the store must survive a restart\""
	qImpact = "Smallest to ship; no server to run."
)

// askSource has two questions (the first with impacts, refs and a source), a
// to-do and a blocked worker, all of project a.
func askSource() *source {
	return &source{snap: agentstate.Snapshot{
		Questions: []owner.Question{
			{ID: "D1", Project: "a", From: "b-250", Title: "Which db?", Question: "Which database should it use?",
				Options: []string{"sqlite", "postgres", "files"}, Recommended: recommended(1),
				Impacts: []string{qImpact, "Needs a server; scales out.", "Slowest to query."},
				Refs:    []string{"PR 12", "#250"}, Source: qSource},
			{ID: "D2", Project: "a", Title: "Ship now?", Question: "Ship the release today?", Options: []string{"yes", "no"}},
		},
		Todos:   []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Test the bag"}},
		Workers: []agents.Worker{blockedWorker("b-2", "a", "Allow rm?")},
	}}
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

// qRig is a notch with the questions card in a terminal-sized window.
type qRig struct {
	t     *testing.T
	card  *QuestionsCard
	notch *Notch
	src   *source
	tt    *ui.Tester
	term  ui.Handle // the focusable stand-in for the terminal
}

func newQRig(t *testing.T, src *source) *qRig {
	t.Helper()
	r := &qRig{t: t, src: src}
	r.card = &QuestionsCard{Source: resolver{src}, Run: Runner{Go: func(f func()) { f() }}}
	r.notch = &Notch{Snapshot: src.Snapshot, Project: func() string { return "a" },
		Open: r.card.Open, Close: r.card.Close, Opened: r.card.IsOpen, Panel: r.card.View}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Children(func() {
			ui.Box(c).Label("terminal").Fill().Focusable().Bind(&r.term)
			r.notch.View(c)
		})
	}, 900, 600)
	return r
}

func (r *qRig) open(key string) {
	r.t.Helper()
	items := r.notch.Items()
	i := slices.IndexFunc(items, func(t NeedTab) bool { return t.Key == key })
	if i < 0 {
		r.t.Fatalf("no item %s in %v", key, keys(items))
	}
	r.card.Open(items[i])
	r.tt.Frame()
	r.tt.Frame()
}

func (r *qRig) y(text string) float32 {
	r.t.Helper()
	e, ok := r.tt.Find(text)
	if !ok {
		r.t.Fatalf("no %q in %q", text, r.tt.Texts())
	}
	return e.Y
}

func TestChipsPerItemInTheOrderOfTheNeedsRow(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	// A blocked menu first, then the questions, then the to-dos (D124).
	yb, y1, y2, yt := r.y("Allow rm?"), r.y("Which db?"), r.y("Ship now?"), r.y("Test the bag")
	if yb != y1 || y1 != y2 || y2 != yt {
		t.Fatalf("the chips should share a line: %v %v %v %v", yb, y1, y2, yt)
	}
	xb, _ := r.tt.Find("Allow rm?")
	x1, _ := r.tt.Find("Which db?")
	x2, _ := r.tt.Find("Ship now?")
	xt, _ := r.tt.Find("Test the bag")
	if !(xb.X < x1.X && x1.X < x2.X && x2.X < xt.X) {
		t.Errorf("chip order: blocked %v, D1 %v, D2 %v, T1 %v", xb.X, x1.X, x2.X, xt.X)
	}
	if !r.tt.HasText(questionsVia) {
		t.Errorf("%q missing: %q", questionsVia, r.tt.Texts())
	}
	// The selected item is the question that opened it.
	if !r.tt.HasText("Which database should it use?") || r.tt.HasText("Ship the release today?") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestChipClickSwitchesItem(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	if err := r.tt.Click("Ship now?"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.tt.HasText("Ship the release today?") || r.tt.HasText("Which database should it use?") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	if err := r.tt.Click("Test the bag"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.tt.HasText(QuestionsDone) {
		t.Errorf("a to-do shows its done action: %q", r.tt.Texts())
	}
}

func TestChoicesShowNumberRecommendedTagAndImpact(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	// The recommended choice is first, with its tag.
	if !(r.y("postgres") < r.y("sqlite") && r.y("sqlite") < r.y("files")) {
		t.Errorf("the recommended choice should come first: %q", r.tt.Texts())
	}
	if !r.tt.HasText("recommended") || !r.tt.HasText("1") || !r.tt.HasText("3") {
		t.Errorf("number or tag missing: %q", r.tt.Texts())
	}
	// Each impact sits under the label of its own option (D125).
	if !(r.y(qImpact) > r.y("sqlite") && r.y(qImpact) < r.y("files")) {
		t.Errorf("the impact of sqlite is not under it: %q", r.tt.Texts())
	}
	if !(r.y("Needs a server; scales out.") > r.y("postgres") && r.y("Needs a server; scales out.") < r.y("sqlite")) {
		t.Errorf("the impact of postgres is not under it: %q", r.tt.Texts())
	}
	if !r.tt.HasText(LabelQuestionsField) {
		t.Errorf("the free-answer field is missing: %q", r.tt.Texts())
	}
	if !r.tt.HasText("b-250") && !r.tt.HasText("from b-250") {
		t.Errorf("the context line is missing: %q", r.tt.Texts())
	}
}

func TestChoiceWithoutImpactShowsOnlyItsLabel(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D2")
	if !r.tt.HasText("yes") || !r.tt.HasText("no") || r.tt.HasText(LabelQuestionsDetails) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestDetailsShowsOnlyWithRefsOrSourceAndOpensAndCloses(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D2")
	if r.tt.HasText(LabelQuestionsDetails) {
		t.Fatalf("Details shows with no refs or source: %q", r.tt.Texts())
	}
	// A source alone is enough, and refs alone too.
	r.src.snap.Questions[1].Source = "an excerpt"
	r.tt.Frame()
	if !r.tt.HasText(LabelQuestionsDetails) {
		t.Fatalf("Details missing for a source: %q", r.tt.Texts())
	}
	r.src.snap.Questions[1].Source, r.src.snap.Questions[1].Refs = "", []string{"PR 7"}
	r.tt.Frame()
	if !r.tt.HasText(LabelQuestionsDetails) {
		t.Fatalf("Details missing for refs: %q", r.tt.Texts())
	}

	r.open("D1")
	if !r.tt.HasText(LabelQuestionsDetails) || r.tt.HasText(qSource) || r.tt.HasText("PR 12  #250") {
		t.Fatalf("Details should start closed: %q", r.tt.Texts())
	}
	if err := r.tt.Click(LabelQuestionsDetails); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		r.tt.Frame()
	}
	if !r.tt.HasText(qSource) || !r.tt.HasText("PR 12  #250") {
		t.Fatalf("Details did not open: %q", r.tt.Texts())
	}
	if err := r.tt.Click(LabelQuestionsDetails); err != nil {
		t.Fatal(err)
	}
	for range 30 {
		r.tt.Frame()
	}
	if r.tt.HasText(qSource) {
		t.Errorf("Details did not close: %q", r.tt.Texts())
	}
}

func TestClickAChoiceSendsItAndMovesToTheNextItem(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D1")
	if err := r.tt.Click("files"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if len(src.answers) != 1 || src.answers[0] != [2]string{"D1", "files"} {
		t.Fatalf("answers %v", src.answers)
	}
	if !r.card.IsOpen() || !r.tt.HasText("Ship the release today?") || r.tt.HasText("Which database should it use?") {
		t.Errorf("the card should show the next question: open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func TestLastAnswerClosesTheCardAndTheNotch(t *testing.T) {
	src := &source{snap: agentstate.Snapshot{Questions: []owner.Question{
		{ID: "D2", Project: "a", Title: "Ship now?", Question: "Ship the release today?", Options: []string{"yes", "no"}}}}}
	r := newQRig(t, src)
	r.open("D2")
	if err := r.tt.Click("yes"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.tt.Frame()
	if r.card.IsOpen() || r.tt.HasText("Ship now?") || r.tt.HasText("1 question") {
		t.Errorf("open=%v texts %q, want a closed card and no notch", r.card.IsOpen(), r.tt.Texts())
	}
	// A new question does not open the card by itself.
	src.snap.Questions = append(src.snap.Questions, owner.Question{ID: "D3", Project: "a", Title: "More?", Question: "More?"})
	r.tt.Frame()
	if r.card.IsOpen() || !r.tt.HasText("1 question") || r.tt.HasText("Or type an answer") {
		t.Errorf("the card opened by itself: open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func TestKeysPickEnterSendsPickedElseRecommended(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D1")
	// Enter with nothing picked sends the recommended choice (postgres).
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 1 || src.answers[0] != [2]string{"D1", "postgres"} {
		t.Fatalf("Enter sent %v, want the recommended choice", src.answers)
	}

	src = askSource()
	r = newQRig(t, src)
	r.open("D1")
	r.tt.Key(0, ui.Key3) // third place: files
	if len(src.answers) != 0 {
		t.Fatalf("a number key sent %v, it only picks", src.answers)
	}
	if r.card.picked != 2 {
		t.Fatalf("picked %d, want 2", r.card.picked)
	}
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 1 || src.answers[0][1] != "files" {
		t.Errorf("Enter sent %v, want the picked choice", src.answers)
	}

	r = newQRig(t, askSource())
	r.open("D1")
	r.tt.Key(0, ui.Key4) // only three choices
	if r.card.picked != -1 {
		t.Errorf("key 4 picked %d with three choices", r.card.picked)
	}
}

func TestEnterWithoutRecommendedOrPickSendsNothing(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D2")
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 0 {
		t.Errorf("Enter sent %v with nothing picked and no recommended choice", src.answers)
	}
}

func TestTypedTextBeatsThePickAndNumbersTypeInTheField(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D1")
	r.tt.Key(0, ui.Key1) // pick sqlite
	if err := r.tt.Click(LabelQuestionsField); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.tt.Type("2 weeks")
	if r.card.picked != 0 || r.card.text != "2 weeks" {
		t.Fatalf("picked %d text %q: a number typed in the field must stay text", r.card.picked, r.card.text)
	}
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 1 || src.answers[0] != [2]string{"D1", "2 weeks"} {
		t.Errorf("answers %v", src.answers)
	}
}

func TestTabMovesToTheNextChipAndWraps(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	r.tt.Key(0, ui.KeyTab)
	if r.card.key != "D2" {
		t.Fatalf("Tab: key %q, want D2", r.card.key)
	}
	r.tt.Key(0, ui.KeyTab) // the to-do
	r.tt.Key(0, ui.KeyTab) // wraps to the blocked worker
	if r.card.key != "b-2" {
		t.Errorf("Tab wrapped to %q, want b-2", r.card.key)
	}
}

// The control that had the focus goes when Tab changes the item. The card
// takes the focus again, so a second Tab still works.
func TestTabKeepsWorkingAfterTheItemChanges(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	r.tt.Click(LabelQuestionsDetails) // the focus moves to the trigger
	r.tt.Key(0, ui.KeyTab)
	r.tt.Frame()
	r.tt.Key(0, ui.KeyTab)
	r.tt.Frame()
	if r.card.key == "D1" || r.card.key == "D2" {
		t.Errorf("two Tab keys left the card on %q", r.card.key)
	}
}

func TestEscapeClosesTheCard(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	r.tt.Key(0, ui.KeyEscape)
	r.tt.Frame()
	if r.card.IsOpen() {
		t.Error("Escape left the card open")
	}
}

func TestPressOutsideClosesTheCard(t *testing.T) {
	r := newQRig(t, askSource())
	r.open("D1")
	r.tt.ClickAt(5, 590)
	r.tt.Frame()
	if r.card.IsOpen() {
		t.Error("a press on the terminal left the card open")
	}
}

func TestKeysWorkOnlyWithTheFocusInTheCard(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	// The terminal has the focus when the card shows, and the card does not
	// take it: the terminal keeps every key.
	r.tt.Frame()
	r.tt.ClickAt(450, 300)
	r.tt.Frame()
	r.card.Open(NeedTab{Kind: NeedQuestion, Key: "D1"})
	r.card.focus = false
	r.tt.Frame()
	r.tt.Frame()
	for _, k := range []ui.Key{ui.Key1, ui.Key2, ui.KeyEnter, ui.KeyTab} {
		r.tt.Key(0, k)
	}
	if len(src.answers) != 0 || r.card.picked != -1 || r.card.key != "D1" {
		t.Errorf("the card took keys without the focus: answers %v picked %d key %q", src.answers, r.card.picked, r.card.key)
	}
	if !r.card.IsOpen() {
		t.Error("the card closed")
	}
}

func TestAnswerRunsOffTheUIThread(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	var queued []func()
	r.card.Run = Runner{Go: func(f func()) { queued = append(queued, f) }}
	r.open("D1")
	if err := r.tt.Click("sqlite"); err != nil {
		t.Fatal(err)
	}
	if len(src.answers) != 0 {
		t.Fatal("Answer ran on the UI thread")
	}
	// A second send while the first runs does nothing more.
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

func TestSendErrorKeepsTheCardAndShowsIt(t *testing.T) {
	src := askSource()
	src.actionErr = errors.New("answer saved; coordinator not reached")
	r := newQRig(t, src)
	r.open("D1")
	if err := r.tt.Click("sqlite"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.card.IsOpen() || !r.tt.HasText("answer saved; coordinator not reached") {
		t.Errorf("open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func TestEmptyFieldSendsNothing(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D2")
	r.tt.Key(0, ui.KeyEnter)
	if len(src.answers) != 0 {
		t.Errorf("answers %v", src.answers)
	}
}

func TestQuestionAnsweredElsewhereLeavesTheChips(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("D1")
	src.snap.Resolved = []string{"D2"}
	r.tt.Frame()
	if r.tt.HasText("Ship now?") || !r.tt.HasText("Which db?") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	// The item shown goes: the card moves to the item now at its place.
	src.snap.Resolved = []string{"D1"}
	r.tt.Frame()
	r.tt.Frame()
	if r.tt.HasText("Which database should it use?") || !r.card.IsOpen() {
		t.Errorf("open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func TestTodoIsMarkedDone(t *testing.T) {
	src := askSource()
	r := newQRig(t, src)
	r.open("T1")
	if !r.tt.HasText("Test the bag") {
		t.Fatalf("to-do text missing: %q", r.tt.Texts())
	}
	if err := r.tt.Click(QuestionsDone); err != nil {
		t.Fatal(err)
	}
	if len(src.dones) != 1 || src.dones[0] != "T1" {
		t.Errorf("dones %v", src.dones)
	}
	r.tt.Frame()
	if r.tt.HasText("Test the bag") && r.card.key == "T1" {
		t.Errorf("the done to-do stays: %q", r.tt.Texts())
	}
}

func TestTodoDoneErrorKeepsTheCard(t *testing.T) {
	src := askSource()
	src.actionErr = errors.New("disk full")
	r := newQRig(t, src)
	r.open("T1")
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if !r.card.IsOpen() || !r.tt.HasText("disk full") {
		t.Errorf("open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func menuRig(t *testing.T, texts ...string) *qRig {
	t.Helper()
	src := askSource()
	src.snap.Workers[0].Turn = 1
	src.snap.Workers[0].Address = "t9"
	src.snap.Workers[0].Command = "claude"
	src.snap.Workers[0].Kind = "permission"
	src.text = claudeMenu
	src.texts = texts
	return newQRig(t, src)
}

func TestBlockedMenuShowsQuestionAndChoices(t *testing.T) {
	r := menuRig(t)
	r.src.text = "────────\n" + claudeMenu
	r.open("b-2")
	if !r.tt.HasText("Do you want to proceed?") || !r.tt.HasText("rm -rf build") {
		t.Fatalf("question or command missing: %q", r.tt.Texts())
	}
	if !(r.y("Yes") < r.y("Yes, and don't ask again") && r.y("Yes, and don't ask again") < r.y("No, and tell Claude what to do differently")) {
		t.Errorf("choices out of order: %q", r.tt.Texts())
	}
	if !r.tt.HasText(QuestionsFocusWorker) || r.tt.HasText(questionsVia) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestBlockedMenuAnswerReachesTheWorkerTerminal(t *testing.T) {
	r := menuRig(t)
	r.open("b-2")
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.sent) != 1 || string(r.src.sent[0]) != "1" || r.src.sentTo[0] != "t9" {
		t.Errorf("sent %q to %v", r.src.sent, r.src.sentTo)
	}
	if len(r.src.answers) != 0 {
		t.Errorf("a menu answer went to the owner store: %v", r.src.answers)
	}
	if !r.card.IsOpen() {
		t.Error("the card stays until the worker leaves blocked")
	}
}

func TestBlockedMenuDigitSendsAndEnterDoesNothing(t *testing.T) {
	r := menuRig(t)
	r.open("b-2")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.src.sent) != 0 {
		t.Fatalf("Enter sent %q", r.src.sent)
	}
	r.tt.Key(0, ui.Key2)
	if len(r.src.sent) != 1 || string(r.src.sent[0]) != "2" || r.src.sentTo[0] != "t9" {
		t.Errorf("sent %q to %v", r.src.sent, r.src.sentTo)
	}
}

func TestBlockedMenuPromptChangedSendsNothing(t *testing.T) {
	r := menuRig(t, claudeMenu, claudeMenuOther)
	r.open("b-2")
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if len(r.src.sent) != 0 || !r.tt.HasText(QuestionsPromptChanged) || !r.tt.HasText("No") {
		t.Errorf("sent %q texts %q", r.src.sent, r.tt.Texts())
	}
}

func TestBlockedMenuWithoutAMenuShowsTheMessage(t *testing.T) {
	r := menuRig(t)
	r.src.text = "nothing to see"
	r.open("b-2")
	if !r.tt.HasText("Allow rm?") || !r.tt.HasText(QuestionsFocusWorker) || r.tt.HasText("Yes") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	r = menuRig(t)
	r.src.readErr = errors.New("core gone")
	r.open("b-2")
	if !r.tt.HasText("core gone") || r.tt.HasText("Yes") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestBlockedMenuSendErrorKeepsTheCard(t *testing.T) {
	r := menuRig(t)
	r.src.sendErr = errors.New("keys failed")
	r.open("b-2")
	if err := r.tt.Click("Yes"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.card.IsOpen() || !r.tt.HasText("keys failed") {
		t.Errorf("open=%v texts %q", r.card.IsOpen(), r.tt.Texts())
	}
}

func TestBlockedMenuTextChoiceAndFocusWorkerSelectTheWorker(t *testing.T) {
	r := menuRig(t)
	var got string
	r.card.Select = func(a string, _ bool) { got = a }
	r.open("b-2")
	if err := r.tt.Click("No, and tell Claude what to do differently"); err != nil {
		t.Fatal(err)
	}
	if len(r.src.sent) != 0 || got != "t9" || r.card.IsOpen() {
		t.Errorf("sent %q, selected %q, open %v", r.src.sent, got, r.card.IsOpen())
	}
	r = menuRig(t)
	got = ""
	r.card.Select = func(a string, _ bool) { got = a }
	r.open("b-2")
	if err := r.tt.Click(QuestionsFocusWorker); err != nil {
		t.Fatal(err)
	}
	if got != "t9" || r.card.IsOpen() {
		t.Errorf("selected %q, open %v", got, r.card.IsOpen())
	}
}

func TestBlockedMenuRereadsOnANewTurnAndGoesWhenUnblocked(t *testing.T) {
	r := menuRig(t)
	r.open("b-2")
	n := len(r.src.read)
	if n != 1 {
		t.Fatalf("%d reads after open, want 1", n)
	}
	r.src.snap.Workers[0].Turn = 2
	r.tt.Frame()
	if len(r.src.read) != n+1 {
		t.Errorf("%d reads after a new turn, want %d", len(r.src.read), n+1)
	}
	r.src.snap.Workers[0].Status = "working"
	r.tt.Frame()
	r.tt.Frame()
	if r.tt.HasText("Do you want to proceed?") || r.card.key == "b-2" {
		t.Errorf("the item should leave the chips: key %q texts %q", r.card.key, r.tt.Texts())
	}
}

func TestBlockedMenuOpensTheWorkerPR(t *testing.T) {
	entry, calls := entryRig(307)
	src := &source{snap: agentstate.Snapshot{Workers: []agents.Worker{entryWorker}}}
	src.snap.Workers[0].Status = "blocked"
	r := newQRig(t, src)
	r.card.Entry = entry
	r.open(entryWorker.Name)
	if err := r.tt.Click("Open PR #307"); err != nil {
		t.Fatalf("%v; texts %q", err, r.tt.Texts())
	}
	if len(calls.prs) != 1 || calls.prs[0] != [2]any{"/wt/a", 307} || r.card.IsOpen() {
		t.Errorf("calls %v open %v", calls.prs, r.card.IsOpen())
	}
}

func TestCardOptionsPutTheRecommendedFirstWithItsImpact(t *testing.T) {
	q := askSource().snap.Questions[0]
	opts := CardOptions(q)
	if opts[0].Text != "postgres" || !opts[0].Recommended || opts[0].Impact != "Needs a server; scales out." {
		t.Errorf("first %+v", opts[0])
	}
	if opts[1].Text != "sqlite" || opts[1].Recommended || opts[1].Impact != qImpact {
		t.Errorf("second %+v", opts[1])
	}
	q.Recommended, q.Impacts = nil, nil
	if opts := CardOptions(q); opts[0].Text != "sqlite" || opts[0].Recommended || opts[0].Impact != "" {
		t.Errorf("without a recommendation: %+v", opts)
	}
}

func TestChipOfABlockedWorkerWithoutAMessageShowsItsName(t *testing.T) {
	src := askSource()
	src.snap.Workers[0].Msg = ""
	r := newQRig(t, src)
	r.open("b-2")
	if !r.tt.HasText("b-2") {
		t.Errorf("the blocked chip has no text: %q", r.tt.Texts())
	}
	if _, ok := r.tt.Find("b-2"); !ok {
		t.Error("no chip labelled with the worker name")
	}
}
