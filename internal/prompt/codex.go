package prompt

// codexTextMarks show Codex choices that ask for typed text.
var codexTextMarks = []string{"tell codex"}

// parseCodex reads a Codex approval menu. Its choices are numbered, so a
// digit key selects its choice.
func parseCodex(screen string) (Prompt, bool) {
	return parseMenu(screen, codexTextMarks)
}
