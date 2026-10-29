package board

import (
	_ "embed"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/herdr"
)

//go:embed testdata/board_fixture_v1.json
var fixtureJSON []byte

func sp(s string) *string { return &s }

func mustParse(t *testing.T, body string) *Snapshot {
	t.Helper()
	b, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return b
}

func fixture(t *testing.T) *Snapshot {
	t.Helper()
	b, err := Parse(fixtureJSON)
	if err != nil {
		t.Fatalf("Parse fixture: %v", err)
	}
	return b
}

func row(t *testing.T, body string) WorkRow {
	t.Helper()
	var r WorkRow
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func item(t *testing.T, body string) NeedItem {
	t.Helper()
	var it NeedItem
	if err := json.Unmarshal([]byte(body), &it); err != nil {
		t.Fatal(err)
	}
	return it
}

func eq(t *testing.T, got, want any, msg ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s got %#v, want %#v", strings.Join(msg, " "), got, want)
	}
}

func isJSONErr(err error) bool {
	var e Error
	return errors.As(err, &e) && e.Kind == ErrJSON
}

func TestFixtureParsesWithItsHeader(t *testing.T) {
	b := fixture(t)
	eq(t, b.Version, uint32(1))
	eq(t, b.Session, "8c1f0f6e-3a2b-4c5d-9e8f-1a2b3c4d5e6f")
	eq(t, b.Rev, uint64(42))
	eq(t, b.Summary, "1 of 2 workers busy · 3 need you")
	eq(t, b.WrapUp, false)
	eq(t, b.NeedCount, uint32(3))
	eq(t, b.ClearRows, true)
	eq(t, b.ClearItems, true)
	eq(t, b.Context, &ContextUse{Tokens: 120_000, Limit: 300_000})
}

func TestFixtureKeepsRowAndItemOrder(t *testing.T) {
	b := fixture(t)
	var rows, items []string
	for _, r := range b.Rows {
		rows = append(rows, r.Key)
	}
	for _, i := range b.Items {
		items = append(items, i.Key)
	}
	eq(t, rows, []string{"owner/name#640", "owner/name#641", "owner/name#642"})
	eq(t, items, []string{"T12", "D7", "N4", "D6"})
}

func TestFixtureWorkingRowReadsItsWhoAndStages(t *testing.T) {
	w := fixture(t).Rows[0]
	eq(t, w.Title, "sort bags")
	eq(t, w.Who.Text, "b-1151 5m")
	eq(t, w.Who.Kind, WhoText)
	eq(t, w.Stage(0), StageDone)
	eq(t, w.Stage(1), StageRun)
	eq(t, w.Finished, false)
	eq(t, w.PaneID, sp("w1:p4"))
	eq(t, w.ReportBy, sp("b-1151"))
}

func TestFixtureYouRowHasATodoStageAndYouWho(t *testing.T) {
	y := fixture(t).Rows[1]
	eq(t, y.Who.Kind, WhoYou)
	eq(t, y.Who.Text, "your test")
	eq(t, y.Stage(4), StageTodo)
	eq(t, y.Item, sp("T12"))
}

func TestFixtureJoinedRowHasNoStages(t *testing.T) {
	j := fixture(t).Rows[2]
	eq(t, j.Joined, sp("#641"))
	eq(t, j.Finished, true)
	eq(t, len(j.Stages), 0)
	eq(t, j.Stage(0), StageNone)
}

func TestStagePastTheRowReadsNone(t *testing.T) {
	b := fixture(t)
	eq(t, len(b.Rows[0].Stages), 5)
	eq(t, b.Rows[0].Stage(7), StageNone)
}

func TestUnknownStageStringReadsUnknownAndDrawsNotStarted(t *testing.T) {
	r := row(t, `{"key":"k","stages":["done","bogus"]}`)
	eq(t, r.Stage(1), StageUnknown)
	eq(t, StageStatus(r.Stage(1)), icons.NotStarted)
}

func TestStageStringsParseToTheirVariants(t *testing.T) {
	r := row(t, `{"key":"k","stages":["done","run","wait","none","ask","todo","block"]}`)
	var got []Stage
	for i := 0; i < 7; i++ {
		got = append(got, r.Stage(i))
	}
	eq(t, got, []Stage{StageDone, StageRun, StageWait, StageNone, StageAsk, StageTodo, StageBlock})
}

