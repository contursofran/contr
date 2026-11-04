package agents

import (
	"fmt"
	"os"
	"path/filepath"
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

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func usage(in, read, create int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"content":[],"usage":{"input_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"output_tokens":9}}}`, in, read, create)
}

func TestContextTokens(t *testing.T) {
	p := writeTranscript(t, prompt, usage(10, 100, 5), prompt, usage(3, 200000, 30000), `{"type":"user","message":{"content":"x"}}`)
	if n, err := ContextTokens(p); err != nil || n != 230003 {
		t.Errorf("ContextTokens = %d, %v; want the last reply's 230003", n, err)
	}
	if n, err := ContextTokens(writeTranscript(t, prompt)); err != nil || n != 0 {
		t.Errorf("no usage = %d, %v", n, err)
	}
	// A long file: only the tail is read, and the cut first line is skipped.
	big := strings.Repeat(`{"type":"user","message":{"content":"`+strings.Repeat("x", 1000)+`"}}`+"\n", tailBytes/1000+10)
	p = filepath.Join(t.TempDir(), "big.jsonl")
	if err := os.WriteFile(p, []byte(usage(1, 1, 1)+"\n"+big+usage(7, 0, 0)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := ContextTokens(p); err != nil || n != 7 {
		t.Errorf("big = %d, %v; want 7", n, err)
	}
	if _, err := ContextTokens(filepath.Join(t.TempDir(), "none")); err == nil {
		t.Error("missing file: no error")
	}
}

func TestRanCommand(t *testing.T) {
	cmd := `{"type":"user","message":{"content":"<command-message>contr:orchestrate</command-message>\n<command-name>/contr:orchestrate</command-name>"}}`
	if ran, err := RanCommand(writeTranscript(t, prompt, cmd, words), "contr:orchestrate"); err != nil || !ran {
		t.Errorf("ran = %v, %v", ran, err)
	}
	if ran, err := RanCommand(writeTranscript(t, prompt, words), "contr:orchestrate"); err != nil || ran {
		t.Errorf("no command: ran = %v, %v", ran, err)
	}
	// The tag in a file the session read, or in what the assistant wrote, is no run of the command.
	read := `{"type":"user","message":{"content":[{"type":"tool_result","content":"const orchestrateLine = ` + "`" + `{\"type\":\"user\",\"message\":{\"content\":\"<command-name>/contr:orchestrate</command-name>\"}}` + "`" + `"}]}}`
	said := `{"type":"assistant","message":{"content":[{"type":"text","text":"<command-name>/contr:orchestrate</command-name>"}]}}`
	if ran, err := RanCommand(writeTranscript(t, prompt, read, said, words), "contr:orchestrate"); err != nil || ran {
		t.Errorf("tag in a tool result: ran = %v, %v", ran, err)
	}
}
