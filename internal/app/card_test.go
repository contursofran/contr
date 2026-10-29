package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

const row640 = "owner/name#640"

// cardFixture is the board fixture of the board package.
func cardFixture(t *testing.T) *board.Snapshot {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "board", "testdata", "board_fixture_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	snap, err := board.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func cardItems(t *testing.T, json string) []board.NeedItem {
	t.Helper()
	snap, err := board.Parse([]byte(`{"version":1,"session":"s","rev":1,"items":` + json + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return snap.Items
}

func openItem(t *testing.T, kind string, rec *uint) board.NeedItem {
	t.Helper()
	items := cardItems(t, `[{"key":"X1","kind":"`+kind+`","title":"Title","open":true,"options":["one","two"]}]`)
	items[0].Recommended = rec
	return items[0]
}

func uptr(n uint) *uint { return &n }

func treeWith(tab string, panes ...string) *herdr.HerdrTree {
	ps := make([]herdr.Pane, len(panes))
	for i, id := range panes {
		ps[i] = herdr.Pane{ID: id}
	}
	return &herdr.HerdrTree{Workspaces: []herdr.Workspace{{ID: "w1", Tabs: []herdr.Tab{{ID: "t1", Label: tab, Panes: ps}}}}}
}

func TestTheDefaultPickIsTheFirstOpenItem(t *testing.T) {
	got, ok := EffectivePick(cardFixture(t), nil)
	if !ok || got != ItemPick("T12") {
		t.Fatalf("got %v %v", got, ok)
	}
}

func TestAPickOnTheBoardIsKept(t *testing.T) {
	snap := cardFixture(t)
	for _, p := range []BoardPick{ItemPick("D7"), RowPick(row640)} {
		if got, ok := EffectivePick(snap, &p); !ok || got != p {
			t.Fatalf("got %v %v, want %v", got, ok, p)
		}
	}
}

func TestAPickWhoseKeyIsGoneFallsBackToTheFirstOpenItem(t *testing.T) {
	snap := cardFixture(t)
	for _, p := range []BoardPick{RowPick("owner/name#999"), ItemPick("D99")} {
		if got, ok := EffectivePick(snap, &p); !ok || got != ItemPick("T12") {
			t.Fatalf("got %v %v", got, ok)
		}
	}
}

func TestWithNoOpenItemAndNoPickNothingIsShown(t *testing.T) {
	snap := cardFixture(t)
	snap.Items = cardItems(t, `[{"key":"D6","kind":"ask","open":false}]`)
	if got, ok := EffectivePick(snap, nil); ok {
		t.Fatalf("got %v", got)
	}
}

func TestTheTabLabelIsTheOneOfTheTabThatHoldsThePane(t *testing.T) {
	tree := treeWith("builder", "w1:p4")
	tree.Workspaces[0].Tabs = append(tree.Workspaces[0].Tabs, herdr.Tab{ID: "t2", Label: "reviewer", Panes: []herdr.Pane{{ID: "w1:p2"}}})
	for pane, want := range map[string]string{"w1:p4": "builder", "w1:p2": "reviewer"} {
		if got, ok := TabLabelForPane(tree, pane); !ok || got != want {
			t.Fatalf("%s: got %q %v", pane, got, ok)
		}
	}
	if got, ok := TabLabelForPane(tree, "w1:p9"); ok {
		t.Fatalf("got %q", got)
	}
	if _, ok := TabLabelForPane(nil, "w1:p4"); ok {
		t.Fatal("nil tree")
	}
}

func TestTheItemMetaLeavesOutWhatTheItemLacks(t *testing.T) {
	const now = 1_000_000_000
	for _, c := range []struct{ json, want string }{
		{`{"key":"D7","kind":"ask","from":"p-7","at":700000000}`, "p-7 · 3d"},
		{`{"key":"D7","kind":"ask","at":999700000}`, "5m"},
		{`{"key":"D7","kind":"ask","from":"p-7"}`, "p-7"},
		{`{"key":"D7","kind":"ask","from":""}`, ""},
	} {
		items := cardItems(t, "["+c.json+"]")
		if got := ItemMeta(items[0], now); got != c.want {
			t.Errorf("%s: got %q, want %q", c.json, got, c.want)
		}
	}
}

func TestALinkWithoutASchemeGetsHttp(t *testing.T) {
	if got := LinkURL("example.com/pull/41"); got != "http://example.com/pull/41" {
		t.Error(got)
	}
	if got := LinkURL("https://example.com/pull/41"); got != "https://example.com/pull/41" {
		t.Error(got)
	}
}

func TestTheRecommendedOptionIsFilledAndTheOthersAreOutlined(t *testing.T) {
	if OptionStyle(true) != ButtonFilled || OptionStyle(false) != ButtonOutlined {
		t.Fatal("styles")
	}
}

func TestAQueuedAnswerShowsTheItemResolvedWithIt(t *testing.T) {
	item := cardItems(t, `[{"key":"D7","kind":"ask"}]`)[0]
	answer := "two lines per row"
	q := ShownItem(item, &answer)
	if q.Open || q.Answer == nil || *q.Answer != answer {
		t.Fatalf("%+v", q)
	}
	if !item.Open || item.Answer != nil {
		t.Fatal("the original changed")
	}
	if n := ShownItem(item, nil); n.Open != item.Open || n.Answer != nil {
		t.Fatal("without a queued answer, nothing changes")
	}
}

func TestAResolvedItemWithoutAnAnswerShowsDone(t *testing.T) {
	items := cardItems(t, `[{"key":"D6","kind":"ask","open":false,"answer":"keep"},{"key":"D6","kind":"ask","open":false}]`)
	if ResolvedAnswer(items[0]) != "keep" || ResolvedAnswer(items[1]) != "done" {
		t.Fatal("answers")
	}
}

func TestTheCardItemIsThePickedItemOnly(t *testing.T) {
	items := cardFixture(t).Items
	p := ItemPick("N4")
	if it := CardItem(items, &p); it == nil || it.Key != "N4" {
		t.Fatal("N4")
	}
	for _, p := range []BoardPick{ItemPick("D99"), RowPick(row640)} {
		if CardItem(items, &p) != nil {
			t.Fatalf("%v", p)
		}
	}
	if CardItem(items, nil) != nil {
		t.Fatal("nil")
	}
}

func TestDownStepsThroughTheItemsAndStopsAtTheLast(t *testing.T) {
	items := cardFixture(t).Items
	p := ItemPick("T12")
	if got, ok := SteppedPick(items, &p, true); !ok || got != ItemPick("D7") {
		t.Fatal(got, ok)
	}
	p = ItemPick("D6")
	if _, ok := SteppedPick(items, &p, true); ok {
		t.Fatal("D6 is the last item")
	}
}

func TestUpStepsBackAndStopsAtTheFirst(t *testing.T) {
	items := cardFixture(t).Items
	p := ItemPick("D7")
	if got, ok := SteppedPick(items, &p, false); !ok || got != ItemPick("T12") {
		t.Fatal(got, ok)
	}
	p = ItemPick("T12")
	if _, ok := SteppedPick(items, &p, false); ok {
		t.Fatal("T12 is the first item")
	}
}

func TestDownFromARowOrNoPickGoesToTheFirstItemAndUpDoesNotMove(t *testing.T) {
	items := cardFixture(t).Items
	row := RowPick(row640)
	if got, ok := SteppedPick(items, &row, true); !ok || got != ItemPick("T12") {
		t.Fatal(got, ok)
	}
	if _, ok := SteppedPick(items, &row, false); ok {
		t.Fatal("up from a row")
	}
	if got, ok := SteppedPick(items, nil, true); !ok || got != ItemPick("T12") {
		t.Fatal(got, ok)
	}
	if _, ok := SteppedPick(items, nil, false); ok {
		t.Fatal("up with no pick")
	}
}

func TestWithNoItemsNoKeyMovesThePick(t *testing.T) {
	if _, ok := SteppedPick(nil, nil, true); ok {
		t.Fatal("down")
	}
	p := ItemPick("D7")
	if _, ok := SteppedPick(nil, &p, false); ok {
		t.Fatal("up")
	}
}

func TestEnterSendsTheRecommendedOptionOfAnOpenDecision(t *testing.T) {
	items := cardFixture(t).Items
	got, ok := EnterAction(items[1])
	if !ok || got != board.Answer("D7", 0, "one line per row") {
		t.Fatal(got, ok)
	}
}

func TestEnterSendsNothingForADecisionWithoutAUsableRecommendation(t *testing.T) {
	if _, ok := EnterAction(openItem(t, "ask", nil)); ok {
		t.Error("no recommendation")
	}
	if _, ok := EnterAction(openItem(t, "ask", uptr(7))); ok {
		t.Error("out of range")
	}
}

func TestEnterSendsDoneForAToDoAndDismissForNews(t *testing.T) {
	items := cardFixture(t).Items
	if got, ok := EnterAction(items[0]); !ok || got != board.Done("T12") {
		t.Error(got, ok)
	}
	if got, ok := EnterAction(items[2]); !ok || got != board.Dismiss("N4") {
		t.Error(got, ok)
	}
	if got, ok := EnterAction(openItem(t, "weird", nil)); !ok || got != board.Dismiss("X1") {
		t.Error("an unknown kind reads as news", got, ok)
	}
}

func TestEnterSendsNothingForAResolvedItem(t *testing.T) {
	if _, ok := EnterAction(cardFixture(t).Items[3]); ok {
		t.Fatal("D6 is resolved")
	}
}

func TestADigitSendsTheOptionAtItsPlaceInCardOrder(t *testing.T) {
	item := openItem(t, "ask", uptr(0))
	if got, ok := OptionAction(item, 0); !ok || got != board.Answer("X1", 0, "one") {
		t.Error(got, ok)
	}
	if got, ok := OptionAction(item, 1); !ok || got != board.Answer("X1", 1, "two") {
		t.Error(got, ok)
	}
	if _, ok := OptionAction(item, 2); ok {
		t.Error("there are two options")
	}
	item.Recommended = uptr(1)
	if got, ok := OptionAction(item, 0); !ok || got != board.Answer("X1", 1, "two") {
		t.Error("the recommended option comes first", got, ok)
	}
}

func TestADigitSendsNothingForAToDoNewsOrAResolvedDecision(t *testing.T) {
	items := cardFixture(t).Items
	if _, ok := OptionAction(items[0], 0); ok {
		t.Error("to-do")
	}
	if _, ok := OptionAction(items[2], 0); ok {
		t.Error("news")
	}
	resolved := openItem(t, "ask", uptr(0))
	resolved.Open = false
	if _, ok := OptionAction(resolved, 0); ok {
		t.Error("resolved")
	}
}

func TestTheItemCardLabelNamesTheItemItsKindAndItsOptions(t *testing.T) {
	items := cardFixture(t).Items
	decide := ItemActions{Kind: ActionsDecide, Options: []OptionFace{
		{Label: "one line per row", Recommended: true}, {Label: "two lines per row"},
	}}
	for _, c := range []struct {
		item    board.NeedItem
		actions ItemActions
		want    string
	}{
		{items[1], decide, "D7 Row layout, decide, options one line per row, two lines per row"},
		{items[0], ItemActions{Kind: ActionsDo}, "T12 Test the bag drag, do"},
		{items[2], ItemActions{Kind: ActionsKnow}, "N4 CI is green on linux-pc, fyi"},
	} {
		if got := ItemCardLabel(c.item, c.actions); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

// View tests.

// cardClicks records the card clicks by name.
type cardClicks struct{ calls []string }

func (r *cardClicks) fn(name string) ClickFn { return func() { r.calls = append(r.calls, name) } }

func cardTester(face CardFace) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(theme.Color(theme.BgWindow)).Font(theme.FontFamily).
			FontSize(theme.FontSize).FixedLineHeight(theme.LineHeight).
			TextColor(theme.Color(theme.Text)).Justify(ui.End).Children(func() {
			AnswerCard(c, face, 1_761_234_867_890)
		})
	}, 380, 500)
}

func decideFace(t *testing.T, r *cardClicks, inert bool) *ItemFace {
	item := cardFixture(t).Items[1]
	item.Recommended = uptr(1)
	opts := board.CardOptions(item)
	faces := make([]OptionFace, len(opts))
	for i, o := range opts {
		faces[i] = OptionFace{Label: o.Text, Recommended: o.Recommended}
		if !inert {
			faces[i].Click = r.fn("opt" + o.Text)
		}
	}
	a := ItemActions{Kind: ActionsDecide, Options: faces}
	f := &ItemFace{Item: item, Actions: a, Inert: inert, NextKey: "N4", NextClick: r.fn("next")}
	if !inert {
		f.Actions.Chat = r.fn("chat")
	}
	return f
}

func hasAll(t *testing.T, tt *ui.Tester, want ...string) {
	t.Helper()
	tt.Frame()
	for _, w := range want {
		if _, ok := tt.Find(w); !ok {
			t.Errorf("missing %q in %v", w, tt.Texts())
		}
	}
}

func hasNone(t *testing.T, tt *ui.Tester, not ...string) {
	t.Helper()
	tt.Frame()
	for _, w := range not {
		if _, ok := tt.Find(w); ok {
			t.Errorf("unexpected %q", w)
		}
	}
}

func TestTheCardIs168TallAndEndsOneMarginAboveThePanelBottom(t *testing.T) {
	tt := cardTester(nil)
	tt.Frame()
	r, ok := tt.Find(LabelCard)
	if !ok {
		t.Fatal("the frame is not drawn")
	}
	if r.H != theme.AnswerCardHeight {
		t.Errorf("height %v", r.H)
	}
	if r.Y+r.H != 500-theme.AnswerMargin || r.X != theme.AnswerMargin {
		t.Errorf("rect %+v", r)
	}
}

func TestWithNothingPickedOrOpenTheCardIsEmpty(t *testing.T) {
	tt := cardTester(nil)
	tt.Frame()
	for _, s := range tt.Texts() {
		if s != "" && s != LabelCard {
			t.Fatalf("something in it: %q", s)
		}
	}
}

func TestADecideItemShowsItsOptionsThenAnswerInChat(t *testing.T) {
	var r cardClicks
	tt := cardTester(decideFace(t, &r, false))
	hasAll(t, tt, "two lines per row", "one line per row", AnswerInChat, "Next: N4", "D7  Row layout", "p-7 · 5m")
	first, _ := tt.Find("two lines per row")
	second, _ := tt.Find("one line per row")
	chat, _ := tt.Find(AnswerInChat)
	before := func(a, b ui.Rect) bool { return a.Y < b.Y || (a.Y == b.Y && a.X < b.X) }
	if !before(first, second) {
		t.Error("the recommended option is first")
	}
	if !before(second, chat) {
		t.Error("Answer in chat comes last")
	}
	hasNone(t, tt, CardDone, CardDismiss, CardInertNote)
	if err := tt.Click("two lines per row"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(AnswerInChat); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Next: N4"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"opttwo lines per row", "chat", "next"}; !slices.Equal(r.calls, want) {
		t.Errorf("calls %v", r.calls)
	}
}

func TestAToDoShowsDoneAndItsLink(t *testing.T) {
	var r cardClicks
	item := cardFixture(t).Items[0]
	face := &ItemFace{Item: item, Actions: ItemActions{Kind: ActionsDo, Done: r.fn("done"), Link: r.fn("link")}}
	tt := cardTester(face)
	hasAll(t, tt, CardDone, CardOpenLink)
	hasNone(t, tt, CardDismiss, AnswerInChat, "Next: D7")
	_ = tt.Click(CardOpenLink)
	_ = tt.Click(CardDone)
	if want := []string{"link", "done"}; !slices.Equal(r.calls, want) {
		t.Errorf("calls %v", r.calls)
	}
}

func TestAToDoWithoutALinkHasNoOpenLink(t *testing.T) {
	var r cardClicks
	face := &ItemFace{Item: cardFixture(t).Items[0], Actions: ItemActions{Kind: ActionsDo, Done: r.fn("done")}}
	tt := cardTester(face)
	hasAll(t, tt, CardDone)
	hasNone(t, tt, CardOpenLink)
}

func TestANewsItemShowsDismiss(t *testing.T) {
	var r cardClicks
	face := &ItemFace{Item: cardFixture(t).Items[2], Actions: ItemActions{Kind: ActionsKnow, Dismiss: r.fn("dismiss")}}
	tt := cardTester(face)
	hasAll(t, tt, CardDismiss)
	hasNone(t, tt, CardDone)
	_ = tt.Click(CardDismiss)
	if !slices.Equal(r.calls, []string{"dismiss"}) {
		t.Errorf("calls %v", r.calls)
	}
}

func TestWithoutAnotherOpenItemThereIsNoNext(t *testing.T) {
	face := &ItemFace{Item: cardFixture(t).Items[0], Actions: ItemActions{Kind: ActionsDo, Done: func() {}}}
	hasNone(t, cardTester(face), "Next: D7")
}

func TestAResolvedItemShowsItsAnswerAndNoUndo(t *testing.T) {
	item := cardFixture(t).Items[3]
	face := &ItemFace{Item: item, Actions: ItemActions{Kind: ActionsResolved, Answer: ResolvedAnswer(item)}}
	tt := cardTester(face)
	hasAll(t, tt, "✓ "+ResolvedAnswer(item))
	hasNone(t, tt, CardUndo, CardSending, CardDone)
}

func TestAnAnswerQueuedShowsUndoThenSendingOnceWritten(t *testing.T) {
	var r cardClicks
	item := cardFixture(t).Items[1]
	face := &ItemFace{Item: item, Actions: ItemActions{Kind: ActionsResolved, Answer: "two lines per row", Undo: r.fn("undo")}}
	tt := cardTester(face)
	hasAll(t, tt, "✓ two lines per row", CardUndo)
	hasNone(t, tt, CardSending, "two lines per row, options")
	_ = tt.Click(CardUndo)
	if !slices.Equal(r.calls, []string{"undo"}) {
		t.Errorf("calls %v", r.calls)
	}
	face.Actions.Undo, face.Actions.Sending = nil, true
	tt = cardTester(face)
	hasAll(t, tt, CardSending)
	hasNone(t, tt, CardUndo)
}

func TestAnInertBoardDimsTheItemButtonsAndDoesNothing(t *testing.T) {
	var r cardClicks
	face := decideFace(t, &r, true)
	tt := cardTester(face)
	hasAll(t, tt, CardInertNote, "two lines per row", AnswerInChat)
	for _, b := range []string{"two lines per row", AnswerInChat} {
		if err := tt.Click(b); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.calls) != 0 {
		t.Errorf("an inert button called %v", r.calls)
	}
	// Next still picks.
	_ = tt.Click("Next: N4")
	if !slices.Equal(r.calls, []string{"next"}) {
		t.Errorf("calls %v", r.calls)
	}
}

func rowFace(t *testing.T, r *cardClicks, i int, focus, report, pr bool) *RowFace {
	f := &RowFace{Row: cardFixture(t).Rows[i]}
	if focus {
		f.FocusTab, f.FocusClick = "builder", r.fn("focus")
	}
	if report {
		f.ReportClick = r.fn("report")
	}
	if pr {
		f.OpenPRClick = r.fn("pr")
	}
	return f
}

func TestAWorkRowShowsItsButtons(t *testing.T) {
	var r cardClicks
	tt := cardTester(rowFace(t, &r, 0, true, true, true))
	hasAll(t, tt, "#640  sort bags", "b-1151 (builder) is working (5m).", "Focus builder", CardReport, CardOpenPR)
	hasNone(t, tt, CardDone, "p-7")
	for _, b := range []string{"Focus builder", CardReport, CardOpenPR} {
		if err := tt.Click(b); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"focus", "report", "pr"}; !slices.Equal(r.calls, want) {
		t.Errorf("calls %v", r.calls)
	}
}

func TestARowWithoutPaneUrlOrReportHasNoneOfItsButtons(t *testing.T) {
	var r cardClicks
	tt := cardTester(rowFace(t, &r, 2, false, false, false))
	hasAll(t, tt, "#642  tab order")
	hasNone(t, tt, "Focus builder", CardReport, CardOpenPR)
}

func TestARowShowsOnlyTheButtonsThatApply(t *testing.T) {
	var r cardClicks
	tt := cardTester(rowFace(t, &r, 1, false, false, true))
	hasAll(t, tt, CardOpenPR)
	hasNone(t, tt, "Focus builder", CardReport)
}
