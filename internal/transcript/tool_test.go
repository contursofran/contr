package transcript

import (
	"reflect"
	"testing"
)

func TestDescribe(t *testing.T) {
	for _, c := range []struct {
		name, input        string
		kind               ToolKind
		server, label, arg string
	}{
		{"Skill", `{"skill":"humanizer"}`, KindSkill, "", "Skill · humanizer", ""},
		{"Skill", `{"skill":"contr:chat-writing","args":"short\nmore"}`, KindSkill, "contr", "Skill · chat-writing", "short …"},
		{"mcp__claude_ai_Google_Calendar__list_events", `{}`, KindConnector, "Google Calendar", "Google Calendar · list events", ""},
		{"mcp__claude_ai_Slack__slack_send_message", `{"channel":"#dev"}`, KindConnector, "Slack", "Slack · send message", "#dev"},
		{"mcp__claude_ai_Notion__notion-search", `{"query":"plan"}`, KindConnector, "Notion", "Notion · search", "plan"},
		{"mcp__claude_ai_Google_Calendar__google_calendar_list_events", `{}`, KindConnector, "Google Calendar", "Google Calendar · list events", ""},
		{"mcp__claude_ai_Slack__slack", `{}`, KindConnector, "Slack", "Slack · slack", ""},
		{"mcp__claude_ai_Slack__slackbot_post", `{}`, KindConnector, "Slack", "Slack · slackbot post", ""},
		{"mcp__plugin_contr_contr__send", `{"to":"w1","body":"x"}`, KindPlugin, "contr", "contr · send", "w1"},
		{"mcp__slack__x", `{"channel":"#general","text":"hi"}`, KindMCP, "slack", "slack · x", "#general"},
		{"Task", `{"subagent_type":"Explore","description":"Find files"}`, KindAgent, "", "Agent · Explore", "Find files"},
		{"Agent", `{"description":"Look"}`, KindAgent, "", "Agent · general", "Look"},
		{"WebFetch", `{"url":"https://go.dev/doc/?x=1"}`, KindWeb, "", "Fetch", "go.dev/doc"},
		{"WebSearch", `{"query":"mygo list"}`, KindWeb, "", "Search", "mygo list"},
		{"Bash", `{"command":"go test","description":"Run tests"}`, KindShell, "", "Bash", "Run tests"},
		{"Read", `{"file_path":"/x/y/a.go"}`, KindFile, "", "Read", "a.go"},
		{"Grep", `{"pattern":"func main"}`, KindSearch, "", "Grep", "func main"},
		{"Weird", `{bad`, KindOther, "", "Weird", ""},
	} {
		kind, server, label, arg, _ := Describe(c.name, []byte(c.input))
		if kind != c.kind || server != c.server || label != c.label || arg != c.arg {
			t.Errorf("Describe(%s, %s) = %d %q %q %q, want %d %q %q %q", c.name, c.input, kind, server, label, arg, c.kind, c.server, c.label, c.arg)
		}
	}
}

func TestDescribeArgs(t *testing.T) {
	_, _, _, _, args := Describe("X", []byte(`{"g":1,"f":"6","e":"5","d":"4","c":"3","b":"2","a":"1"}`))
	want := [][2]string{{"a", "1"}, {"b", "2"}, {"c", "3"}, {"d", "4"}, {"e", "5"}, {"f", "6"}}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v", args)
	}
	_, _, _, _, args = Describe("X", []byte(`{"n":{"k":true}}`))
	if !reflect.DeepEqual(args, [][2]string{{"n", `{"k":true}`}}) {
		t.Errorf("args = %v", args)
	}
}
