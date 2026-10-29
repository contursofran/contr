package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func answerOf(key string, option uint64, label string) Action { return Answer(key, option, label) }

func env(seq, at uint64, a Action) Envelope { return Envelope{Seq: seq, At: at, Action: a} }

func seqsOf(f ActionsFile) []uint64 {
	out := []uint64{}
	for _, e := range f.Actions {
		out = append(out, e.Seq)
	}
	return out
}

func newQ(acted uint64, existing *ActionsFile) *Outbox { return NewOutbox("s1", acted, existing) }

func fileOf(session string, actions ...Envelope) *ActionsFile {
	return &ActionsFile{Version: ActionsVersion, Session: session, Actions: actions}
}

func needItem(t *testing.T, body string) NeedItem {
	t.Helper()
	var it NeedItem
	if err := json.Unmarshal([]byte(body), &it); err != nil {
		t.Fatal(err)
	}
	return it
}

func wantEnvs(t *testing.T, got []Envelope, want ...Envelope) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("actions = %+v, want %+v", got, want)
	}
}

func wantSeqs(t *testing.T, f ActionsFile, want ...uint64) {
	t.Helper()
	if got := seqsOf(f); !slices.Equal(got, want) {
		t.Fatalf("seqs = %v, want %v", got, want)
	}
}

func wantPush(t *testing.T, q *Outbox, a Action, now uint64, wantSeq uint64, wantOK bool) {
	t.Helper()
	seq, ok := q.Push(a, now)
	if seq != wantSeq || ok != wantOK {
		t.Fatalf("Push(%+v) = %d, %v; want %d, %v", a, seq, ok, wantSeq, wantOK)
	}
}

func wantPending(t *testing.T, q *Outbox, key, text string, undoable, found bool) {
	t.Helper()
	gt, gu, gf := q.PendingAnswer(key)
	if gt != text || gu != undoable || gf != found {
		t.Fatalf("PendingAnswer(%q) = %q, %v, %v; want %q, %v, %v", key, gt, gu, gf, text, undoable, found)
	}
}

func TestEachActionSerializesToItsContractJSON(t *testing.T) {
	table := []struct {
		action Action
		json   string
	}{
		{answerOf("D7", 1, "two lines"), `{"do":"answer","key":"D7","option":1,"label":"two lines"}`},
		{Done("T12"), `{"do":"done","key":"T12"}`},
		{Dismiss("T13"), `{"do":"dismiss","key":"T13"}`},
		{Chat("D8"), `{"do":"chat","key":"D8"}`},
		{ClearRows(), `{"do":"clear_rows"}`},
		{ClearItems(), `{"do":"clear_items"}`},
		{WrapUp(true), `{"do":"wrap_up","on":true}`},
		{WrapUp(false), `{"do":"wrap_up","on":false}`},
	}
	for _, c := range table {
		if got := string(c.action.JSON()); got != c.json {
			t.Errorf("JSON(%+v) = %s, want %s", c.action, got, c.json)
		}
		var back Action
		if err := json.Unmarshal([]byte(c.json), &back); err != nil {
			t.Fatal(err)
		}
		if back != c.action {
			t.Errorf("parse %s = %+v, want %+v", c.json, back, c.action)
		}
	}
}

func TestEnvelopePutsSeqAndAtBeforeTheActionFields(t *testing.T) {
	e := env(17, 1_761_234_567_890, answerOf("D7", 1, "two lines"))
	const js = `{"seq":17,"at":1761234567890,"do":"answer","key":"D7","option":1,"label":"two lines"}`
	if got := string(e.JSON()); got != js {
		t.Fatalf("JSON = %s", got)
	}
	var back Envelope
	if err := json.Unmarshal([]byte(js), &back); err != nil || back != e {
		t.Fatalf("parse = %+v, %v", back, err)
	}
}

func TestEnvelopeOfAUnitActionParses(t *testing.T) {
	var e Envelope
	if err := json.Unmarshal([]byte(`{"seq":22,"at":1761234572000,"do":"clear_items"}`), &e); err != nil {
		t.Fatal(err)
	}
	if e != env(22, 1_761_234_572_000, ClearItems()) {
		t.Fatalf("got %+v", e)
	}
}

