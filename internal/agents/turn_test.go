package agents

import (
	"strings"
	"testing"
)

// Transcript lines in the shape Claude Code writes.
const (
	prompt     = `{"type":"user","message":{"role":"user","content":"do the task"}}`
	promptList = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<hc from=\"t1 lead\">fix it</hc>"}]}}`
	metaPrompt = `{"type":"user","isMeta":true,"message":{"role":"user","content":"caveat"}}`
	result     = `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"x","content":"ok"}]}}`
	sendCall   = `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"hc send \"$CONTR_COORDINATOR\" \"b1: done\""}}]}}`
	otherCall  = `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`
	mcpCall    = `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__plugin_contr_contr__message","input":{"name":"t1"}}]}}`
	words      = `{"type":"assistant","message":{"content":[{"type":"text","text":"  PR #5 is open.  "}]}}`
	summary    = `{"type":"summary","summary":"x"}`
)

func TestLastTurn(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  Turn
	}{
		{"report in the turn", []string{prompt, otherCall, result, sendCall, result, words}, Turn{Reported: true, Said: "PR #5 is open."}},
		{"no report", []string{prompt, otherCall, result, words}, Turn{Said: "PR #5 is open."}},
		{"report only in an earlier turn", []string{prompt, sendCall, result, words, promptList, otherCall, result}, Turn{}},
		{"tool results and meta lines keep the turn", []string{prompt, sendCall, result, metaPrompt, summary, words}, Turn{Reported: true, Said: "PR #5 is open."}},
		{"the kit's message tool counts", []string{promptList, mcpCall, result}, Turn{Reported: true}},
		{"bad lines are skipped", []string{prompt, "not json", sendCall}, Turn{Reported: true}},
		{"empty", nil, Turn{}},
	}
	for _, c := range cases {
		got, err := LastTurn(strings.NewReader(strings.Join(c.lines, "\n")))
		if err != nil || got != c.want {
			t.Errorf("%s: LastTurn = %+v, %v; want %+v", c.name, got, err, c.want)
		}
	}
}
