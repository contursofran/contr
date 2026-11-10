package transcript

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Transcript lines in the shape Claude Code writes. No line comes from a
// real transcript.
const (
	lPrompt    = `{"type":"user","uuid":"u1","timestamp":"2026-11-10T10:00:00.000Z","message":{"role":"user","content":"fix the bug"}}`
	lImage     = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"look at this"},{"type":"image","source":{"type":"base64","data":"AAAA"}}]}}`
	lSaid      = `{"type":"assistant","uuid":"a1","timestamp":"2026-11-10T10:00:01.000Z","message":{"content":[{"type":"text","text":"I **will** fix it."}]}}`
	lThinking  = `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"secret","signature":"x"}]}}`
	lCall      = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./...\necho done","description":"Run tests"}}]}}`
	lResult    = `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok\nall passed"}]}}`
	lErrResult = `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":[{"type":"text","text":"exit 1"}]}]}}`
	lHC        = `{"type":"user","message":{"content":"<hc from=\"t7 b1\">b1: done, PR #5</hc>"}}`
	lAnswered  = `{"type":"user","message":{"content":"Owner answered q3: use plan A\n"}}`
	lDidHC     = `{"type":"user","message":{"content":"<hc from=\"t2 contr\">Owner did td4: merged it</hc>"}}`
	lMeta      = `{"type":"user","isMeta":true,"message":{"content":"Caveat: local commands"}}`
	lReminder  = `{"type":"user","message":{"content":[{"type":"text","text":"<system-reminder>be brief</system-reminder>"}]}}`
	lLocal     = `{"type":"user","message":{"content":"<local-command-stdout>Set model</local-command-stdout>"}}`
	lSysLocal  = `{"type":"system","subtype":"local_command","content":"<command-name>/model</command-name>"}`
	lSystem    = `{"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}`
	lCompact   = `{"type":"user","isCompactSummary":true,"message":{"content":"This session is being continued…"}}`
	lHook      = `{"type":"attachment","attachment":{"type":"hook_additional_context","hookEvent":"UserPromptSubmit","content":["add it to your todos"]}}`
	lPlaybook  = `{"type":"attachment","attachment":{"type":"hook_additional_context","hookEvent":"SessionStart","content":["contr: you are the coordinator of this project; …\nThe playbook:\n\n…"]}}`
	lPlaybookU = `{"type":"user","message":{"content":"contr: the conversation was compacted. You are the coordinator"}}`
	lInterrupt = `{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`
	lCommand   = `{"type":"user","message":{"content":"<command-message>contr:orchestrate</command-message>\n<command-name>/contr:orchestrate</command-name>\n<command-args>ship #5</command-args>"}}`
	lSide      = `{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"sub"}]}}`
	lMCP       = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"mcp__plugin_contr_contr__message","input":{"name":"t1"}}]}}`
	lRead      = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"/x/a.go","limit":4}}]}}`
	lGrep      = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t4","name":"Grep","input":{"pattern":"func main","path":"/x"}}]}}`
	lUnknown   = `{"type":"file-history-snapshot","snapshot":{}}`
	lNewField  = `{"type":"assistant","brandNew":{"a":[1]},"message":{"content":[{"type":"text","text":"hi","future":true},{"type":"future_block"}]}}`
)

