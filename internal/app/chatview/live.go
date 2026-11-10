package chatview

// The live line (docs/spec/ChatView.md, D252): between the transcript and the composer, what the
// agent does now. The transcript gets Claude's text only when a block ends, so this line tells
// the owner that work goes on.

import (
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/questions"
	"github.com/contursofran/contr/internal/app/tabmodel"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/prompt"
)

// Labels and texts of the live line.
const (
	// LabelLive names the live line.
	LabelLive = "live line"
	// TextNeedsYou shows when the session needs the owner and contr cannot read its menu.
	TextNeedsYou = "The terminal needs you"
)

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

// menuState is the menu of a session that needs the owner, read from its screen as the
// questions card reads it. stamp is the worker's turn and message when it was read.
type menuState struct {
	menu    *prompt.Prompt
	stamp   string
	reading bool
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

// menuOf reads and answers the menu of w through the chat's sends.
func (ch *Chat) menuOf(w agents.Worker) questions.Menu {
	cmd := w.Command
	if cmd == "" {
		cmd = w.Profile
	}
	return questions.Menu{Address: w.Address, Command: cmd, Kind: w.Kind, Read: ch.Read, SendKeys: ch.SendKeys}
}

// watchMenu reads the menu of a blocked session when its turn or message changed: it follows the
// worker, with no timer. A session that is not blocked has no menu.
func (ch *Chat) watchMenu(st liveState) {
	if st.kind != liveNeedsYou || st.w.Status != "blocked" || ch.Read == nil {
		ch.menu = menuState{}
		return
	}
	stamp := st.w.Address + "|" + strconv.FormatUint(st.w.Turn, 10) + "|" + st.w.Msg
	if ch.menu.reading || ch.menu.stamp == stamp {
		return
	}
	ch.menu = menuState{stamp: stamp, reading: true}
	m := ch.menuOf(st.w)
	ch.Env.Do(func() func() {
		p, ok, err := m.ReadMenu()
		return func() {
			if ch.menu.stamp != stamp {
				return
			}
			ch.menu.reading = false
			if ok {
				ch.menu.menu = &p
			}
			if err != nil {
				ch.errs[st.w.Address] = err.Error()
			}
			ch.Env.Redraw()
		}
	})
}

// choose sends choice i of the menu shown, as the questions card does: the screen is read again
// and the keys go only when the menu is the one the owner saw (D51). A choice that asks for
// typed text switches to Terminal.
func (ch *Chat) choose(st liveState, i int, toTerminal func()) {
	mn := ch.menu.menu
	if mn == nil || i < 0 || i >= len(mn.Choices) || ch.sending || ch.SendKeys == nil {
		return
	}
	if mn.Choices[i].Text {
		toTerminal()
		return
	}
	seen, id, stamp := *mn, st.w.Address, ch.menu.stamp
	m := ch.menuOf(st.w)
	ch.sending = true
	ch.Env.Do(func() func() {
		now, ok, changed, err := m.Choose(seen, i)
		return func() {
			ch.sending = false
			switch {
			case err != nil:
				ch.errs[id] = err.Error()
			case changed:
				ch.errs[id] = questions.QuestionsPromptChanged
				if ch.menu.stamp == stamp {
					ch.menu.menu = nil
					if ok {
						ch.menu.menu = &now
					}
				}
			}
			ch.Env.Redraw()
		}
	})
}

// digit takes a digit typed into the empty field while a menu shows: it sends that choice at
// once and clears the field, as the key does in the terminal (D140). A menu takes no Enter.
func (ch *Chat) digit(id string, st liveState, draft *string) {
	if st.kind != liveNeedsYou || ch.menu.menu == nil || len(*draft) != 1 {
		return
	}
	n := int((*draft)[0] - '0')
	if n < 1 || n > 9 || n > len(ch.menu.menu.Choices) {
		return
	}
	*draft = ""
	ch.choose(st, n-1, ch.toTerm)
}

// live builds the live line of st. Working: the working glyph, what the agent does now and the
// time since the turn started, which asks for a redraw each second while it shows (D55). Needs
// you: the question and its choices, or the fallback with Switch to Terminal. Idle: nothing.
func (ch *Chat) live(c *ui.Context, st liveState, toTerminal func()) {
	pal := theme.Current()
	switch st.kind {
	case liveWorking:
		what, start, ok := ch.model.Activity()
		column(c, func() {
			ui.Row(c).Label(LabelLive+" "+what).FillWidth().Height(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
				kit.StatusGlyph(c, kit.GlyphOpts{State: kit.GlyphWorking, Small: true})
				kit.Styled(ui.Text(c, what), theme.StyleValue).SingleLine().MinWidth(0).TextColor(pal.Ink62)
				if ok {
					kit.Styled(ui.Text(c, ElapsedText(c.Now().Sub(start))), theme.StyleValue).NoWrap().Shrink(0).TextColor(pal.Ink38)
					c.After(time.Second)
				}
			})
		})
	case liveNeedsYou:
		ch.watchMenu(st)
		column(c, func() { ch.needsYou(c, pal, st, toTerminal) })
	default:
		ch.watchMenu(st)
	}
}

// needsYou is the line of a session that waits on the owner: a blocked dot, the question in
// ink-100 and the choices as buttons; without a menu, "The terminal needs you" and Switch to
// Terminal.
func (ch *Chat) needsYou(c *ui.Context, pal *theme.Palette, st liveState, toTerminal func()) {
	mn := ch.menu.menu
	question := TextNeedsYou
	if mn != nil {
		question = mn.Question
	}
	ui.Column(c).Label(LabelLive+" "+question).FillWidth().Gap(theme.Space8).Children(func() {
		ui.Row(c).FillWidth().MinHeight(lineHeight).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			ch.dotBox(c, st.w.DisplayName()+" needs you", pal.Blocked)
			kit.Styled(ui.Text(c, question), theme.StyleBody).Wrap().Grow(1).MinWidth(0).TextColor(pal.Ink100)
		})
		ui.Row(c).FillWidth().Wrap().Gap(theme.Space8).Padding(0, 0, 0, dotSize+theme.Space8).Children(func() {
			if mn == nil {
				kit.CardButton(c, TextToTerminal, kit.ButtonOutlined, pal.Ink85, toTerminal)
				return
			}
			for i, choice := range mn.Choices {
				if i >= 9 {
					break
				}
				kit.CardButton(c, ChoiceText(i, choice.Label), kit.ButtonOutlined, pal.Ink85, func() { ch.choose(st, i, toTerminal); ch.field.Focus() })
			}
		})
	})
}

// ChoiceText is the text of a choice button: "1 Yes".
func ChoiceText(i int, label string) string { return strconv.Itoa(i+1) + " " + label }
