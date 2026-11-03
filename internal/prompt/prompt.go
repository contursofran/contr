// Package prompt reads the menu a blocked worker shows on its terminal.
// It is pure: it takes screen text and returns the question and the choices.
package prompt

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// maxChoices is the most choices one digit key can pick.
const maxChoices = 9

// Choice is one answer in a menu.
type Choice struct {
	Label string
	// Keys are the raw bytes that select the choice. No Enter is added.
	Keys []byte
	// Text is true when the choice asks the owner for typed follow-up text.
	// The card cannot type it, so it must not send these keys.
	Text bool
}

// Prompt is a question with its choices.
type Prompt struct {
	Question string
	Choices  []Choice
}

// Equal reports whether two prompts have the same question and choices.
func (p Prompt) Equal(o Prompt) bool {
	return p.Question == o.Question && slices.EqualFunc(p.Choices, o.Choices, func(a, b Choice) bool {
		return a.Label == b.Label && a.Text == b.Text && slices.Equal(a.Keys, b.Keys)
	})
}

// Parse reads the last menu on screen. command is the worker's command line
// or profile name, and kind is the blocked kind (permission, question, auth).
// It returns false for an unknown agent, an auth prompt, or an unknown form.
func Parse(command, kind, screen string) (Prompt, bool) {
	if kind == "auth" {
		return Prompt{}, false
	}
	switch agentOf(command) {
	case "claude":
		return parseClaude(screen)
	case "codex":
		return parseCodex(screen)
	}
	return Prompt{}, false
}

// agentOf names the agent that a command line starts.
func agentOf(command string) string {
	f := strings.Fields(command)
	if len(f) == 0 {
		return ""
	}
	switch filepath.Base(f[0]) {
	case "claude":
		return "claude"
	case "codex":
		return "codex"
	}
	return ""
}

var itemRe = regexp.MustCompile(`^(\d+)\.\s+(\S.*)$`)

// clean removes box characters and the cursor mark from one screen line.
func clean(line string) string {
	line = strings.Trim(line, " \t\r│┃╭╮╰╯─━")
	for _, m := range []string{"❯", "›", ">"} {
		if rest, ok := strings.CutPrefix(line, m); ok {
			line = strings.TrimSpace(rest)
			break
		}
	}
	return strings.TrimSpace(line)
}

type item struct {
	line  int
	num   int
	label string
}

// parseMenu finds the last run of numbered items that counts up from 1 and
// returns it with the nearest text line above it. textMarks lists label
// parts that show a choice that asks for typed text.
func parseMenu(screen string, textMarks []string) (Prompt, bool) {
	lines := strings.Split(screen, "\n")
	for i := range lines {
		lines[i] = clean(lines[i])
	}
	var items []item
	for i, l := range lines {
		if m := itemRe.FindStringSubmatch(l); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			items = append(items, item{i, n, strings.TrimSpace(m[2])})
		}
	}
	if len(items) == 0 {
		return Prompt{}, false
	}
	// Walk back from the last item until the numbers reach 1.
	start := len(items) - 1
	for items[start].num != 1 {
		if start == 0 || items[start-1].num != items[start].num-1 {
			return Prompt{}, false
		}
		start--
	}
	run := items[start:]
	if len(run) < 2 || len(run) > maxChoices {
		return Prompt{}, false
	}
	var p Prompt
	for _, it := range run {
		c := Choice{Label: it.label, Keys: []byte(strconv.Itoa(it.num))}
		for _, m := range textMarks {
			if strings.Contains(strings.ToLower(it.label), m) {
				c.Text = true
			}
		}
		p.Choices = append(p.Choices, c)
	}
	for i := run[0].line - 1; i >= 0; i-- {
		if lines[i] != "" {
			p.Question = lines[i]
			break
		}
	}
	return p, true
}