func TestActionsFileRoundTrips(t *testing.T) {
	f := fileOf("s1", env(17, 1, answerOf("D7", 1, "two lines")), env(18, 2, Done("T12")))
	got, ok := ParseActions(f.JSON())
	if !ok || got.Session != "s1" || !slices.Equal(got.Actions, f.Actions) {
		t.Fatalf("got %+v, %v", got, ok)
	}
}

// The fixture pins the bytes that the coordinator mod reads.
func TestActionsFileBytesMatchTheFixture(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "actions_fixture_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := fileOf("s1",
		env(17, 1_761_234_567_890, answerOf("D7", 1, "two lines")),
		env(18, 1_761_234_568_000, Done("T12")),
		env(19, 1_761_234_569_000, Dismiss("T13")),
		env(20, 1_761_234_570_000, Chat("D8")),
		env(21, 1_761_234_571_000, ClearRows()),
		env(22, 1_761_234_572_000, ClearItems()),
		env(23, 1_761_234_573_000, WrapUp(true)),
	)
	if got := f.JSON(); string(got) != string(want) {
		t.Fatalf("bytes differ:\n got %s\nwant %s", got, want)
	}
	parsed, ok := ParseActions(want)
	if !ok || !slices.Equal(parsed.Actions, f.Actions) {
		t.Fatalf("fixture does not parse back: %+v, %v", parsed, ok)
	}
}

func TestEmptyActionsFileWritesAnEmptyList(t *testing.T) {
	got := string(newQ(0, nil).File().JSON())
	if got != `{"version":1,"session":"s1","actions":[]}` {
		t.Fatalf("got %s", got)
	}
}

