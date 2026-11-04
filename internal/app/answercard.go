package app

// The answer card: a card that floats over the bottom of the terminal area,
// above the needs-you row, and opens when a tab of the row is clicked. A
// question shows its options (the recommended one first and filled) and a
// field for a free answer; a to-do shows Done; a blocked worker shows its
// question and one button for each choice of its menu (#148). The owner's answer goes to the
// coordinator that asked, through [AgentSource.Answer] (#162, S5).

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/owner"
	"github.com/contursofran/contr/internal/prompt"
)

// keyOptions are the digit keys; the key at index i sends option place i.
var keyOptions = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// Labels of the answer card.
const (
	LabelAnswerCard  = "answer card"
	LabelAnswerInput = "answer input"
	CardSend         = "Send"
	CardClose        = "Close"
	CardFocusWorker  = "Focus worker"
	CardDoneButton   = "Done"
	// CardPromptChanged shows when the worker's menu changed after the owner
	// saw it; nothing was sent.
	CardPromptChanged = "The prompt changed; look again."
)

// CardOption is one option of a question, in the order the card shows it.
type CardOption struct {
	Text        string
	Recommended bool
}

// CardOptions are the options of q with the recommended one first. Other
// options keep their order.
func CardOptions(q owner.Question) []CardOption {
	var out []CardOption
	for i, o := range q.Options {
		if q.Recommended != nil && *q.Recommended == i {
			out = append([]CardOption{{Text: o, Recommended: true}}, out...)
			continue
		}
		out = append(out, CardOption{Text: o})
	}
	return out
}

// FloatCard is the answer card that floats over the terminal. Its methods run on the UI thread.
type FloatCard struct {
	Source AgentSource
	Run    Runner
	// Invalidate redraws the window; may be nil.
	Invalidate func()
	// Select selects the worker at an address (the Focus button of a blocked
	// tab); may be nil.
	Select func(address string, orchestrator bool)

	open   *NeedTab
	text   string // the free answer being typed
	err    string // the last failure, shown in the foot
	busy   bool
	handle ui.Handle
	focus  bool // focus the card on the next build

	// The menu of a blocked worker. seen is the prompt the owner sees; stamp
	// is the worker's turn and message when it was read.
	menu    *prompt.Prompt
	stamp   string
	reading bool
}

// Open shows the card for t.
func (a *FloatCard) Open(t NeedTab) {
	a.open, a.text, a.err, a.busy, a.focus = &t, "", "", false, true
	a.menu, a.stamp, a.reading = nil, "", false
	a.redraw()
}

// Close hides the card.
func (a *FloatCard) Close() {
	a.open = nil
	a.redraw()
}

// Opened returns the tab the card shows, or false when it is closed.
func (a *FloatCard) Opened() (NeedTab, bool) {
	if a.open == nil {
		return NeedTab{}, false
	}
	return *a.open, true
}

func (a *FloatCard) redraw() {
	if a.Invalidate != nil {
		a.Invalidate()
	}
}

// question finds the open question of the card.
func (a *FloatCard) question(snap agentstate.Snapshot) (owner.Question, bool) {
	if a.open == nil {
		return owner.Question{}, false
	}
	i := slices.IndexFunc(snap.Questions, func(q owner.Question) bool { return q.ID == a.open.Key })
	if i < 0 {
		return owner.Question{}, false
	}
	return snap.Questions[i], true
}

// send answers the question of the card. The file and the send to the
// coordinator run off the UI thread. A failure stays in the card's foot; the
// card closes only on success.
func (a *FloatCard) send(answer string) {
	if a.open == nil || a.busy || a.open.Kind != NeedQuestion || answer == "" || resolvedID(a.Source.Snapshot(), a.open.Key) {
		return
	}
	id := a.open.Key
	a.busy = true
	a.Run.Do(func() func() {
		err := a.Source.Answer(id, answer)
		return func() {
			a.busy = false
			if a.open == nil || a.open.Key != id {
				return
			}
			if err != nil {
				a.err = err.Error()
			} else {
				a.open = nil
			}
			a.redraw()
		}
	})
}

// done marks the to-do of the card done.
func (a *FloatCard) done() {
	if a.open == nil || a.busy || a.open.Kind != NeedTodo || resolvedID(a.Source.Snapshot(), a.open.Key) {
		return
	}
	id := a.open.Key
	a.busy = true
	a.Run.Do(func() func() {
		err := a.Source.Done(id)
		return func() {
			a.busy = false
			if a.open == nil || a.open.Key != id {
				return
			}
			if err != nil {
				a.err = err.Error()
			} else {
				a.open = nil
			}
			a.redraw()
		}
	})
}

