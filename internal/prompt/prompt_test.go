package prompt

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func labels(p Prompt) []string {
	var out []string
	for _, c := range p.Choices {
		out = append(out, c.Label)
	}
	return out
}

func check(t *testing.T, p Prompt, question string, want []string, texts []bool) {
	t.Helper()
	if p.Question != question {
		t.Errorf("question = %q, want %q", p.Question, question)
	}
	got := labels(p)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("labels = %q, want %q", got, want)
	}
	for i, c := range p.Choices {
		if string(c.Keys) != string(rune('1'+i)) {
			t.Errorf("choice %d keys = %q", i, c.Keys)
		}
		if c.Text != texts[i] {
			t.Errorf("choice %d text = %v, want %v", i, c.Text, texts[i])
		}
	}
}

func TestParseClaudePermission(t *testing.T) {
	p, ok := Parse("claude --model opus", "permission", read(t, "claude_permission.txt"))
	if !ok {
		t.Fatal("not parsed")
	}
	check(t, p, "Do you want to proceed?", []string{
		"Yes",
		"Yes, and don't ask again for npm test commands in /work",
		"No, and tell Claude what to do differently (esc)",
	}, []bool{false, false, true})
}

func TestParseClaudeQuestion(t *testing.T) {
	p, ok := Parse("/usr/local/bin/claude", "question", read(t, "claude_question.txt"))
	if !ok {
		t.Fatal("not parsed")
	}
	check(t, p, "Which library should the parser use?", []string{
		"regexp", "strings only", "Type something.", "Chat about this",
	}, []bool{false, false, true, true})
}

func TestParseCodexApproval(t *testing.T) {
	p, ok := Parse("codex", "permission", read(t, "codex_approval.txt"))
	if !ok {
		t.Fatal("not parsed")
	}
	check(t, p, "$ touch /work/out.txt", []string{
		"Yes, proceed (y)",
		"Yes, and don't ask again for this command (a)",
		"No, and tell Codex what to do differently (esc)",
	}, []bool{false, false, true})
}

func TestParseUnknown(t *testing.T) {
	if _, ok := Parse("claude", "permission", read(t, "unknown.txt")); ok {
		t.Fatal("parsed an unknown screen")
	}
}

func TestParseUnknownCommand(t *testing.T) {
	if _, ok := Parse("bash", "permission", read(t, "claude_permission.txt")); ok {
		t.Fatal("parsed for bash")
	}
	if _, ok := Parse("", "permission", read(t, "claude_permission.txt")); ok {
		t.Fatal("parsed for an empty command")
	}
}

func TestParseAuthKind(t *testing.T) {
	if _, ok := Parse("claude", "auth", read(t, "claude_permission.txt")); ok {
		t.Fatal("offered choices for auth")
	}
}

func TestParseOldPromptAbove(t *testing.T) {
	p, ok := Parse("claude", "permission", read(t, "old_prompt.txt"))
	if !ok {
		t.Fatal("not parsed")
	}
	check(t, p, "Do you want to run make?", []string{"Yes", "Yes, and don't ask again", "No"},
		[]bool{false, false, false})
}

func TestParseGapInNumbers(t *testing.T) {
	if _, ok := Parse("claude", "question", "Pick\n1. a\n2. b\n4. d\n"); ok {
		t.Fatal("parsed numbers with a gap")
	}
}

func TestParseTenChoices(t *testing.T) {
	var b strings.Builder
	b.WriteString("Pick\n")
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		b.WriteString(n + ". item\n")
	}
	if _, ok := Parse("claude", "question", b.String()); ok {
		t.Fatal("parsed ten choices")
	}
}

func TestParseSingleChoice(t *testing.T) {
	if _, ok := Parse("claude", "question", "Pick\n1. only\n"); ok {
		t.Fatal("parsed one choice")
	}
}

func TestPromptEqual(t *testing.T) {
	a, _ := Parse("claude", "permission", read(t, "claude_permission.txt"))
	b, _ := Parse("claude", "permission", read(t, "claude_permission.txt"))
	if !a.Equal(b) {
		t.Fatal("same screens differ")
	}
	b.Question = "Other?"
	if a.Equal(b) {
		t.Fatal("different question is equal")
	}
	b, _ = Parse("claude", "permission", read(t, "claude_permission.txt"))
	b.Choices[1].Label = "Changed"
	if a.Equal(b) {
		t.Fatal("different label is equal")
	}
	b, _ = Parse("claude", "permission", read(t, "claude_permission.txt"))
	b.Choices = b.Choices[:2]
	if a.Equal(b) {
		t.Fatal("different count is equal")
	}
	b, _ = Parse("claude", "permission", read(t, "claude_permission.txt"))
	b.Choices[0].Keys = []byte("9")
	if a.Equal(b) {
		t.Fatal("different keys are equal")
	}
}

// TestParseDetail: the lines of the box above the question are the detail;
// a menu with no box above it has none.
func TestParseDetail(t *testing.T) {
	p, ok := Parse("claude", "permission", read(t, "claude_permission.txt"))
	if !ok || !slices.Equal(p.Detail, []string{"Bash command", "npm test", "Run the unit tests"}) {
		t.Errorf("boxed: detail %q, %v", p.Detail, ok)
	}
	sep := "● Done.\n────────────\n Bash command\n\n   go test ./...\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n"
	if p, ok := Parse("claude", "permission", sep); !ok || !slices.Equal(p.Detail, []string{"Bash command", "go test ./..."}) {
		t.Errorf("separator: detail %q, %v", p.Detail, ok)
	}
	if p, ok := Parse("claude", "permission", read(t, "old_prompt.txt")); !ok || p.Detail != nil {
		t.Errorf("no box: detail %q, %v", p.Detail, ok)
	}
	if p, ok := Parse("claude", "question", read(t, "claude_question.txt")); !ok || p.Detail != nil {
		t.Errorf("question with no box: detail %q, %v", p.Detail, ok)
	}
}
