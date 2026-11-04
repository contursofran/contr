package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/prompt"
)

// noPrompt is the text of pendingPrompt when the worker waits on no menu.
const noPrompt = "no prompt"

// promptLines is how many terminal lines pendingPrompt reads to find the
// menu, as the answer card does.
const promptLines = 40

// pendingPrompt returns the menu that a blocked worker waits on: the detail
// (for a permission, the tool and its command), the question and the
// numbered choices. It returns noPrompt when the worker is not blocked or
// its screen has no menu that contr can read.
func pendingPrompt(ctx context.Context, m *agents.Manager, name string) (string, error) {
	w, err := m.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if !w.Live || w.Status != "blocked" {
		return noPrompt, nil
	}
	screen, err := m.Read(ctx, name, promptLines)
	if err != nil {
		return "", err
	}
	p, ok := prompt.Parse(w.Command, w.Kind, screen)
	if !ok {
		return noPrompt, nil
	}
	var b strings.Builder
	for _, d := range p.Detail {
		fmt.Fprintln(&b, d)
	}
	fmt.Fprintln(&b, p.Question)
	for i, c := range p.Choices {
		fmt.Fprintf(&b, "%d. %s\n", i+1, c.Label)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
