package chatview

import (
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
)

// callOf is a transcript line with one tool call; resultOf is its result.
func callOf(id, name, input string) string {
	return `{"type":"assistant","uuid":"c` + id + `","timestamp":"2026-11-10T10:00:01Z","message":{"content":[{"type":"tool_use","id":"` +
		id + `","name":` + strconv.Quote(name) + `,"input":` + input + `}]}}`
}

func resultOf(id string) string {
	return `{"type":"user","uuid":"r` + id + `","timestamp":"2026-11-10T10:00:02.4Z","message":{"content":[{"type":"tool_result","tool_use_id":"` +
		id + `","content":"ok"}]}}`
}

// mixedCalls has a done call of each kind, and a call of no kind.
var mixedCalls = []string{
	lPrompt,
	callOf("k1", "Skill", `{"skill":"contr:chat-writing"}`), resultOf("k1"),
	callOf("k2", "mcp__claude_ai_Slack__slack_send_message", `{"channel":"#dev","text":"hi"}`), resultOf("k2"),
	callOf("k3", "mcp__plugin_contr_contr__send", `{"to":"b-709","body":"x"}`), resultOf("k3"),
	callOf("k4", "mcp__linear__list_issues", `{"query":"chat"}`), resultOf("k4"),
	callOf("k5", "Task", `{"subagent_type":"Explore","description":"Find the call line"}`), resultOf("k5"),
	callOf("k6", "WebFetch", `{"url":"https://example.com/docs/page"}`), resultOf("k6"),
	callOf("k7", "Bash", `{"command":"go test ./...","description":"Run the tests"}`), resultOf("k7"),
	callOf("k8", "Read", `{"file_path":"/repo/internal/app/chatview/view.go"}`), resultOf("k8"),
	callOf("k9", "Grep", `{"pattern":"callLine"}`), resultOf("k9"),
	callOf("k10", "TodoWrite", `{"todos":[]}`), resultOf("k10"),
	said("s1", "Done."),
}

// TestCallLineIcon: each done call line shows the icon of its kind; a call of no kind shows none
// (D274).
func TestCallLineIcon(t *testing.T) {
	v := sheetChat(t, "idle", mixedCalls...)
	tt := ui.NewTester(v.view, sheetW, 700)
	tt.Frame()
	for label, icon := range map[string]string{
		"Skill · chat-writing": "skill", "Slack · send message": "connector", "contr · send": "plugin",
		"linear · list issues": "connector", "Agent · Explore": "agent", "Fetch": "globe",
		"Bash": "terminal", "Read": "file", "Grep": "search",
	} {
		if apptest.Absent(tt, LabelCall+" "+label) {
			t.Errorf("no call line %q", label)
		}
		if apptest.Absent(tt, LabelCallIcon+" "+icon) {
			t.Errorf("no %q icon for %q", icon, label)
		}
	}
	if !tt.HasText("Slack · send message") || !tt.HasText("#dev") {
		t.Errorf("no label or key argument in %v", tt.Texts())
	}
}

// TestCallLineRunningGlyph: a running call shows the working glyph in the slot, not its icon.
func TestCallLineRunningGlyph(t *testing.T) {
	v := sheetChat(t, "working", lPrompt, callOf("k1", "Grep", `{"pattern":"x"}`), callOf("k2", "Read", `{"file_path":"a.go"}`), resultOf("k2"))
	tt := ui.NewTester(v.view, sheetW, 400)
	tt.Frame()
	if !apptest.Absent(tt, LabelCallIcon+" search") {
		t.Error("a running call shows its kind icon")
	}
	if apptest.Absent(tt, LabelCallIcon+" file") {
		t.Error("a done call shows no kind icon")
	}
	if apptest.Absent(tt, "working") {
		t.Error("a running call shows no working glyph")
	}
}