func read(t *testing.T, lines ...string) Batch {
	t.Helper()
	var r Reader
	b, err := r.Read(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func one(t *testing.T, line string) Entry {
	t.Helper()
	b := read(t, line)
	if len(b.Entries) != 1 {
		t.Fatalf("%s: %d entries, want 1: %+v", line, len(b.Entries), b.Entries)
	}
	return b.Entries[0]
}

func TestPrompt(t *testing.T) {
	e := one(t, lPrompt)
	if e.Kind != Prompt || e.Text != "fix the bug" || e.Images != 0 || e.UUID != "u1" || e.Time != "2026-11-10T10:00:00.000Z" || e.Sidechain {
		t.Errorf("prompt = %+v", e)
	}
	e = one(t, lImage)
	if e.Kind != Prompt || e.Text != "look at this" || e.Images != 1 {
		t.Errorf("image prompt = %+v", e)
	}
	e = one(t, lCommand)
	if e.Kind != Prompt || e.Text != "/contr:orchestrate ship #5" {
		t.Errorf("slash command = %+v", e)
	}
}

func TestClaude(t *testing.T) {
	e := one(t, lSaid)
	if e.Kind != Said || e.Text != "I **will** fix it." || e.UUID != "a1" {
		t.Errorf("said = %+v", e)
	}
	e = one(t, lThinking)
	if e.Kind != Thinking || e.Text != "" {
		t.Errorf("thinking = %+v", e)
	}
	if e := one(t, lSide); !e.Sidechain || e.Kind != Said {
		t.Errorf("sidechain = %+v", e)
	}
}

func TestToolCall(t *testing.T) {
	b := read(t, lCall, lResult)
	if len(b.Entries) != 1 || len(b.Results) != 0 {
		t.Fatalf("batch = %+v", b)
	}
	c := b.Entries[0].Tool
	want := Call{ID: "t1", Name: "Bash", Summary: "go test ./... …", Done: true, Excerpt: "ok\nall passed"}
	if b.Entries[0].Kind != Tool || *c != want {
		t.Errorf("call = %+v, want %+v", *c, want)
	}
	c = read(t, lCall, lErrResult).Entries[0].Tool
	if !c.Done || !c.Error || c.Excerpt != "exit 1" {
		t.Errorf("error result = %+v", *c)
	}
	c = one(t, lCall).Tool
	if c.Done || c.Error {
		t.Errorf("no result yet = %+v", *c)
	}
	long := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t5","name":"Bash","input":{"command":"` + strings.Repeat("a", 300) + `"}}]}}`
	for line, want := range map[string]string{lMCP: "message", lRead: "/x/a.go", lGrep: "func main", long: strings.Repeat("a", summaryMax) + "…",
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t6","name":"mcp__solo","input":{}}]}}`: "solo"} {
		if got := one(t, line).Tool.Summary; got != want {
			t.Errorf("summary of %s = %q, want %q", line, got, want)
		}
	}
}

func TestExcerptIsShort(t *testing.T) {
	long := strings.Repeat("é", 5000)
	b := read(t, lCall, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"`+long+`"}]}}`)
	if got := b.Entries[0].Tool.Excerpt; got != strings.Repeat("é", excerptMax)+"…" {
		t.Errorf("excerpt has %d runes", len([]rune(got)))
	}
}

func TestNotes(t *testing.T) {
	cases := []struct {
		line string
		text string
		want KitNote
	}{
		{lHC, "b1: done, PR #5", KitNote{Kind: NoteMessage, Addr: "t7", Name: "b1"}},
		{lAnswered, "Owner answered q3: use plan A", KitNote{Kind: NoteAnswered, ID: "q3"}},
		{lDidHC, "Owner did td4: merged it", KitNote{Kind: NoteDid, Addr: "t2", Name: "contr", ID: "td4"}},
		{`{"type":"user","message":{"content":"<hc from=\"t9\">hi</hc>"}}`, "hi", KitNote{Kind: NoteMessage, Addr: "t9"}},
	}
	for _, c := range cases {
		e := one(t, c.line)
		if e.Kind != Note || e.Text != c.text || *e.Note != c.want {
			t.Errorf("%s: note = %+v %+v; want %q %+v", c.line, e, e.Note, c.text, c.want)
		}
	}
	// Not the hc shape: the owner typed it.
	if e := one(t, `{"type":"user","message":{"content":"<hc from=\"broken"}}`); e.Kind != Prompt {
		t.Errorf("broken hc tag = %+v", e)
	}
}

func TestHidden(t *testing.T) {
	cases := []struct {
		line string
		want Hide
		text string
	}{
		{lMeta, HideMeta, "Caveat: local commands"},
		{lReminder, HideReminder, "<system-reminder>be brief</system-reminder>"},
		{lLocal, HideLocalCommand, "<local-command-stdout>Set model</local-command-stdout>"},
		{lSysLocal, HideLocalCommand, "<command-name>/model</command-name>"},
		{lSystem, HideSystem, "Conversation compacted"},
		{`{"type":"system","subtype":"api_error"}`, HideSystem, "api_error"},
		{lCompact, HideCompactSummary, "This session is being continued…"},
		{lHook, HideHook, "add it to your todos"},
		{`{"type":"attachment","attachment":{"type":"hook_success","stdout":"ok"}}`, HideHook, "ok"},
		{`{"type":"attachment","attachment":{"type":"new_thing","content":"x"}}`, HideMeta, "x"},
		{lPlaybook, HidePlaybook, "contr: you are the coordinator of this project; …\nThe playbook:\n\n…"},
		{lPlaybookU, HidePlaybook, "contr: the conversation was compacted. You are the coordinator"},
		{lInterrupt, HideInterrupt, "[Request interrupted by user]"},
	}
	for _, c := range cases {
		e := one(t, c.line)
		if e.Kind != Hidden || e.Hidden != c.want || e.Text != c.text {
			t.Errorf("%s: = %+v; want hidden %d %q", c.line, e, c.want, c.text)
		}
	}
}

func TestBadAndUnknownLines(t *testing.T) {
	b := read(t, "not json", "", "{", lUnknown, `{"type":"summary","summary":"x"}`, `{"type":"attachment","attachment":"str"}`,
		`{"type":"user","message":{"content":42}}`, `{"type":"assistant","message":{"content":{"x":1}}}`, lNewField, `[1,2]`, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"nobody"}]}}`)
	if len(b.Entries) != 1 || b.Entries[0].Kind != Said || b.Entries[0].Text != "hi" || len(b.Results) != 0 {
		t.Errorf("entries = %+v, results = %+v", b.Entries, b.Results)
	}
}

