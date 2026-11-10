package chatview

import (
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/transcript"
)

// Transcript lines of the tests.
const (
	lPrompt  = `{"type":"user","uuid":"u1","timestamp":"2026-11-10T10:00:00Z","message":{"content":"start builders for 609"}}`
	lHook    = `{"type":"user","uuid":"u2","message":{"content":"<user-prompt-submit-hook>check the board</user-prompt-submit-hook>"}}`
	lMeta    = `{"type":"user","uuid":"u3","isMeta":true,"message":{"content":"Caveat: local commands"}}`
	lThink   = `{"type":"assistant","uuid":"a0","message":{"content":[{"type":"thinking","thinking":"hm"}]}}`
	lSaid    = `{"type":"assistant","uuid":"a1","message":{"content":[{"type":"text","text":"| PR | state |\n|---|---|\n| #633 | open |"}]}}`
	lCall1   = `{"type":"assistant","uuid":"a2","timestamp":"2026-11-10T10:00:01Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"git log --oneline -3"}}]}}`
	lCall2   = `{"type":"assistant","uuid":"a3","timestamp":"2026-11-10T10:00:02Z","message":{"content":[{"type":"tool_use","id":"t2","name":"mcp__plugin_contr_contr__send","input":{}}]}}`
	lRes1    = `{"type":"user","uuid":"u4","timestamp":"2026-11-10T10:00:03.5Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`
	lRes2Err = `{"type":"user","uuid":"u5","timestamp":"2026-11-10T10:00:04Z","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"no"}]}}`
	lNote1   = `{"type":"user","uuid":"u6","message":{"content":"<hc from=\"w2:t1 b-631\">b-631: done, PR #633, check ok</hc>"}}`
	lNote2   = `{"type":"user","uuid":"u7","message":{"content":"<hc from=\"w2:t2 b-632\">ended its turn without a report. Last words: waiting</hc>"}}`
	lSaid2   = `{"type":"assistant","uuid":"a4","message":{"content":[{"type":"text","text":"b-631 is done."}]}}`
	lSide    = `{"type":"assistant","uuid":"s1","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent text"}]}}`
)