func TestUnknownWhoKindReadsOther(t *testing.T) {
	r := row(t, `{"key":"k","who":{"text":"x","kind":"odd"}}`)
	eq(t, r.Who.Kind, WhoOther)
	eq(t, r.Who.Text, "x")
}

func TestUnknownItemKindReadsOtherAndDrawsAsFyi(t *testing.T) {
	it := item(t, `{"key":"N9","kind":"odd"}`)
	eq(t, it.Kind, ItemOther)
	eq(t, ItemStatus(it), icons.Fyi)
	eq(t, ItemKindLabel(it), "fyi")
}

func TestMissingOptionalFieldsTakeTheirDefaults(t *testing.T) {
	r := row(t, `{"key":"k"}`)
	eq(t, r.Label, "")
	eq(t, r.Title, "")
	eq(t, len(r.Stages), 0)
	eq(t, r.Who, Who{})
	eq(t, r.Joined, (*string)(nil))
	eq(t, r.Finished, false)
	eq(t, r.Report, (*string)(nil))

	it := item(t, `{"key":"D1","kind":"ask"}`)
	eq(t, it.Open, true)
	eq(t, it.Title, "")
	eq(t, len(it.Options), 0)
	eq(t, it.Recommended, (*uint)(nil))
}

func TestUnknownTopLevelAndRowFieldsAreIgnored(t *testing.T) {
	b := mustParse(t, `{
		"version": 1,
		"session": "s1",
		"rev": 7,
		"future": {"x": [1, 2]},
		"rows": [{"key": "k", "extra": true}]
	}`)
	eq(t, b.Rev, uint64(7))
	eq(t, len(b.Rows), 1)
	eq(t, b.Rows[0].Key, "k")
}

func TestVersionTwoIsAVersionErrorWhateverTheBody(t *testing.T) {
	_, err := Parse([]byte(`{"version": 2, "rows": "not a list"}`))
	eq(t, err, error(Error{Kind: ErrVersion, Version: 2}))
	var e Error
	if !errors.As(err, &e) {
		t.Fatal("not a board.Error")
	}
	eq(t, e.Line(), "board v2 · this contr reads v1")
}

func TestMissingVersionIsAJSONError(t *testing.T) {
	_, err := Parse([]byte(`{"session": "s1", "rev": 7}`))
	if !isJSONErr(err) {
		t.Errorf("got %v", err)
	}
}

func TestATruncatedBoardIsAJSONError(t *testing.T) {
	_, err := Parse(fixtureJSON[:len(fixtureJSON)/2])
	if !isJSONErr(err) {
		t.Errorf("got %v", err)
	}
}

func TestNonJSONIsAJSONError(t *testing.T) {
	_, err := Parse([]byte("not json"))
	if !isJSONErr(err) {
		t.Fatalf("got %v", err)
	}
	if !strings.HasPrefix(err.(Error).Line(), "board unreadable: ") {
		t.Errorf("line %q", err.(Error).Line())
	}
}

func TestJSONErrorLineCutsTheMessageToSixtyChars(t *testing.T) {
	long := Error{Kind: ErrJSON, Msg: strings.Repeat("x", 100)}
	eq(t, long.Line(), "board unreadable: "+strings.Repeat("x", 60))
	wide := Error{Kind: ErrJSON, Msg: strings.Repeat("é", 100)}
	eq(t, wide.Line(), "board unreadable: "+strings.Repeat("é", 60))
}

func TestBridgeTokenV1ShapeParses(t *testing.T) {
	got, ok := ParseBridgeToken("1 abc-DEF_9 42")
	eq(t, ok, true)
	eq(t, got, BridgeRef{Session: "abc-DEF_9", Rev: 42})
}

func TestBridgeTokenSessionOfSixtyFourCharsParses(t *testing.T) {
	session := strings.Repeat("a", 64)
	got, ok := ParseBridgeToken("1 " + session + " 0")
	eq(t, ok, true)
	eq(t, got.Session, session)
}