// enter is the Enter key: the typed answer, else the recommended option, else
// Done for a to-do.
func (a *FloatCard) enter(snap agentstate.Snapshot) {
	if a.open == nil {
		return
	}
	switch a.open.Kind {
	case NeedQuestion:
		if a.text != "" {
			a.send(a.text)
			return
		}
		if q, ok := a.question(snap); ok {
			if opts := CardOptions(q); len(opts) > 0 && opts[0].Recommended {
				a.send(opts[0].Text)
			}
		}
	case NeedTodo:
		a.done()
	}
}

// option is a digit key: the option at place pos (0 for "1").
func (a *FloatCard) option(snap agentstate.Snapshot, pos int) {
	if a.open != nil && a.open.Kind == NeedBlocked {
		if a.menu != nil && pos < len(a.menu.Choices) {
			a.choose(pos)
		}
		return
	}
	if q, ok := a.question(snap); ok {
		if opts := CardOptions(q); pos < len(opts) {
			a.send(opts[pos].Text)
		}
	}
}

func resolvedID(snap agentstate.Snapshot, id string) bool { return slices.Contains(snap.Resolved, id) }

// View builds the card, or nothing while it is closed. It is placed in its
// parent, at the bottom, so the parent must be the terminal area.
func (a *FloatCard) View(c *ui.Context) {
	if a.open == nil {
		return
	}
	snap := a.Source.Snapshot()
	t := *a.open
	resolved := resolvedID(snap, t.Key)
	root := ui.Column(c).Label(LabelAnswerCard).Absolute().
		Left(theme.AnswerMargin).Right(theme.AnswerMargin).Bottom(theme.AnswerMargin).
		Height(theme.AnswerCardHeight).Gap(theme.AnswerGap).Padding(theme.AnswerPad).
		Background(theme.Color(theme.BgAnswer)).Border(1, theme.Color(theme.AnswerBorder)).
		Radius(theme.AnswerRadius).Focusable().FocusRing(false).Bind(&a.handle)
	root = root.OnShortcut(0, ui.KeyEscape, a.Close).
		OnShortcut(0, ui.KeyEnter, func() { a.enter(snap) })
	for pos, key := range keyOptions {
		if pos >= 4 && t.Kind != NeedBlocked {
			break
		}
		root = root.OnShortcut(0, key, func() { a.option(snap, pos) })
	}
	if t.Kind == NeedBlocked {
		a.watchBlocked(snap, t)
	}
	if a.focus {
		a.focus = false
		a.handle.Focus(c)
	}
	root.Children(func() {
		headLine(c, &t.Icon, t.Key+"  "+t.Title, resolved, t.Project)
		cardText(c, a.body(t, snap, resolved))
		ui.Row(c).Shrink(0).Wrap().Gap(theme.AnswerButtonGap).Children(func() { a.actions(c, t, snap, resolved) })
		if t.Kind == NeedQuestion && !resolved {
			a.input(c)
		}
		if msg := a.foot(snap); msg != "" {
			ui.Text(c, msg).SingleLine().Shrink(0).TextColor(theme.Color(theme.TextDim))
		}
	})
}

// body is the text under the head line.
func (a *FloatCard) body(t NeedTab, snap agentstate.Snapshot, resolved bool) *string {
	text := t.Title
	switch t.Kind {
	case NeedBlocked:
		if a.menu != nil && a.menu.Question != "" {
			// The detail first: for a permission, the tool and its command.
			text = strings.Join(append(slices.Clone(a.menu.Detail), a.menu.Question), "\n")
		}
	case NeedQuestion:
		if q, ok := a.question(snap); ok {
			text = q.Question
		}
	case NeedTodo:
		text = ""
		if i := slices.IndexFunc(snap.Todos, func(o owner.Todo) bool { return o.ID == t.Key }); i >= 0 {
			text = snap.Todos[i].Text
		}
	}
	if resolved {
		text = "Done."
	}
	return &text
}

// foot is the line at the bottom: the failure of the last action, else the
// problem of the state.
func (a *FloatCard) foot(snap agentstate.Snapshot) string {
	if a.err != "" {
		return a.err
	}
	return snap.Problem
}

