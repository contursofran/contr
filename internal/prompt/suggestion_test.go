package prompt

import "testing"

func TestSuggestion(t *testing.T) {
	const rule = "\x1b[0m\x1b[38;2;136;136;136m────\x1b[0m\r\n"
	// history is a sent prompt above the input line, as Claude Code draws it.
	const history = "❯ \x1b[0m\x1b[38;2;255;255;255m\x1b[48;2;55;55;55mfix the test\x1b[0m\r\n"
	cases := []struct {
		name, screen, want string
	}{
		// From a real session: the suggestion after a turn ended.
		{"suggestion", history + rule + "❯ \x1b[0m\x1b[2mcommit this\x1b[0m\r\n" + rule, "commit this"},
		{"faint with a color", rule + "❯ \x1b[2;38;5;244mrun the tests\x1b[0m\r\n", "run the tests"},
		{"none", history + rule + "❯ \x1b[0m\r\n" + rule, ""},
		{"no mark", "plain output\r\n", ""},
		{"typed text", rule + "❯ \x1b[0mcommit this\r\n", ""},
		{"faint ends", rule + "❯ \x1b[2mcommit\x1b[22m this\r\n", ""},
		// A new prompt went in: the input line is empty, the old suggestion is gone and the
		// sent prompt sits above it.
		{"stale after a new prompt", "❯ \x1b[2mcommit this\x1b[0m\r\n" + history + rule + "❯ \x1b[0m\r\n", ""},
		{"menu choice", " ❯ 1. Yes\r\n   2. No\r\n", ""},
		{"bad escape", "❯ \x1b]0;title\x07\x1b[2mx\r\n", ""},
	}
	for _, c := range cases {
		if got := Suggestion([]byte(c.screen)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