func TestBridgeTokenRejectsBadShapesVersionsSessionsAndRevs(t *testing.T) {
	bad := []string{
		"2 abc 1",
		"1 ../x 1",
		"1 a/b 1",
		"1 abc",
		"1 abc -1",
		"1 abc +1",
		"1 abc 1 extra",
		"1  abc 1",
		"1 abc 1 ",
		"1 abc ",
		" 1 abc 1",
		"1 abc 18446744073709551616",
		"",
		"1 " + strings.Repeat("a", 65) + " 1",
	}
	for _, token := range bad {
		if got, ok := ParseBridgeToken(token); ok {
			t.Errorf("token %q parsed as %v", token, got)
		}
	}
}

func TestBoardPathJoinsTheDirAndTheSessionJSON(t *testing.T) {
	eq(t, Path("dir", "s1"), filepath.Join("dir", "s1.json"))
}

func TestEveryStageMapsToItsStatus(t *testing.T) {
	table := []struct {
		stage  Stage
		status icons.Status
	}{
		{StageDone, icons.Done},
		{StageRun, icons.Working},
		{StageWait, icons.Waiting},
		{StageNone, icons.NotStarted},
		{StageAsk, icons.Decide},
		{StageTodo, icons.Do},
		{StageBlock, icons.Blocked},
		{StageUnknown, icons.NotStarted},
	}
	for _, c := range table {
		if got := StageStatus(c.stage); got != c.status {
			t.Errorf("stage %d: got %v, want %v", c.stage, got, c.status)
		}
	}
}

func TestOpenItemsMapToTheirStatusAndLabel(t *testing.T) {
	for _, c := range []struct {
		json   string
		status icons.Status
		label  string
	}{
		{`{"key":"D7","kind":"ask"}`, icons.Decide, "decide"},
		{`{"key":"T12","kind":"do"}`, icons.Do, "do"},
		{`{"key":"N4","kind":"know"}`, icons.Fyi, "fyi"},
	} {
		it := item(t, c.json)
		eq(t, ItemStatus(it), c.status, c.json)
		eq(t, ItemKindLabel(it), c.label, c.json)
	}
}

func TestResolvedItemMapsToDoneWhateverItsKind(t *testing.T) {
	it := item(t, `{"key":"D6","kind":"ask","open":false,"answer":"keep it"}`)
	eq(t, ItemStatus(it), icons.Done)
	eq(t, ItemKindLabel(it), "done")
}

func TestFixtureReadsItsAgents(t *testing.T) {
	b := fixture(t)
	eq(t, len(b.Agents), 2)
	eq(t, b.Agents[0].PaneID, "w1:p4")
	eq(t, b.Agents[1].Model, (*string)(nil))
}

func TestABoardWithoutAgentsReadsNone(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1}`)
	eq(t, len(b.Agents), 0)
	eq(t, len(AgentMeta(b)), 0)
}

func TestAnAgentWithoutAPaneIDIsDroppedAndTheOthersStay(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "agents": [
		{"name": "b-1"},
		{"pane_id": "p1", "name": "kept"}
	]}`)
	eq(t, len(b.Agents), 1)
	eq(t, b.Agents[0].PaneID, "p1")
	eq(t, b.Agents[0].Name, sp("kept"))
}

func TestAnAgentWithANullPaneIDIsDropped(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "agents": [
		{"pane_id": null, "name": "b-1"},
		{"pane_id": "p1"}
	]}`)
	eq(t, len(b.Agents), 1)
	eq(t, b.Agents[0].PaneID, "p1")
}

func TestWrongTypedAgentEntriesAreDropped(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "agents": [
		{"pane_id": "p0", "name": 5},
		{"pane_id": "p1", "model": ["haiku"]},
		"not an object",
		{"pane_id": "p2", "role": "builder"}
	]}`)
	eq(t, len(b.Agents), 1)
	eq(t, b.Agents[0].PaneID, "p2")
	eq(t, b.Agents[0].Role, sp("builder"))
}

func TestRowsAndItemsStillReadWhenAnAgentIsBad(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "summary": "2 open", "need_count": 1,
		"rows": [{"key": "k", "label": "#7", "pane_id": "w1:p9"}],
		"items": [{"key": "D1", "kind": "ask", "open": true, "title": "Pick one"}],
		"agents": [{"name": "b-1"}, {"pane_id": 3}]}`)
	eq(t, len(b.Agents), 0)
	eq(t, b.Summary, "2 open")
	eq(t, b.NeedCount, uint32(1))
	eq(t, len(b.Rows), 1)
	eq(t, b.Rows[0].Label, "#7")
	eq(t, len(b.Items), 1)
	eq(t, b.Items[0].Title, "Pick one")
	// The row still links its pane, with no agent entry.
	eq(t, AgentMeta(b)["w1:p9"], herdr.AgentMeta{Link: sp("#7")})
}

func TestAgentsAsAStringReadsAsAnEmptyList(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "summary": "ok", "agents": "x",
		"rows": [{"key": "k", "label": "#7"}]}`)
	eq(t, len(b.Agents), 0)
	eq(t, b.Summary, "ok")
	eq(t, len(b.Rows), 1)
}

