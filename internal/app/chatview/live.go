package chatview

// The live state (docs/spec/ChatView.md, D252, D276): whether the agent works, for the composer's
// footer. The transcript gets Claude's text only when a block ends, so the footer
// tells the owner that work goes on. A session that waits on a menu has no line: the notch answers it
// (D260).

import (
	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/tabmodel"
)

// LabelLive names the working status of the composer footer: "live line Working 7s".
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