func (a *FloatCard) actions(c *ui.Context, t NeedTab, snap agentstate.Snapshot, resolved bool) {
	switch {
	case resolved:
	case t.Kind == NeedQuestion:
		if q, ok := a.question(snap); ok {
			for _, o := range CardOptions(q) {
				cardButton(c, o.Text, OptionStyle(o.Recommended), theme.Text, func() { a.send(o.Text) })
			}
		}
	case t.Kind == NeedTodo:
		cardButton(c, CardDoneButton, ButtonFilled, theme.Text, a.done)
	case t.Kind == NeedBlocked:
		if a.menu != nil {
			for i, ch := range a.menu.Choices {
				cardButton(c, ch.Label, OptionStyle(i == 0), theme.Text, func() { a.choose(i) })
			}
		}
		style := ButtonFilled
		if a.menu != nil {
			style = ButtonPlain
		}
		cardButton(c, CardFocusWorker, style, theme.Text, func() { a.focusWorker(t) })
	}
	cardButton(c, CardClose, ButtonPlain, theme.TextDim, a.Close)
}

// input is the field for a free answer, with its Send button.
func (a *FloatCard) input(c *ui.Context) {
	ui.Row(c).Shrink(0).Gap(theme.AnswerButtonGap).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c, &a.text).Label(LabelAnswerInput).Placeholder("Type an answer").Grow(1).MinWidth(0)
		if in.Submitted() {
			a.send(a.text)
		}
		cardButton(c, CardSend, ButtonOutlined, theme.Text, func() { a.send(a.text) })
	})
}

// findBlocked finds the worker of a blocked card.
func findBlocked(snap agentstate.Snapshot, name string) (agents.Worker, bool) {
	for _, w := range append(append([]agents.Worker(nil), snap.Orchestrators...), snap.Workers...) {
		if w.Name == name {
			return w, true
		}
	}
	return agents.Worker{}, false
}

// watchBlocked runs while the card of a blocked worker builds. It closes the
// card when the worker is no longer blocked, and reads the worker's screen
// again when its turn or message changed (event driven: it follows the
// snapshot, with no timer).
func (a *FloatCard) watchBlocked(snap agentstate.Snapshot, t NeedTab) {
	w, ok := findBlocked(snap, t.Key)
	if !ok || w.Status != "blocked" {
		a.open = nil
		return
	}
	stamp := strconv.FormatUint(w.Turn, 10) + "|" + w.Msg
	if a.reading || stamp == a.stamp {
		return
	}
	a.stamp, a.reading = stamp, true
	a.Run.Do(func() func() {
		p, ok, err := a.readMenu(t)
		return func() {
			a.reading = false
			if a.open == nil || a.open.Key != t.Key {
				return
			}
			a.menu = nil
			if ok {
				a.menu = &p
			}
			if err != nil {
				a.err = err.Error()
			}
			a.redraw()
		}
	})
}

// readMenu reads the screen of the worker of t and parses its menu.
func (a *FloatCard) readMenu(t NeedTab) (prompt.Prompt, bool, error) {
	screen, err := a.Source.Read(t.Address, promptLines)
	if err != nil {
		return prompt.Prompt{}, false, err
	}
	p, ok := prompt.Parse(t.Command, t.BlockKind, screen)
	return p, ok, nil
}

// promptLines is how many terminal lines the card reads to find the menu.
const promptLines = 40

// choose picks choice i of the menu. A choice that asks for typed text
// focuses the worker. Otherwise the screen is read and parsed again, and the
// keys go only when the menu is the one the owner saw.
func (a *FloatCard) choose(i int) {
	if a.open == nil || a.busy || a.menu == nil || i >= len(a.menu.Choices) {
		return
	}
	t, seen := *a.open, *a.menu
	ch := seen.Choices[i]
	if ch.Text {
		a.focusWorker(t)
		return
	}
	a.busy = true
	a.Run.Do(func() func() {
		now, ok, err := a.readMenu(t)
		changed := err == nil && !(ok && now.Equal(seen))
		if err == nil && !changed {
			err = a.Source.SendKeys(t.Address, ch.Keys)
		}
		return func() {
			a.busy = false
			if a.open == nil || a.open.Key != t.Key {
				return
			}
			switch {
			case err != nil:
				a.err = err.Error()
			case changed:
				a.err = CardPromptChanged
				a.menu = nil
				if ok {
					a.menu = &now
				}
			default:
				a.err = ""
			}
			a.redraw()
		}
	})
}

func (a *FloatCard) focusWorker(t NeedTab) {
	if a.Select != nil {
		a.Select(t.Address, t.Orchestrator)
	}
	a.Close()
}