func TestAgentsAsNullReadsAsAnEmptyList(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "summary": "ok", "agents": null,
		"rows": [{"key": "k", "label": "#7"}]}`)
	eq(t, len(b.Agents), 0)
	eq(t, b.Summary, "ok")
	eq(t, len(b.Rows), 1)
}

func TestNullAgentFieldsReadAsNotKnown(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "agents": [
		{"pane_id": "p1", "name": null, "model": "  ", "role": null}
	]}`)
	// A blank string is kept as given; the agents list treats it as missing.
	eq(t, AgentMeta(b)["p1"], herdr.AgentMeta{Model: sp("  ")})
}

func TestAgentMetaTakesNameModelAndRoleFromAgents(t *testing.T) {
	meta := AgentMeta(fixture(t))
	eq(t, len(meta), 2)
	eq(t, meta["w1:p4"], herdr.AgentMeta{
		Name: sp("b-1151"), Model: sp("haiku 5.5"), Role: sp("builder"), Link: sp("#640"),
	})
	eq(t, meta["w1:p2"], herdr.AgentMeta{
		Name: sp("r-1152"), Role: sp("reviewer"), Link: sp("#641"),
	})
}

func TestAgentMetaLinksTheOpenRowOfThePane(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "rows": [
		{"key": "k", "label": "#7", "pane_id": "w1:p9"},
		{"key": "j", "label": "#8"}
	]}`)
	meta := AgentMeta(b)
	eq(t, meta["w1:p9"], herdr.AgentMeta{Link: sp("#7")})
	// A row without a pane id gives no entry.
	eq(t, len(meta), 1)
}

func TestAgentMetaSkipsFinishedAndJoinedRows(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "rows": [
		{"key": "a", "label": "#1", "pane_id": "p1", "finished": true},
		{"key": "b", "label": "#2", "pane_id": "p1", "joined": "#9"},
		{"key": "c", "label": "#3", "pane_id": "p1"},
		{"key": "d", "label": "#5", "pane_id": "p1"},
		{"key": "e", "label": "#4", "pane_id": "p2", "finished": true}
	]}`)
	meta := AgentMeta(b)
	// The first open row in slot order wins the link.
	eq(t, meta["p1"], herdr.AgentMeta{Link: sp("#3")})
	if _, ok := meta["p2"]; ok {
		t.Error("p2 has an entry")
	}
}

func TestAgentMetaFirstEntryForAPaneWins(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "agents": [
		{"pane_id": "p1", "name": "first"},
		{"pane_id": "p1", "name": "second", "role": "planner"}
	]}`)
	eq(t, AgentMeta(b)["p1"], herdr.AgentMeta{Name: sp("first")})
}

func TestPaneMarksTakesTheFirstLiveRowPerPane(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "rows": [
		{"key": "a", "label": "#1", "pane_id": "w1:p4", "finished": true},
		{"key": "b", "label": "#2", "pane_id": "w1:p4", "who": {"text": "b-1151 5m", "kind": "you"}},
		{"key": "c", "label": "#3", "pane_id": "w1:p4"}
	]}`)
	marks := PaneMarks(b)
	eq(t, len(marks), 1)
	mark := marks["w1:p4"]
	eq(t, mark.Label, "#2")
	eq(t, mark.Who, Who{Text: "b-1151 5m", Kind: WhoYou})
}

