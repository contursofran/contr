package chatview

// The mark of a worker (docs/spec/ChatView.md, D285): in the chat, a worker that sent a note shows
// the mark of the model family it runs on in place of its 6px dot. The state shows in the mark's
// ink and motion, as the dot had it: blocked ink-100, working ink-88 with the hop of the Claude
// mascot, else ink-30. No hue (D259, D261). A worker of another program keeps the dot.

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
)

// Family is the model family of a worker.
type Family string

// The families with a mark. FamilyNone keeps the dot.
const (
	FamilyNone     Family = ""
	FamilyClaude   Family = "claude"
	FamilyOpenAI   Family = "openai"
	FamilyDeepSeek Family = "deepseek"
)

// markSize is the side of a worker's mark: the small glyph's size.
const markSize = 12

// FamilyOf is the family of a model name or a program: the model first, then the profile's model,
// then the program. "opus", "claude-sonnet-5-5", "claude" are Claude; "gpt-6.1", "o4", "codex"
// are OpenAI; "deepseek-v4" is DeepSeek. Case does not count. Any other name: FamilyNone.
func FamilyOf(model, profileModel, command string) Family {
	for _, s := range []string{model, profileModel, filepath.Base(command)} {
		if f := familyOfName(strings.ToLower(strings.TrimSpace(s))); f != FamilyNone {
			return f
		}
	}
	return FamilyNone
}

func familyOfName(s string) Family {
	switch {
	case s == "" || s == ".":
		return FamilyNone
	case strings.Contains(s, "deepseek"):
		return FamilyDeepSeek
	case strings.Contains(s, "claude"), strings.Contains(s, "opus"), strings.Contains(s, "sonnet"),
		strings.Contains(s, "haiku"), strings.Contains(s, "anthropic"):
		return FamilyClaude
	case strings.Contains(s, "codex"), strings.Contains(s, "gpt"), strings.Contains(s, "openai"),
		s == "o3" || s == "o4" || strings.HasPrefix(s, "o3-") || strings.HasPrefix(s, "o4-"):
		return FamilyOpenAI
	}
	return FamilyNone
}

// familyOf is the family of worker w, from what contr knows of it: its model label, the model of
// its profile and its program. No new read.
func (ch *Chat) familyOf(w agents.Worker) Family {
	pm := ""
	if ch.ModelOf != nil && w.Profile != "" {
		pm = ch.ModelOf(w.Profile)
	}
	return FamilyOf(w.Model, pm, w.Command)
}

// familyMark is the SVG of family f: the Claude mascot, or a brand mark of Simple Icons. Nil for
// FamilyNone or a missing file.
func familyMark(f Family) *ui.SVG {
	switch f {
	case FamilyClaude:
		return kit.ClaudeMascot()
	case FamilyOpenAI, FamilyDeepSeek:
		return icons.Brand(string(f))
	}
	return nil
}

// mark draws mark m of a worker in color, labelled. While working it hops as the Claude mascot
// does (kit.ClaudeHopOffset); Reduce Motion keeps it still.
func (ch *Chat) mark(c *ui.Context, m *ui.SVG, label string, color ui.Color, working bool) {
	still := c.Preferences().ReduceMotion
	state := kit.GlyphIdle
	if working {
		state = kit.GlyphWorking
	}
	ui.Box(c).Label(label).Size(markSize, markSize).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		if working && !still {
			p.AnimationFrameRate(30)
		}
		r.Y -= kit.ClaudeHopOffset(state, p.Now(), still)
		p.Icon(m, r, color)
	})
}
