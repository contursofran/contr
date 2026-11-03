package prompt

// claudeTextMarks show Claude Code choices that ask for typed text.
var claudeTextMarks = []string{"tell claude", "type something", "chat about this"}

// parseClaude reads a Claude Code numbered menu. A digit key selects its
// choice.
func parseClaude(screen string) (Prompt, bool) {
	return parseMenu(screen, claudeTextMarks)
}