func TestPaneMarksSkipsJoinedRowsAndRowsWithoutAPane(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "rows": [
		{"key": "a", "label": "#1", "pane_id": "p1", "joined": "#9"},
		{"key": "b", "label": "#2"},
		{"key": "c", "label": "#3", "pane_id": null},
		{"key": "d", "label": "#4", "pane_id": "p1"},
		{"key": "e", "label": "#5", "pane_id": "p2", "joined": "#8"}
	]}`)
	marks := PaneMarks(b)
	// Only the live row with a pane gives a mark: "#4" on "p1".
	eq(t, len(marks), 1)
	eq(t, marks["p1"].Label, "#4")
}

func TestPaneMarksKeepsOnlyAnOpenItem(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1,
		"items": [
			{"key": "D7", "kind": "ask", "open": true},
			{"key": "D8", "kind": "ask", "open": false}
		],
		"rows": [
			{"key": "a", "label": "#1", "pane_id": "p1", "item": "D7"},
			{"key": "b", "label": "#2", "pane_id": "p2", "item": "D8"},
			{"key": "c", "label": "#3", "pane_id": "p3", "item": "T9"}
		]}`)
	marks := PaneMarks(b)
	eq(t, marks["p1"].OpenItem, sp("D7"))
	eq(t, marks["p2"].OpenItem, (*string)(nil), "resolved")
	eq(t, marks["p3"].OpenItem, (*string)(nil), "no such item")
}

func TestPaneMarksSeesABlockedStage(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s", "rev": 1, "rows": [
		{"key": "a", "label": "#6", "pane_id": "p1", "stages": ["done", "block", "none", "none", "none"]},
		{"key": "b", "label": "#7", "pane_id": "p2", "stages": ["done", "run", "none", "none", "none"]}
	]}`)
	marks := PaneMarks(b)
	eq(t, marks["p1"].Blocked, true)
	eq(t, marks["p2"].Blocked, false)
}

func TestFixtureDeclaresTheActionFields(t *testing.T) {
	b := fixture(t)
	eq(t, b.Actions, uint32(1))
	eq(t, b.Acted, uint64(0))
	eq(t, len(b.Failed), 0)
}

func TestOldBoardWithoutActionFieldsReadsThemAsDefaults(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s1", "rev": 7}`)
	eq(t, b.Actions, uint32(0))
	eq(t, b.Acted, uint64(0))
	eq(t, len(b.Failed), 0)
}

func TestFailedEntriesReadTheirSeqKeyAndText(t *testing.T) {
	b := mustParse(t, `{
		"version": 1, "session": "s1", "rev": 7, "acted": 16,
		"failed": [
			{"seq": 15, "key": "D5", "text": "D5 is not open"},
			{"seq": 16}
		]
	}`)
	eq(t, b.Acted, uint64(16))
	eq(t, b.Failed, []ActionFailure{
		{Seq: 15, Key: sp("D5"), Text: "D5 is not open"},
		{Seq: 16},
	})
}

func TestFailedEntryWithoutSeqIsDroppedAndTheOthersStay(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s1", "rev": 7, "acted": 16, "failed": [
		{"key": "D5"},
		{"seq": 16, "key": "D7", "text": "D7 is not open"}
	]}`)
	eq(t, b.Acted, uint64(16))
	eq(t, b.Failed, []ActionFailure{{Seq: 16, Key: sp("D7"), Text: "D7 is not open"}})
}

func TestWrongTypedFailedEntriesAreDroppedAndTheBoardStillReads(t *testing.T) {
	b := mustParse(t, `{"version": 1, "session": "s1", "rev": 7, "summary": "2 open",
		"rows": [{"key": "k", "label": "#7"}],
		"failed": [
			{"seq": "15"},
			{"seq": 16, "key": 5},
			"not an object",
			{"seq": 17, "text": "kept"}
		]}`)
	eq(t, b.Summary, "2 open")
	eq(t, len(b.Rows), 1)
	eq(t, b.Failed, []ActionFailure{{Seq: 17, Text: "kept"}})
}

func TestFailedAsAStringOrNullReadsAsAnEmptyList(t *testing.T) {
	for _, value := range []string{`"x"`, "null"} {
		b := mustParse(t, `{"version": 1, "session": "s1", "rev": 7, "failed": `+value+`}`)
		eq(t, len(b.Failed), 0, "failed as "+value)
	}
}

func TestAWrongActedOrActionsValueStillFailsTheBoard(t *testing.T) {
	for _, body := range []string{
		`{"version": 1, "session": "s1", "rev": 7, "acted": "16"}`,
		`{"version": 1, "session": "s1", "rev": 7, "actions": "1"}`,
	} {
		if _, err := Parse([]byte(body)); !isJSONErr(err) {
			t.Errorf("%s: got %v", body, err)
		}
	}
}