// apply reads lines with r and applies the batch to m.
func apply(t *testing.T, m *Model, r *transcript.Reader, lines ...string) {
	t.Helper()
	b, err := r.Read(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	m.Apply(b)
}

func kinds(m *Model) []RowKind {
	var out []RowKind
	for _, r := range m.Rows() {
		out = append(out, r.Kind)
	}
	return out
}

func sameKinds(a, b []RowKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGrouping(t *testing.T) {
	var m Model
	apply(t, &m, &transcript.Reader{}, lPrompt, lHook, lMeta, lThink, lSaid, lCall1, lNote1, lCall2, lNote2, lSaid2, lSide)
	want := []RowKind{RowPrompt, RowSaid, RowGroup, RowOwnerNote, RowSaid}
	if got := kinds(&m); !sameKinds(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	g := m.Rows()[2]
	if len(g.Calls) != 2 || len(g.Notes) != 1 || GroupText(len(g.Notes), len(g.Calls)) != "1 worker note · 2 tool calls" {
		t.Errorf("group = %+v", g)
	}
	if s := m.Senders(g); len(s) != 1 || s[0] != "w2:t1" {
		t.Errorf("senders = %v", s)
	}
	if !m.Running(g) {
		t.Error("calls without a result: the group must run")
	}
	if e := m.Entry(m.Rows()[3].Entry); e.Note == nil || e.Note.Name != "b-632" {
		t.Errorf("owner note = %+v", e)
	}
}

func TestResultJoinsItsCallAcrossBatches(t *testing.T) {
	var m Model
	r := &transcript.Reader{}
	apply(t, &m, r, lPrompt, lCall1, lCall2)
	g := m.Rows()[1]
	apply(t, &m, r, lRes1)
	if c := m.Entry(g.Calls[0]).Tool; !c.Done || c.Error {
		t.Fatalf("call 1 = %+v", c)
	}
	if !m.Running(m.Rows()[1]) {
		t.Error("call 2 has no result yet")
	}
	if d, ok := Duration(m.Entry(g.Calls[0])); !ok || d != 2500*time.Millisecond {
		t.Errorf("duration = %v %v", d, ok)
	}
	apply(t, &m, r, lRes2Err)
	if c := m.Entry(g.Calls[1]).Tool; !c.Done || !c.Error {
		t.Errorf("call 2 = %+v", c)
	}
	if m.Running(m.Rows()[1]) {
		t.Error("all calls done: the group must not run")
	}
	if len(m.Rows()) != 2 {
		t.Errorf("a result adds no row: %v", kinds(&m))
	}
}

func TestResultFindsItsCallByID(t *testing.T) {
	var m Model
	apply(t, &m, &transcript.Reader{}, lCall1)
	// A wrong index falls back to the call id.
	m.Apply(transcript.Batch{Results: []transcript.Result{{Index: 7, ID: "t1", Time: "2026-11-10T10:00:02Z"}}})
	if c := m.Entry(0).Tool; !c.Done || c.End != "2026-11-10T10:00:02Z" {
		t.Errorf("call = %+v", c)
	}
	m.Apply(transcript.Batch{Results: []transcript.Result{{Index: 0, ID: "nobody"}}})
}

func TestAppendKeepsRowsAndIDs(t *testing.T) {
	var m Model
	r := &transcript.Reader{}
	apply(t, &m, r, lPrompt, lCall1)
	first := m.Rows()[1].ID
	apply(t, &m, r, lCall2, lNote1)
	if len(m.Rows()) != 2 || m.Rows()[1].ID != first || len(m.Rows()[1].Calls) != 2 {
		t.Fatalf("a later call joins the open group: %+v", m.Rows())
	}
	apply(t, &m, r, lSaid2, lCall1)
	if got := kinds(&m); !sameKinds(got, []RowKind{RowPrompt, RowGroup, RowSaid, RowGroup}) {
		t.Errorf("a text closes the group: %v", got)
	}
	if m.Rows()[3].ID == first {
		t.Error("two groups share an id")
	}
}

func TestBlocksOfOneLineGetTheirOwnIDs(t *testing.T) {
	var m Model
	line := `{"type":"assistant","uuid":"a9","message":{"content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]}}`
	apply(t, &m, &transcript.Reader{}, line)
	if rs := m.Rows(); len(rs) != 2 || rs[0].ID == rs[1].ID {
		t.Errorf("rows = %+v", rs)
	}
}

func TestReset(t *testing.T) {
	var m Model
	apply(t, &m, &transcript.Reader{}, lPrompt, lCall1)
	m.Reset()
	if m.Len() != 0 || len(m.Rows()) != 0 {
		t.Fatalf("after reset: %d entries, %d rows", m.Len(), len(m.Rows()))
	}
	apply(t, &m, &transcript.Reader{}, lPrompt, lCall1)
	if got := kinds(&m); !sameKinds(got, []RowKind{RowPrompt, RowGroup}) {
		t.Errorf("read again = %v", got)
	}
}

func TestNeedsOwner(t *testing.T) {
	note := func(kind transcript.NoteKind, text string) transcript.Entry {
		return transcript.Entry{Kind: transcript.Note, Text: text, Note: &transcript.KitNote{Kind: kind, Name: "b-1"}}
	}
	for text, want := range map[string]bool{
		"ended its turn without a report. Last words: x": true,
		"b-1: blocked, waits on a permission":            true,
		"blocked: permission":                            true,
		"b-1: done, PR #3":                               false,
		"ended its turn; it may have sent a report":      false,
		"the board is not blocked":                       false,
		"b-1 says: blocked":                              false,
	} {
		if got := NeedsOwner(note(transcript.NoteMessage, text)); got != want {
			t.Errorf("NeedsOwner(%q) = %v, want %v", text, got, want)
		}
	}
	if NeedsOwner(note(transcript.NoteAnswered, "blocked")) {
		t.Error("an owner answer never needs the owner")
	}
	if NeedsOwner(transcript.Entry{Kind: transcript.Said, Text: "blocked"}) {
		t.Error("a text of Claude is no note")
	}
}

func TestTexts(t *testing.T) {
	for _, c := range []struct {
		notes, calls int
		want         string
	}{{3, 6, "3 worker notes · 6 tool calls"}, {1, 0, "1 worker note"}, {0, 1, "1 tool call"}, {0, 0, ""}} {
		if got := GroupText(c.notes, c.calls); got != c.want {
			t.Errorf("GroupText(%d, %d) = %q", c.notes, c.calls, got)
		}
	}
	for d, want := range map[time.Duration]string{400 * time.Millisecond: "0.4s", 12 * time.Second: "12s", 125 * time.Second: "2m 5s"} {
		if got := DurationText(d); got != want {
			t.Errorf("DurationText(%v) = %q, want %q", d, got, want)
		}
	}
	for name, want := range map[string]string{"Bash": "Bash", "mcp__plugin_contr_contr__send": "contr", "mcp__github__x": "github"} {
		if got := ToolName(name); got != want {
			t.Errorf("ToolName(%q) = %q", name, got)
		}
	}
	for _, e := range []transcript.Entry{
		{Time: "2026-11-10T10:00:00Z", Tool: &transcript.Call{}},
		{Time: "x", Tool: &transcript.Call{Done: true, End: "2026-11-10T10:00:00Z"}},
		{Time: "2026-11-10T10:00:05Z", Tool: &transcript.Call{Done: true, End: "2026-11-10T10:00:00Z"}},
	} {
		if _, ok := Duration(e); ok {
			t.Errorf("Duration(%+v) has a value", e)
		}
	}
}
