package app

// The Chat mode of the terminal panel (docs/spec/ChatView.md, D246): the frame's side of package
// chatview.

import (
	"log"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/slash"
)

// slashSource gives the composer's popup the commands and skills of the session at an address,
// read from the folder of its agent (D273).
type slashSource struct {
	r      *slash.Reader
	worker func(addr string) (agents.Worker, bool)
}

// Suggestions finds the folder on the UI thread; the read runs off it.
func (s slashSource) Suggestions(addr string) func() ([]chatview.Suggestion, error) {
	var dir string
	if w, ok := s.worker(addr); ok {
		dir = w.Worktree
	}
	return func() ([]chatview.Suggestion, error) {
		items, err := s.r.Items(dir)
		out := make([]chatview.Suggestion, len(items))
		for i, it := range items {
			out[i] = chatview.Suggestion{Name: it.Name, Detail: it.Detail}
		}
		return out, err
	}
}

// attachChat makes the chat view, which reads the session files under the state dir of the
// session panel. Its composer sends and interrupts through the agent source, as the orchestrate
// pill and the questions card do (D252); without a source it sends nothing. The stop ends its
// watches.
func (f *Frame) attachChat(env view.Env, src agentstate.AgentSource) (stop func()) {
	f.Chat = chatview.New(env, f.Session.StateDir, nil)
	f.Chat.Worker = f.agentAt
	f.Chat.Meta = f.chatMeta
	f.Chat.NextPrompt = f.nextPrompt
	f.Chat.Team = f.chatTeam
	f.Chat.OpenNeeds = f.openNeeds
	// The notch is made after the chat: it is read when the pinned prompt builds.
	f.Chat.TextScale = f.ChatScale
	f.Chat.Notch = func() (ui.Rect, bool) {
		if f.Notch == nil {
			return ui.Rect{}, false
		}
		return f.Notch.Bounds()
	}
	f.Chat.Chips = newChatChips(f, env)
	if home, err := os.UserHomeDir(); err == nil {
		f.Chat.Slash = slashSource{r: &slash.Reader{Home: home}, worker: f.agentAt}
	} else {
		log.Printf("chat: no home folder, no slash popup: %v", err)
	}
	if f.Outline != nil {
		f.Outline.Chat = f.chatOutline
	}
	if src != nil {
		f.Chat.Send, f.Chat.Keys, f.Chat.Interrupt = src.Send, src.SendKeys, src.Interrupt
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

// chatTeam is the team of the orchestrator at addr for the chat's team row (D259), from the agent
// state: ok only while it runs and orchestrates (D191, the pill's rule). Its agents are its live
// workers, the pill's number (LiveWorkersOf): a working one is working, one in error error, a
// blocked one only in NeedsYou (its menu is an item), any other idle; an ended one is not in the
// team (D260). NeedsYou is the badge of its sidebar row (D240): its own menu and those of its
// workers, its questions and to-dos.
func (f *Frame) chatTeam(addr string) (chatview.Team, bool) {
	snap := f.snapshot()
	var o agents.Worker
	found := false
	for _, w := range snap.Orchestrators {
		if w.Address == addr && w.Live {
			o, found = w, true
			break
		}
	}
	if addr == "" || !found || !snap.Orchestrating(o) {
		return chatview.Team{}, false
	}
	var t chatview.Team
	for _, w := range snap.Workers {
		if w.Parent != addr || !w.Live {
			continue
		}
		t.Agents++
		switch w.Status {
		case "working":
			t.Working++
		case "error":
			t.Error++
		case "blocked":
		default:
			t.Idle++
		}
	}
	if f.SpacesList != nil {
		t.NeedsYou = f.SpacesList.ItemsOf(addr)
	}
	return t, true
}

// openNeeds opens the notch's card as ⌘J does: the click on "N needs you" of the team row. An
// open card stays open.
func (f *Frame) openNeeds() {
	if f.Notch == nil || (f.Notch.Opened != nil && f.Notch.Opened()) {
		return
	}
	f.Notch.Toggle()
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

// chatSwitch is the Chat | Terminal switch at the top right of the terminal panel, or nil when
// the selected terminal is not a Claude Code session.
func (f *Frame) chatSwitch() func(c *ui.Context) {
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
	ui.Box(c).Absolute().Fill().Children(func() { f.Chat.View(c) })
	return true
}

// chatOutline gives the outline the ticks of the chat while it shows (D270): one for each mark of
// the chat, in the room of its transcript list. A pin or a question takes its text from the owner
// store while it is open; an open question and a decision that waits are blocked. A click scrolls
// the chat to the row of the tick.
func (f *Frame) chatOutline() (questions.ChatTicks, bool) {
	if f.Chat == nil || !f.chatShown() {
		return questions.ChatTicks{}, false
	}
	marks := f.Chat.Marks()
	snap := f.snapshot()
	rows := make(map[string]int, len(marks))
	ticks := make([]questions.OutlineTick, 0, len(marks))
	for _, m := range marks {
		t := questions.OutlineTick{ID: m.ID, Headline: m.Text, Pinned: m.At, Kind: m.Kind.Word(), Blocked: m.Waits}
		switch m.Kind {
		case chatview.MarkAsked:
			t.Headline = m.Kind.Word() + " " + m.Item
			for _, q := range snap.Questions {
				if q.ID == m.Item {
					t.Headline, t.Blocked = q.Question, true
				}
			}
		case chatview.MarkPinned:
			t.Headline = m.Kind.Word() + " " + m.Item
			for _, td := range snap.Todos {
				if td.ID == m.Item {
					t.Headline = td.Text
				}
			}
		}
		t.Headline = strings.Join(strings.Fields(t.Headline), " ")
		t.Width = questions.TickWidth(t.Headline)
		rows[m.ID] = m.Row
		ticks = append(ticks, t)
	}
	top, bottom, _ := f.Chat.Area()
	return questions.ChatTicks{Addr: f.selectedTerminal(), Ticks: ticks, Top: top, Bottom: bottom,
		Jump: func(id string) {
			if i, ok := rows[id]; ok {
				f.Chat.ScrollToRow(i)
			}
		}}, true
}
