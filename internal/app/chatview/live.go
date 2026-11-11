package chatview

// The live line (docs/spec/ChatView.md, D252): between the transcript and the composer, what the
// agent does now. The transcript gets Claude's text only when a block ends, so this line tells
// the owner that work goes on. A session that waits on a menu has no line: the notch answers it
// (D260).

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/theme"
)

// LabelLive names the live line.
const LabelLive = "live line"

type liveKind int

const (
	liveIdle liveKind = iota
	liveWorking
	liveNeedsYou
)

// liveState is the state of the agent of the chat's terminal, from its worker.
type liveState struct {
	kind liveKind
	w    agents.Worker
}

// state is the live state of the agent at terminal id: working, needs you, else idle.
func (ch *Chat) state(id string) liveState {
	if ch.Worker == nil {
		return liveState{}
	}
	w, ok := ch.Worker(id)
	if !ok {
		return liveState{}
	}
	switch tabmodel.TabState(w) {
	case kit.GlyphWorking:
		return liveState{liveWorking, w}
	case kit.GlyphNeedsYou:
		return liveState{liveNeedsYou, w}
	}
	return liveState{w: w}
}

// live builds the live line of a working agent, the top line of the composer card (D271): the
// working glyph, what it does now and the time since the turn started, which asks for a redraw
// each second while it shows (D55). Idle or waiting on a menu: nothing, and it returns false.
func (ch *Chat) live(c *ui.Context, st liveState) bool {
	if st.kind != liveWorking {
		return false
	}
	pal := theme.Current()
	what, start, ok := ch.model.Activity()
	elapsed := ""
	if ok {
		elapsed = ElapsedText(c.Now().Sub(start))
	}
	topLine(c).Label(LabelLive + " " + what).Gap(theme.Space8).Children(func() {
		ch.working(c, pal, what, elapsed)
	})
	return true
}

// topLine is the row of the team row or the live line at the top of the composer card. It ends as
// far from the card's right edge as the text starts from its left edge.
func topLine(c *ui.Context) ui.Element {
	return ui.Row(c).FillWidth().Height(lineHeight).AlignItems(ui.Center).Margin(0, cardPadLeft-cardPadRight, 0, 0)
}
