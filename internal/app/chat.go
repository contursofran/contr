package app

// The Chat mode of the terminal panel (docs/spec/ChatView.md, D246): the frame's side of package
// chatview.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/view"
)

// attachChat makes the chat view, which reads the session files under the state dir of the
// session panel. Its composer and live line send through the agent source, as the orchestrate
// pill and the questions card do (D252); without a source they send nothing. The stop ends its
// watches.
func (f *Frame) attachChat(env view.Env, src agentstate.AgentSource) (stop func()) {
	f.Chat = chatview.New(env, f.Session.StateDir, nil)
	f.Chat.Worker = f.agentAt
	f.Chat.Meta = f.chatMeta
	if src != nil {
		f.Chat.Send, f.Chat.Interrupt, f.Chat.Read, f.Chat.SendKeys = src.Send, src.Interrupt, src.Read, src.SendKeys
	}
	f.syncChat()
	return f.Chat.Close
}

// chatMeta is the model and the context of the terminal at addr for the right of the composer:
// the model of line 2 of its row and the size of its context, "opus high · 212k". A part that is
// not known is left out.
func (f *Frame) chatMeta(addr string) string {
	var parts []string
	if w, ok := f.agentAt(addr); ok && f.SpacesList != nil {
		if m := f.SpacesList.ModelFor(w); m != "" {
			parts = append(parts, m)
		}
	}
	if c, ok := contextOf(f.planUsage(), addr, f.contextLabelsOf(addr)); ok {
		parts = append(parts, c.Used())
	}
	return strings.Join(parts, " · ")
}

// syncChat points the chat view at the selected terminal.
func (f *Frame) syncChat() {
	if f.Chat != nil {
		f.Chat.Sync(f.selectedTerminal())
	}
}

// chatAvailable tells that the terminal at addr is a Claude Code session: an orchestrator (a
// plain terminal that runs Claude Code is one too, D190), or a terminal whose transcript path a
// hook of the kit saved (a worker, D243). A plain terminal has neither: it gets no switch.
func (f *Frame) chatAvailable(addr string) bool {
	if f.Chat == nil || addr == "" {
		return false
	}
	if w, ok := f.agentAt(addr); ok && w.Role == agents.RoleOrchestrator {
		return true
	}
	return f.Chat.HasPath(addr)
}

// chatShown tells that the terminal panel shows the chat of the selected terminal.
func (f *Frame) chatShown() bool {
	addr := f.selectedTerminal()
	return f.chatAvailable(addr) && f.Chat.On(addr)
}

// chatLead is the Chat | Terminal switch left of the orchestrate pill, or nil when the selected
// terminal is not a Claude Code session.
func (f *Frame) chatLead() func(c *ui.Context) {
	addr := f.selectedTerminal()
	if !f.chatAvailable(addr) {
		return nil
	}
	return func(c *ui.Context) { f.Chat.Switch(c, addr) }
}

// toggleChat switches the selected Claude Code session between Chat and Terminal (⌘E). Back in
// Terminal, the grid takes the focus.
func (f *Frame) toggleChat() {
	addr := f.selectedTerminal()
	if !f.chatAvailable(addr) {
		return
	}
	f.Chat.Toggle(addr)
	f.focusTerm = !f.Chat.On(addr)
}

// toTerminal is the dock's Terminal button.
func (f *Frame) toTerminal() {
	f.Chat.SetOn(f.selectedTerminal(), false)
	f.focusTerm = true
}

// chatView builds the chat in the place of the grid, or tells the chat that it does not show.
// ok is false when the terminal panel shows the grid.
func (f *Frame) chatView(c *ui.Context) (ok bool) {
	if f.Chat == nil {
		return false
	}
	if !f.chatShown() {
		f.Chat.Hide()
		return false
	}
	ui.Box(c).Absolute().Fill().Children(func() { f.Chat.View(c, f.toTerminal) })
	return true
}