func TestOffsetAndHalfLine(t *testing.T) {
	first := lPrompt + "\n" + lSaid + "\n"
	half := lCall[:20]
	var r Reader
	b, err := r.Read(strings.NewReader(first + half))
	if err != nil || len(b.Entries) != 2 || b.Next != int64(len(first)) || r.Offset() != b.Next {
		t.Fatalf("first read = %+v, %v", b, err)
	}
	// The caller gives the bytes from Next on; the half line comes whole.
	b, err = r.Read(strings.NewReader(lCall + "\n" + lResult))
	if err != nil || len(b.Entries) != 1 || b.Entries[0].Kind != Tool || b.Next != int64(len(first)+len(lCall)+1) {
		t.Fatalf("second read = %+v, %v", b, err)
	}
	if b.Entries[0].Tool.Done {
		t.Errorf("the result line has no newline yet, so the call has no result")
	}
	// Nothing new: nothing changes.
	b, err = r.Read(strings.NewReader(""))
	if err != nil || len(b.Entries) != 0 || b.Next != int64(len(first)+len(lCall)+1) {
		t.Errorf("empty read = %+v, %v", b, err)
	}
}

func TestResultInLaterRead(t *testing.T) {
	var r Reader
	b, _ := r.Read(strings.NewReader(lPrompt + "\n" + lCall + "\n" + lMCP + "\n"))
	if len(b.Entries) != 3 || b.Entries[1].Tool.Done {
		t.Fatalf("first read = %+v", b)
	}
	b, _ = r.Read(strings.NewReader(lSaid + "\n" + lErrResult + "\n"))
	want := Result{Index: 1, ID: "t1", Error: true, Excerpt: "exit 1"}
	if len(b.Entries) != 1 || len(b.Results) != 1 || b.Results[0] != want {
		t.Fatalf("second read = %+v, want result %+v", b, want)
	}
	// A result comes once; a second one for the same id is dropped.
	b, _ = r.Read(strings.NewReader(lResult + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"sent"}]}}` + "\n"))
	want = Result{Index: 2, ID: "t2", Excerpt: "sent"}
	if len(b.Results) != 1 || b.Results[0] != want {
		t.Errorf("third read = %+v, want %+v", b.Results, want)
	}
}

func TestReadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(lPrompt+"\n"+lCall+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewReader(0)
	b, err := r.ReadFile(p)
	if err != nil || len(b.Entries) != 2 {
		t.Fatalf("read = %+v, %v", b, err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(lResult + "\n" + lSaid)
	_ = f.Close()
	b, err = r.ReadFile(p)
	if err != nil || len(b.Entries) != 0 || len(b.Results) != 1 || b.Results[0].Index != 1 {
		t.Fatalf("append read = %+v, %v", b, err)
	}
	// A reader that starts at an offset reads from there.
	r2 := NewReader(int64(len(lPrompt) + 1))
	b, err = r2.ReadFile(p)
	if err != nil || len(b.Entries) != 1 || b.Entries[0].Kind != Tool || !b.Entries[0].Tool.Done {
		t.Errorf("offset read = %+v, %v", b, err)
	}
	if _, err := r.ReadFile(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("a missing file gives no error")
	}
}

type failReader struct{ data string }

func (f *failReader) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, errors.New("disk gone")
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

func TestReadError(t *testing.T) {
	var r Reader
	b, err := r.Read(&failReader{data: lPrompt + "\n" + lSaid[:10]})
	if err == nil || errors.Is(err, io.EOF) || len(b.Entries) != 1 || b.Next != int64(len(lPrompt)+1) {
		t.Errorf("read = %+v, %v", b, err)
	}
}

// FuzzRead checks that no input makes the reader panic or consume a part
// line.
func FuzzRead(f *testing.F) {
	for _, l := range []string{lPrompt, lCall + "\n" + lResult, lHC, lHook, lCommand, lNewField, `{"type":"user","message":{"content":"<hc from=\""}}`} {
		f.Add(l + "\n")
	}
	f.Fuzz(func(t *testing.T, s string) {
		var r Reader
		b, err := r.Read(strings.NewReader(s))
		if err != nil {
			t.Fatal(err)
		}
		if want := int64(strings.LastIndexByte(s, '\n') + 1); b.Next != want {
			t.Errorf("Next = %d, want %d", b.Next, want)
		}
	})
}
