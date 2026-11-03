package app

// The answer card: a card that floats over the bottom of the terminal area,
// above the needs-you row, and opens when a tab of the row is clicked. A
// question shows its options (the recommended one first and filled) and a
// field for a free answer; a to-do shows Done; a blocked worker shows what it
// says and a button that selects it. The owner's answer goes to the
// coordinator that asked, through [AgentSource.Answer] (#162, S5).

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/owner"
)

// keyOptions are the digit keys; the key at index i sends option place i.
var keyOptions = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4}

// Labels of the answer card.
const (
	LabelAnswerCard  = "answer card"
	LabelAnswerInput = "answer input"
	CardSend         = "Send"
	CardClose        = "Close"
	CardFocusWorker  = "Focus worker"
	CardDoneButton   = "Done"
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
}

// Open shows the card for t.
func (a *FloatCard) Open(t NeedTab) {
	a.open, a.text, a.err, a.busy, a.focus = &t, "", "", false, true
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
		root = root.OnShortcut(0, key, func() { a.option(snap, pos) })
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
		cardButton(c, CardFocusWorker, ButtonFilled, theme.Text, func() {
			if a.Select != nil {
				a.Select(t.Address, t.Orchestrator)
			}
			a.Close()
		})
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