// serde_json escapes only quote, backslash and control characters.
func TestStringsAreEscapedLikeSerdeJSON(t *testing.T) {
	a := answerOf("D7", 0, "a<b>&\"c\"\\\n\t\x01\x7f é   \xff")
	want := "{\"do\":\"answer\",\"key\":\"D7\",\"option\":0,\"label\":\"a<b>&\\\"c\\\"\\\\\\n\\t\\u0001\x7f é   �\"}"
	if got := string(a.JSON()); got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	var back Action
	if err := json.Unmarshal(a.JSON(), &back); err != nil || back.Label != a.Label[:len(a.Label)-1]+"�" {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
}

func TestParseActionsReadsAFileWithoutActionsAsEmpty(t *testing.T) {
	got, ok := ParseActions([]byte(`{"version":1,"session":"s1"}`))
	if !ok || got.Session != "s1" || len(got.Actions) != 0 {
		t.Fatalf("got %+v, %v", got, ok)
	}
}

func TestParseActionsRejectsVersionTwo(t *testing.T) {
	if _, ok := ParseActions([]byte(`{"version":2,"session":"s1","actions":[]}`)); ok {
		t.Fatal("version 2 accepted")
	}
}

func TestParseActionsRejectsBadJSONAndAMissingVersion(t *testing.T) {
	if _, ok := ParseActions([]byte("not json")); ok {
		t.Fatal("bad json accepted")
	}
	if _, ok := ParseActions([]byte(`{"session":"s1","actions":[]}`)); ok {
		t.Fatal("missing version accepted")
	}
}

func TestParseActionsRejectsAnUnknownActionKind(t *testing.T) {
	body := `{"version":1,"session":"s1","actions":[{"seq":1,"at":1,"do":"explode"}]}`
	if _, ok := ParseActions([]byte(body)); ok {
		t.Fatal("unknown kind accepted")
	}
}

func TestParseActionsRejectsAnActionWithAMissingField(t *testing.T) {
	for _, a := range []string{
		`{"seq":1,"at":1,"do":"answer","key":"D7","label":"x"}`,
		`{"seq":1,"at":1,"do":"done"}`,
		`{"seq":1,"at":1,"do":"wrap_up"}`,
		`{"at":1,"do":"clear_rows"}`,
		`{"seq":1,"do":"clear_rows"}`,
	} {
		body := `{"version":1,"session":"s1","actions":[` + a + `]}`
		if _, ok := ParseActions([]byte(body)); ok {
			t.Errorf("accepted %s", a)
		}
	}
}

func TestActionsPathIsTheSessionFileInTheBoardDir(t *testing.T) {
	if got, want := ActionsPath("dir", "s1"), filepath.Join("dir", "s1.actions.json"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestKeyHoldsAndAnswerTextFollowTheKind(t *testing.T) {
	table := []struct {
		action Action
		key    string
		hasKey bool
		holds  bool
		text   string
		hasTxt bool
	}{
		{answerOf("D7", 1, "two lines"), "D7", true, true, "two lines", true},
		{Done("T12"), "T12", true, true, "done", true},
		{Dismiss("T13"), "T13", true, true, "dismissed", true},
		{Chat("D8"), "D8", true, false, "", false},
		{ClearRows(), "", false, false, "", false},
		{ClearItems(), "", false, false, "", false},
		{WrapUp(true), "", false, false, "", false},
	}
	for _, c := range table {
		key, hasKey := c.action.ItemKey()
		if key != c.key || hasKey != c.hasKey {
			t.Errorf("key of %+v = %q, %v", c.action, key, hasKey)
		}
		if c.action.Holds() != c.holds {
			t.Errorf("holds %+v", c.action)
		}
		text, hasTxt := c.action.AnswerText()
		if text != c.text || hasTxt != c.hasTxt {
			t.Errorf("text of %+v = %q, %v", c.action, text, hasTxt)
		}
	}
}

func TestPushHoldsAnAnswerWithoutASeqAndWritesAWrapUpAtOnce(t *testing.T) {
	q := newQ(0, nil)
	wantPush(t, q, answerOf("D7", 1, "two lines"), 1_000, 0, false)
	wantPush(t, q, WrapUp(true), 1_000, 1, true)
	wantEnvs(t, q.File().Actions, env(1, 1_000, WrapUp(true)))
	if ms, ok := q.NextRelease(); !ok || ms != 1_000+UndoMS {
		t.Fatalf("NextRelease = %d, %v", ms, ok)
	}
}

func TestAnUndoneAnswerTakesNoSeq(t *testing.T) {
	q := newQ(0, nil)
	wantPush(t, q, answerOf("D7", 1, "two lines"), 0, 0, false)
	if !q.Undo("D7") {
		t.Fatal("undo failed")
	}
	wantPush(t, q, ClearRows(), 0, 1, true)
}

func TestReleaseWaitsForTheUndoWindow(t *testing.T) {
	q := newQ(0, nil)
	q.Push(answerOf("D7", 1, "two lines"), 1_000)
	if q.Release(1_000 + UndoMS - 1) {
		t.Fatal("released early")
	}
	if len(q.File().Actions) != 0 {
		t.Fatal("written early")
	}
	if !q.Release(1_000 + UndoMS) {
		t.Fatal("not released")
	}
	wantEnvs(t, q.File().Actions, env(1, 1_000, answerOf("D7", 1, "two lines")))
	if _, ok := q.NextRelease(); ok {
		t.Fatal("still held")
	}
	if q.Release(^uint64(0)) {
		t.Fatal("released twice")
	}
}

func TestAHeldActionGetsItsSeqAfterTheWrittenOnes(t *testing.T) {
	q := newQ(0, nil)
	wantPush(t, q, Done("T12"), 0, 0, false)
	wantPush(t, q, ClearRows(), 0, 1, true)
	if !q.Release(UndoMS) {
		t.Fatal("not released")
	}
	wantEnvs(t, q.File().Actions, env(1, 0, ClearRows()), env(2, 0, Done("T12")))
}

func TestReleasedActionsGetTheirSeqsInPushOrder(t *testing.T) {
	q := newQ(0, nil)
	wantPush(t, q, Done("T12"), 0, 0, false)
	wantPush(t, q, ClearRows(), 0, 1, true)
	wantPush(t, q, answerOf("D7", 1, "two lines"), 10, 0, false)
	if !q.Release(UndoMS + 10) {
		t.Fatal("not released")
	}
	wantEnvs(t, q.File().Actions,
		env(1, 0, ClearRows()),
		env(2, 0, Done("T12")),
		env(3, 10, answerOf("D7", 1, "two lines")),
	)
}

func TestAHeldAnswerGetsASeqAboveTheActionTheModTookMeanwhile(t *testing.T) {
	q := newQ(0, nil)
	wantPush(t, q, answerOf("D7", 1, "two lines"), 0, 0, false)
	wantPush(t, q, WrapUp(true), 1_000, 1, true)
	// The mod takes the wrap up and reports acted = 1 before the answer is written.
	if !q.Ack(1) {
		t.Fatal("ack dropped nothing")
	}
	wantPending(t, q, "D7", "two lines", true, true)
	if !q.Release(UndoMS) {
		t.Fatal("not released")
	}
	wantEnvs(t, q.File().Actions, env(2, 0, answerOf("D7", 1, "two lines")))
	wantPending(t, q, "D7", "two lines", false, true)
}

func TestUndoRemovesTheLatestHeldAnswerForTheKey(t *testing.T) {
	q := newQ(0, nil)
	q.Push(answerOf("D7", 1, "two lines"), 0)
	q.Push(Done("D7"), 0)
	if !q.Undo("D7") {
		t.Fatal("undo failed")
	}
	wantPending(t, q, "D7", "two lines", true, true)
	if !q.Release(UndoMS) {
		t.Fatal("not released")
	}
	wantSeqs(t, q.File(), 1)
}

func TestUndoOfANewerHeldAnswerLeavesTheWrittenOnePending(t *testing.T) {
	q := newQ(0, nil)
	q.Push(answerOf("D7", 1, "two lines"), 0)
	if !q.Release(UndoMS) {
		t.Fatal("not released")
	}
	wantPush(t, q, Done("D7"), UndoMS, 0, false)
	wantPending(t, q, "D7", "done", true, true)
	if !q.Undo("D7") {
		t.Fatal("undo failed")
	}
	wantPending(t, q, "D7", "two lines", false, true)
}

func TestUndoRemovesOnlyAHeldAnswer(t *testing.T) {
	q := newQ(0, nil)
	q.Push(answerOf("D7", 1, "two lines"), 0)
	wantPending(t, q, "D7", "two lines", true, true)
	if !q.Undo("D7") {
		t.Fatal("undo failed")
	}
	wantPending(t, q, "D7", "", false, false)
	if q.Undo("D7") {
		t.Fatal("second undo worked")
	}
	if q.ReleaseAll() {
		t.Fatal("something was held")
	}
}

func TestUndoIsFalseAfterTheAnswerIsWritten(t *testing.T) {
	q := newQ(0, nil)
	q.Push(answerOf("D8", 0, "one line"), 0)
	q.Release(UndoMS)
	if q.Undo("D8") {
		t.Fatal("undo of a written answer")
	}
	wantSeqs(t, q.File(), 1)
	wantPending(t, q, "D8", "one line", false, true)
}

func TestUndoIsFalseForAnUnknownKey(t *testing.T) {
	q := newQ(0, nil)
	q.Push(Done("T12"), 0)
	if q.Undo("T99") {
		t.Fatal("undo of an unknown key")
	}
	if ms, ok := q.NextRelease(); !ok || ms != UndoMS {
		t.Fatalf("NextRelease = %d, %v", ms, ok)
	}
}

func TestReleaseAllWritesEveryHeldActionForQuit(t *testing.T) {
	q := newQ(0, nil)
	q.Push(Done("T12"), 0)
	q.Push(answerOf("D7", 1, "two lines"), 0)
	if !q.ReleaseAll() {
		t.Fatal("nothing written")
	}
	wantSeqs(t, q.File(), 1, 2)
	if q.ReleaseAll() {
		t.Fatal("written twice")
	}
	if _, ok := q.NextRelease(); ok {
		t.Fatal("still held")
	}
}

func TestNewKeepsExistingEntriesAboveActedAndContinuesTheSeq(t *testing.T) {
	q := newQ(17, fileOf("s1", env(16, 1, Done("T11")), env(18, 2, ClearRows())))
	if q.Session() != "s1" || q.File().Version != ActionsVersion {
		t.Fatalf("session %q version %d", q.Session(), q.File().Version)
	}
	wantSeqs(t, q.File(), 18)
	wantPush(t, q, ClearItems(), 0, 19, true)
}

func TestNewContinuesAfterTheHighestExistingSeq(t *testing.T) {
	q := newQ(17, fileOf("s1", env(40, 1, ClearRows())))
	wantPush(t, q, ClearItems(), 0, 41, true)
}

func TestNewWithoutAFileStartsAfterActed(t *testing.T) {
	wantPush(t, newQ(17, nil), WrapUp(false), 0, 18, true)
}

func TestNewIgnoresAnExistingFileOfAnotherSession(t *testing.T) {
	q := newQ(17, fileOf("other", env(30, 1, ClearRows())))
	if len(q.File().Actions) != 0 {
		t.Fatal("kept entries of another session")
	}
	wantPush(t, q, ClearItems(), 0, 18, true)
}

func TestNewSortsExistingEntriesBySeq(t *testing.T) {
	q := newQ(0, fileOf("s1", env(3, 1, ClearRows()), env(2, 1, ClearItems())))
	wantSeqs(t, q.File(), 2, 3)
}

func TestAckDropsTheTakenActionsOnly(t *testing.T) {
	q := newQ(15, fileOf("s1",
		env(16, 1, ClearRows()),
		env(17, 2, ClearItems()),
		env(18, 3, WrapUp(true)),
		env(19, 4, WrapUp(false)),
	))
	wantSeqs(t, q.File(), 16, 17, 18, 19)
	if !q.Ack(18) {
		t.Fatal("ack dropped nothing")
	}
	wantSeqs(t, q.File(), 19)
	if q.Ack(18) {
		t.Fatal("second ack dropped something")
	}
}

func failSeqs(fs []ActionFailure) []uint64 {
	out := []uint64{}
	for _, f := range fs {
		out = append(out, f.Seq)
	}
	return out
}

func TestNewFailuresReturnsEachFailureOnce(t *testing.T) {
	q := newQ(16, nil)
	d5, d7 := "D5", "D7"
	failed := []ActionFailure{
		{Seq: 16, Key: &d5, Text: "D5 is not open"},
		{Seq: 17, Key: &d7, Text: "D7 is not open"},
	}
	if got := failSeqs(q.NewFailures(failed)); !slices.Equal(got, []uint64{17}) {
		t.Fatalf("first = %v", got)
	}
	if got := q.NewFailures(failed); len(got) != 0 {
		t.Fatalf("second = %v", got)
	}
	all := append(slices.Clone(failed), ActionFailure{Seq: 18, Text: "stale"})
	if got := failSeqs(q.NewFailures(all)); !slices.Equal(got, []uint64{18}) {
		t.Fatalf("third = %v", got)
	}
}

func TestPendingAnswerGivesTheLabelAndTheUndoableFlag(t *testing.T) {
	q := newQ(0, nil)
	wantPending(t, q, "D7", "", false, false)
	q.Push(answerOf("D7", 1, "two lines"), 0)
	wantPending(t, q, "D7", "two lines", true, true)
	q.Push(Chat("D7"), 0)
	wantPending(t, q, "D7", "two lines", true, true)
	q.Push(Done("D7"), 0)
	wantPending(t, q, "D7", "done", true, true)
}

func wantOptions(t *testing.T, it NeedItem, want ...CardOption) {
	t.Helper()
	if got := CardOptions(it); !slices.Equal(got, want) {
		t.Fatalf("CardOptions = %+v, want %+v", got, want)
	}
}

func TestCardOptionsPutsTheRecommendedOneFirstWithOriginalIndices(t *testing.T) {
	it := needItem(t, `{"key":"D7","kind":"ask","options":["one line","two lines","three"],"recommended":1}`)
	wantOptions(t, it,
		CardOption{1, "two lines", true},
		CardOption{0, "one line", false},
		CardOption{2, "three", false},
	)
}

func TestCardOptionsKeepsTheOrderWhenTheFirstOneIsRecommended(t *testing.T) {
	it := needItem(t, `{"key":"D7","kind":"ask","options":["a","b"],"recommended":0}`)
	wantOptions(t, it, CardOption{0, "a", true}, CardOption{1, "b", false})
}

func TestCardOptionsMarksNoneWithoutARecommendedOption(t *testing.T) {
	it := needItem(t, `{"key":"D7","kind":"ask","options":["a","b"]}`)
	wantOptions(t, it, CardOption{0, "a", false}, CardOption{1, "b", false})
}

func TestCardOptionsMarksNoneWhenRecommendedIsOutOfRange(t *testing.T) {
	it := needItem(t, `{"key":"D7","kind":"ask","options":["a","b"],"recommended":2}`)
	wantOptions(t, it, CardOption{0, "a", false}, CardOption{1, "b", false})
}

func TestCardOptionsOfAnItemWithoutOptionsIsEmpty(t *testing.T) {
	it := needItem(t, `{"key":"D7","kind":"ask","recommended":0}`)
	if got := CardOptions(it); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// Slot order: D7 open, T12 open, D6 resolved, N4 open.
func slotItems(t *testing.T) []NeedItem {
	return []NeedItem{
		needItem(t, `{"key":"D7","kind":"ask","open":true}`),
		needItem(t, `{"key":"T12","kind":"do","open":true}`),
		needItem(t, `{"key":"D6","kind":"ask","open":false}`),
		needItem(t, `{"key":"N4","kind":"know","open":true}`),
	}
}

func isOpen(it NeedItem) bool { return it.Open }

func keyOf(it *NeedItem) string {
	if it == nil {
		return "<none>"
	}
	return it.Key
}

func wantNext(t *testing.T, items []NeedItem, key string, open func(NeedItem) bool, want string) {
	t.Helper()
	if got := keyOf(NextOpen(items, key, open)); got != want {
		t.Fatalf("NextOpen(%q) = %s, want %s", key, got, want)
	}
}

func TestNextOpenPicksTheNextOpenItemAfterTheKey(t *testing.T) {
	items := slotItems(t)
	wantNext(t, items, "D7", isOpen, "T12")
	wantNext(t, items, "T12", isOpen, "N4")
	wantNext(t, items, "D6", isOpen, "N4")
}

func TestNextOpenWrapsToTheFirstOtherOpenItem(t *testing.T) {
	wantNext(t, slotItems(t), "N4", isOpen, "D7")
}

func TestNextOpenIsNoneWhenNoOtherItemIsOpen(t *testing.T) {
	items := []NeedItem{
		needItem(t, `{"key":"D7","kind":"ask","open":true}`),
		needItem(t, `{"key":"D6","kind":"ask","open":false}`),
	}
	wantNext(t, items, "D7", isOpen, "<none>")
	wantNext(t, nil, "D7", isOpen, "<none>")
}

func TestNextOpenUsesTheGivenOpenRule(t *testing.T) {
	// A pending answer counts as not open, so N4 is skipped and the wrap finds D7.
	rule := func(it NeedItem) bool { return it.Open && it.Key != "N4" }
	wantNext(t, slotItems(t), "T12", rule, "D7")
}

func TestNextOpenForAKeyThatIsNotListedTakesTheFirstOpenItem(t *testing.T) {
	wantNext(t, slotItems(t), "X9", isOpen, "D7")
}

func TestAgoReadsSecondsMinutesHoursAndDays(t *testing.T) {
	const now uint64 = 1_761_234_567_890
	table := []struct {
		back uint64
		want string
	}{
		{30_000, "now"},
		{59_999, "now"},
		{60_000, "1m"},
		{5 * 60_000, "5m"},
		{2 * 3_600_000, "2h"},
		{3 * 86_400_000, "3d"},
	}
	for _, c := range table {
		if got := Ago(now, now-c.back); got != c.want {
			t.Errorf("Ago(now, now-%d) = %s, want %s", c.back, got, c.want)
		}
	}
}

func TestAgoReadsAFutureTimeAsNow(t *testing.T) {
	if got := Ago(1_000, 6_000); got != "now" {
		t.Fatalf("got %s", got)
	}
}
