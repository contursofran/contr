package transcript

import (
	"encoding/json"
	"errors"
	"fmt"
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
	lSkillLine = `{"type":"user","message":{"content":"contr: the owner reads your answers in contr's chat. Before you write one, use the skill contr:chat-writing: it says what the chat shows.\ncontr: the conversation was compacted."}}`
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
		{lSkillLine, HidePlaybook, "contr: the owner reads your answers in contr's chat. Before you write one, use the skill contr:chat-writing: it says what the chat shows.\ncontr: the conversation was compacted."},
		{lInterrupt, HideInterrupt, "[Request interrupted by user]"},
		{`{"type":"user","message":{"content":"<user-prompt-submit-hook>check the board</user-prompt-submit-hook>"}}`, HideHook, "<user-prompt-submit-hook>check the board</user-prompt-submit-hook>"},
		{`{"type":"user","message":{"content":"<session-start-hook>ok</session-start-hook>"}}`, HideHook, "<session-start-hook>ok</session-start-hook>"},
		{`{"type":"user","message":{"content":"<bash-stdout>hi</bash-stdout><bash-stderr></bash-stderr>"}}`, HideLocalCommand, "<bash-stdout>hi</bash-stdout><bash-stderr></bash-stderr>"},
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

func TestHookTagNeedsTheShape(t *testing.T) {
	for _, s := range []string{"<b>bold</b> is not a hook", "<not a-hook>x", "<x/a-hook>", "a <user-prompt-submit-hook>"} {
		if e := one(t, `{"type":"user","message":{"content":"`+s+`"}}`); e.Kind != Prompt {
			t.Errorf("%q = %+v; want a prompt", s, e)
		}
	}
}

func TestResultTime(t *testing.T) {
	call := `{"type":"assistant","timestamp":"2026-11-10T10:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`
	res := `{"type":"user","timestamp":"2026-11-10T10:00:02.5Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`
	if c := read(t, call, res).Entries[0].Tool; c.End != "2026-11-10T10:00:02.5Z" {
		t.Errorf("end in the same read = %q", c.End)
	}
	var r Reader
	if _, err := r.Read(strings.NewReader(call + "\n")); err != nil {
		t.Fatal(err)
	}
	b, _ := r.Read(strings.NewReader(res + "\n"))
	if len(b.Results) != 1 || b.Results[0].Time != "2026-11-10T10:00:02.5Z" {
		t.Errorf("result of a later read = %+v", b.Results)
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

// userLine is a user line whose content is the text blocks texts and, when
// images > 0, that many image blocks.
func userLine(t *testing.T, images int, texts ...string) string {
	t.Helper()
	var content []map[string]any
	for _, s := range texts {
		content = append(content, map[string]any{"type": "text", "text": s})
	}
	for range images {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "data": "AAAA"}})
	}
	b, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTaskNotification(t *testing.T) {
	const task = "<task-notification>\n<task-id>bx1</task-id>\n<tool-use-id>toolu_1</tool-use-id>\n<output-file>/tmp/x.output</output-file>\n<status>%s</status>\n<summary>Background command \"go test ./...\" %s (exit code 0)</summary>\n</task-notification>"
	cases := []struct {
		text, status, summary string
	}{
		{fmt.Sprintf(task, "completed", "completed"), "completed", `Background command "go test ./..." completed (exit code 0)`},
		{fmt.Sprintf(task, "failed", "failed"), "failed", `Background command "go test ./..." failed (exit code 0)`},
		// After the harness's preamble, inside a reminder.
		{"<system-reminder>\n[SYSTEM NOTIFICATION - NOT USER INPUT]\n" + fmt.Sprintf(task, "killed", "was stopped") + "\n</system-reminder>", "killed", `Background command "go test ./..." was stopped (exit code 0)`},
		{"[SYSTEM NOTIFICATION - NOT USER INPUT]\n<task-notification><status>completed</status><summary>Agent \"review\" completed</summary><result>long\ntext</result><usage>1</usage></task-notification>", "completed", `Agent "review" completed`},
	}
	for _, c := range cases {
		e := one(t, userLine(t, 0, c.text))
		if e.Kind != Note || e.Note == nil || e.Note.Kind != NoteTask || e.Note.Status != c.status || e.Text != c.summary {
			t.Errorf("%q = %+v %+v; want task %q %q", c.text, e, e.Note, c.status, c.summary)
		}
	}
	if e := one(t, userLine(t, 0, fmt.Sprintf(task, "completed", "completed"))); e.Note.ID != "bx1" {
		t.Errorf("task id = %q", e.Note.ID)
	}
	// The owner wrote about the tag: a prompt.
	if e := one(t, userLine(t, 0, "why is <task-notification> shown?")); e.Kind != Prompt {
		t.Errorf("prompt that names the tag = %+v", e)
	}
}

func TestAgentMessage(t *testing.T) {
	cases := []struct {
		text, name, body string
	}{
		{"Another Claude session sent a message:\n<agent-message from=\"planner-12\">The plan is ready.\nRead it at plan.md</agent-message>", "planner-12", "The plan is ready. …"},
		{`<cross-session-message from="w2">rebase on main</cross-session-message>`, "w2", "rebase on main"},
		{`<teammate-message teammate_id="tester" color="blue">tests pass</teammate-message>`, "tester", "tests pass"},
		{"<system-reminder>a note</system-reminder>\n<agent-message from=\"x\">hi</agent-message>", "x", "hi"},
		{"Another Claude session sent a message:\nhello there", "", "hello there"},
		{"[Subagent hand-back] the files are read; three call sites", "subagent", "reported"},
	}
	for _, c := range cases {
		e := one(t, userLine(t, 0, c.text))
		if e.Kind != Note || e.Note == nil || e.Note.Kind != NoteAgent || e.Note.Name != c.name || e.Text != c.body {
			t.Errorf("%q = %+v %+v; want agent %q %q", c.text, e, e.Note, c.name, c.body)
		}
	}
	// Not the shape: a tag in the middle of a prompt, or a tag of another name.
	for _, s := range []string{"see <agent-message from=\"x\">", "<agent-messages from=\"x\">a</agent-messages> please"} {
		if e := one(t, userLine(t, 0, s)); e.Kind != Prompt {
			t.Errorf("%q = %+v; want a prompt", s, e)
		}
	}
}

func TestHarnessTextsHide(t *testing.T) {
	cases := []struct {
		text string
		want Hide
	}{
		{"<system-reminder>\nThe task tools haven't been used recently.\n</system-reminder>", HideReminder},
		{"<system-reminder>a</system-reminder>\n<system-reminder>b</system-reminder>", HideReminder},
		{"<local-command-caveat>Caveat: the messages below…</local-command-caveat>", HideLocalCommand},
		{"<local-command-stdout>Set model to opus</local-command-stdout>", HideLocalCommand},
		{"<command-message>contr:orchestrate is running…</command-message>", HideHarness},
		{"<user-prompt-submit-hook>3 workers live</user-prompt-submit-hook>", HideHook},
		{"Stop hook feedback:\n[contr hook stop]: report first", HideHook},
		{"[Image: source: /tmp/shot.png]", HideHarness},
		{"The user hasn't heard from you in a while — say in a few words what you're doing, then continue.", HideHarness},
		// The fallback: one tag pair of lowercase letters and dashes.
		{"<new-harness-thing kind=\"x\">\nsome text\n</new-harness-thing>", HideHarness},
		{"<bash-input>ls</bash-input>", HideHarness},
	}
	for _, c := range cases {
		e := one(t, userLine(t, 0, c.text))
		if e.Kind != Hidden || e.Hidden != c.want || e.Text != c.text {
			t.Errorf("%q = %+v; want hidden %d", c.text, e, c.want)
		}
	}
	// Not one tag pair: prompts.
	for _, s := range []string{"<b>bold</b> and more", "<Upper>x</Upper>", "<a-b>x</a-b> then <a-b>y</a-b>", "<x>y</z>"} {
		if e := one(t, userLine(t, 0, s)); e.Kind != Prompt {
			t.Errorf("%q = %+v; want a prompt", s, e)
		}
	}
	// A slash command keeps its prompt.
	if e := one(t, userLine(t, 0, "<command-name>/clear</command-name>")); e.Kind != Prompt || e.Text != "/clear" {
		t.Errorf("slash command = %+v", e)
	}
}

func TestPromptWithHarnessParts(t *testing.T) {
	cases := []struct {
		texts  []string
		images int
		want   string
	}{
		{[]string{"fix the bug\n\n<system-reminder>\nbe brief\n</system-reminder>"}, 0, "fix the bug"},
		{[]string{"fix the bug", "<system-reminder>be brief</system-reminder>"}, 0, "fix the bug"},
		{[]string{"<system-reminder>be brief</system-reminder>\nfix the bug"}, 0, "fix the bug"},
		{[]string{"fix the bug\n<user-prompt-submit-hook>3 workers live</user-prompt-submit-hook>"}, 0, "fix the bug"},
		{[]string{"fix the bug <system-reminder>never closed"}, 0, "fix the bug"},
		{[]string{"[Image: source: /tmp/a.png]", "look at this\n<system-reminder>x</system-reminder>"}, 1, "look at this"},
		{[]string{"[Image: source: /tmp/a.png]"}, 1, ""},
	}
	for _, c := range cases {
		e := one(t, userLine(t, c.images, c.texts...))
		if e.Kind != Prompt || e.Text != c.want || e.Images != c.images {
			t.Errorf("%q = %+v; want prompt %q with %d images", c.texts, e, c.want, c.images)
		}
	}
}

// Shapes of a message that the owner sent while a turn ran (D265). They come
// from what the harness is known or likely to write, not from a real file.
const (
	lQueuedStr    = `{"type":"attachment","uuid":"q1","timestamp":"2026-11-10T10:00:05.000Z","attachment":{"type":"queued_command","prompt":"add auto compact later","commandMode":"prompt"}}`
	lQueuedBlocks = `{"type":"attachment","attachment":{"type":"queued_command","prompt":[{"type":"text","text":"add auto compact later"},{"type":"image","source":{"type":"base64","data":"AAAA"}}]}}`
	lQueuedHC     = `{"type":"attachment","attachment":{"type":"queued_command","prompt":"<hc from=\"t7 b1\">b1: done</hc>"}}`
	lQueuedEmpty  = `{"type":"attachment","attachment":{"type":"queued_command","prompt":""}}`
	lEnqueue      = `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-11-10T10:00:04.000Z","content":"add auto compact later"}`
	lDequeue      = `{"type":"queue-operation","operation":"dequeue","timestamp":"2026-11-10T10:00:05.000Z"}`
	lRemove       = `{"type":"queue-operation","operation":"remove","content":"add auto compact later"}`
	lCallResult   = `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"text","text":"<system-reminder>\nThe user sent a new message while you were working:\nadd auto compact later\n\nIMPORTANT: After completing your current task, you MUST address the user's message above. Do not ignore it.\n</system-reminder>"}]}}`
)

func prompts(b Batch) []Entry {
	var out []Entry
	for _, e := range b.Entries {
		if e.Kind == Prompt {
			out = append(out, e)
		}
	}
	return out
}

func TestQueuedAttachment(t *testing.T) {
	e := one(t, lQueuedStr)
	if e.Kind != Prompt || e.Text != "add auto compact later" || e.UUID != "q1" || e.Time != "2026-11-10T10:00:05.000Z" {
		t.Errorf("string prompt = %+v", e)
	}
	e = one(t, lQueuedBlocks)
	if e.Kind != Prompt || e.Text != "add auto compact later" || e.Images != 1 {
		t.Errorf("block prompt = %+v", e)
	}
	e = one(t, lQueuedHC)
	if e.Kind != Note || e.Note == nil || e.Note.Kind != NoteMessage || e.Note.Addr != "t7" {
		t.Errorf("queued hc message = %+v; want an hc note", e)
	}
	e = one(t, lQueuedEmpty)
	if e.Kind != Hidden {
		t.Errorf("empty queued prompt = %+v; want hidden", e)
	}
}

func TestQueueOperationsAreNoPrompt(t *testing.T) {
	b := read(t, lEnqueue, lDequeue, lRemove)
	if len(b.Entries) != 0 {
		t.Errorf("queue operations = %+v; want no entry", b.Entries)
	}
}

func TestQueuedReminder(t *testing.T) {
	b := read(t, lCall, lCallResult)
	p := prompts(b)
	if len(p) != 1 || p[0].Text != "add auto compact later" {
		t.Fatalf("prompts = %+v; want the queued message", p)
	}
	if !b.Entries[0].Tool.Done || b.Entries[0].Tool.Excerpt != "ok" {
		t.Errorf("call = %+v; want its result", b.Entries[0].Tool)
	}
	// After other reminders, and on a line marked meta.
	e := one(t, userLine(t, 0, "<system-reminder>be brief</system-reminder>", "<system-reminder>The user sent a new message while you were working:\nstop now</system-reminder>"))
	if e.Kind != Prompt || e.Text != "stop now" {
		t.Errorf("after a reminder = %+v", e)
	}
	e = one(t, `{"type":"user","isMeta":true,"message":{"content":"<system-reminder>The user sent a new message while you were working:\nstop now\n</system-reminder>"}}`)
	if e.Kind != Prompt || e.Text != "stop now" {
		t.Errorf("meta line = %+v", e)
	}
	// The lead inside the owner's own text is no reminder.
	e = one(t, userLine(t, 0, "why does it say The user sent a new message while you were working: here"))
	if e.Kind != Prompt || !strings.HasPrefix(e.Text, "why does it say") {
		t.Errorf("own text = %+v", e)
	}
	// A lead with no message is hidden.
	e = one(t, userLine(t, 0, "<system-reminder>The user sent a new message while you were working:\n</system-reminder>"))
	if e.Kind != Hidden {
		t.Errorf("no message = %+v; want hidden", e)
	}
}

func TestQueuedMessageIsOnePrompt(t *testing.T) {
	b := read(t, lEnqueue, lCall, lRemove, lQueuedStr, lCallResult)
	if p := prompts(b); len(p) != 1 || p[0].UUID != "q1" {
		t.Errorf("prompts = %+v; want one, from the attachment", p)
	}
	// The same text again after Claude's text is a new message.
	b = read(t, lQueuedStr, lSaid, lQueuedStr)
	if p := prompts(b); len(p) != 2 {
		t.Errorf("prompts = %+v; want two", p)
	}
	// So after a prompt of the owner.
	b = read(t, lQueuedStr, userLine(t, 0, "go on"), lQueuedStr)
	if p := prompts(b); len(p) != 3 {
		t.Errorf("prompts = %+v; want three", p)
	}
	// Across reads.
	var r Reader
	if _, err := r.Read(strings.NewReader(lQueuedStr + "\n")); err != nil {
		t.Fatal(err)
	}
	b2, err := r.Read(strings.NewReader(lCallResult + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p := prompts(b2); len(p) != 0 {
		t.Errorf("second read = %+v; want no second prompt", p)
	}
}

// surfaceTail is the tail of the reminder in a newer Claude Code version, as
// the coordinator saw it in a session.
const surfaceTail = "\n\nThis is how Claude Code surfaces messages the user sends mid-turn — within the running turn, often alongside the next tool result, rather than as a separate conversation turn. Address the message above as you continue this turn."

func TestQueuedReminderTails(t *testing.T) {
	lead := "The user sent a new message while you were working:\n"
	cases := []struct {
		text   string
		want   string
		images int
	}{
		{lead + "add auto compact later" + surfaceTail, "add auto compact later", 0},
		{"<system-reminder>\n" + lead + "add auto compact later" + surfaceTail + "\n</system-reminder>", "add auto compact later", 0},
		{lead + "look at this\n[Image #1]" + surfaceTail, "look at this", 1},
		{lead + "two lines\nof text\n\nIMPORTANT: After completing your current task, you MUST address the user's message above.", "two lines\nof text", 0},
		// A tail sentence inside a line is the owner's text.
		{lead + "why This is how Claude Code surfaces messages?", "why This is how Claude Code surfaces messages?", 0},
	}
	for _, c := range cases {
		b := read(t, lCall, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"text","text":`+jsonString(t, c.text)+`}]}}`)
		p := prompts(b)
		if len(p) != 1 || p[0].Text != c.want || p[0].Images != c.images {
			t.Errorf("%q: prompts %+v; want %q with %d images", c.text, p, c.want, c.images)
		}
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestImageMarker(t *testing.T) {
	for s, want := range map[string]bool{"[Image #1]": true, " [Image #12] ": true, "[Image #]": false, "[Image #a]": false, "[Image: source: x]": false, "text": false} {
		if ImageMarker(s) != want {
			t.Errorf("ImageMarker(%q) = %v", s, !want)
		}
	}
}
