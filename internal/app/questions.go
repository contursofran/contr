package app

// The questions card (docs/spec/QuestionsCard.md, D124, D125): the open notch.
// It shows one item at a time: a question with its choices and a field for a
// free answer, a to-do with its done action, or the menu of a blocked worker.
// A chip for each item switches between them. The answer goes to the
// coordinator that asked through [AgentSource.Answer] (D36), and a blocked
// worker's choice goes to its terminal (D51). This file holds the state and
// the actions; questionsview.go builds the card.

import (
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/owner"
	"github.com/contursofran/contr/internal/prompt"
)

// Labels and texts of the questions card.
const (
	// LabelQuestionsChips names the chips of the card.
	LabelQuestionsChips   = "Items"
	LabelQuestionsField   = "Or type an answer"
	LabelQuestionsDetails = "Details"
	QuestionsDone         = "Done"
	QuestionsFocusWorker  = "Focus worker"
	// QuestionsPromptChanged shows when the worker's menu changed after the
	// owner saw it; nothing was sent.
	QuestionsPromptChanged = "The prompt changed; look again."
	// LabelCopyCode starts the label of a code chip: "Copy <text>" (D204).
	LabelCopyCode = "Copy "
	// QuestionsCopied is the toast after a click on a code chip.
	QuestionsCopied = "Copied"
)

// keyOptions are the digit keys; the key at index i picks place i.
var keyOptions = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// promptLines is how many terminal lines the card reads to find a menu.
const promptLines = 40

// CardOption is one option of a question, in the order the card shows it.
type CardOption struct {
	Text        string
	Recommended bool
	// Impact is what the option means (D114); empty when the question has none.
	Impact string
}

// CardOptions are the options of q with the recommended one first. Other
// options keep their order. The nth impact goes with the nth option.
func CardOptions(q owner.Question) []CardOption {
	var out []CardOption
	for i, o := range q.Options {
		opt := CardOption{Text: o}
		if i < len(q.Impacts) {
			opt.Impact = q.Impacts[i]
		}
		if q.Recommended != nil && *q.Recommended == i {
			opt.Recommended = true
			out = append([]CardOption{opt}, out...)
			continue
		}
		out = append(out, opt)
	}
	return out
}

func resolvedID(snap agentstate.Snapshot, id string) bool { return slices.Contains(snap.Resolved, id) }

// QuestionsCard is the card of the notch. Its methods run on the UI thread.
type QuestionsCard struct {
	Source AgentSource
	Run    Runner
	// Entry opens the pull request or issue of a blocked worker (D106).
	Entry WorkerEntry
	// Invalidate redraws the window; may be nil.
	Invalidate func()
	// Select selects the worker at an address (Focus worker); may be nil.
	Select func(address string, orchestrator bool)
	// Status shows a short message as a toast (D126); may be nil.
	Status func(string)

	open    bool
	key     string // the item shown
	idx     int    // its chip
	text    string // the free answer being typed
	picked  int    // place of the picked choice, or -1
	details bool
	err     string // the last failure, shown under the field
	busy    bool
	handle  ui.Handle
	focus   bool // focus the card on the next build

	// The menu of a blocked worker. stamp is the worker's turn and message
	// when the menu was read.
	menu    *prompt.Prompt
	stamp   string
	reading bool
}

// Open shows the card for t.
func (q *QuestionsCard) Open(t NeedTab) {
	q.open, q.focus = true, true
	q.reset(t.Key)
	q.err = ""
	q.redraw()
}

// Close hides the card.
func (q *QuestionsCard) Close() {
	q.open = false
	q.redraw()
}

// IsOpen tells whether the card shows.
func (q *QuestionsCard) IsOpen() bool { return q.open }

func (q *QuestionsCard) redraw() {
	if q.Invalidate != nil {
		q.Invalidate()
	}
}

// reset starts a new item: the field, the pick, Details and the menu clear.
func (q *QuestionsCard) reset(key string) {
	q.key, q.text, q.picked, q.details = key, "", -1, false
	q.menu, q.stamp, q.reading = nil, "", false
}

// show switches to chip i. The card takes the focus again: the control that
// had it is gone, and the keys must keep working.
func (q *QuestionsCard) show(items []NeedTab, i int) {
	q.idx, q.focus = i, true
	q.reset(items[i].Key)
	q.err = ""
	q.redraw()
}

// sync finds the chip of the item shown. An item that went (answered here or
// elsewhere) is dropped: the card moves to the item now at its place, or the
// last one. The failure of the last action stays. items must not be empty.
func (q *QuestionsCard) sync(items []NeedTab) int {
	if i := slices.IndexFunc(items, func(t NeedTab) bool { return t.Key == q.key }); i >= 0 {
		q.idx = i
		return i
	}
	q.idx = min(q.idx, len(items)-1)
	err := q.err
	q.reset(items[q.idx].Key)
	q.err = err
	return q.idx
}

// next is Tab: the next chip, from the last to the first.
func (q *QuestionsCard) next(items []NeedTab) {
	if len(items) > 1 {
		q.show(items, (q.idx+1)%len(items))
	}
}

func (q *QuestionsCard) question(snap agentstate.Snapshot) (owner.Question, bool) {
	i := slices.IndexFunc(snap.Questions, func(o owner.Question) bool { return o.ID == q.key })
	if i < 0 {
		return owner.Question{}, false
	}
	return snap.Questions[i], true
}

func (q *QuestionsCard) todo(snap agentstate.Snapshot) (owner.Todo, bool) {
	i := slices.IndexFunc(snap.Todos, func(o owner.Todo) bool { return o.ID == q.key })
	if i < 0 {
		return owner.Todo{}, false
	}
	return snap.Todos[i], true
}

// act runs do off the UI thread. A failure stays in the card;
// the item goes from the chips when the state marks it resolved.
func (q *QuestionsCard) act(do func() error) {
	if q.busy {
		return
	}
	q.busy = true
	q.Run.Do(func() func() {
		err := do()
		return func() {
			q.busy = false
			q.err = ""
			if err != nil {
				q.err = err.Error()
			}
			q.redraw()
		}
	})
}

// answer sends the answer to the question shown.
func (q *QuestionsCard) answer(id, answer string) {
	if answer == "" || resolvedID(q.Source.Snapshot(), id) {
		return
	}
	q.act(func() error { return q.Source.Answer(id, answer) })
}

// done marks the to-do shown done.
func (q *QuestionsCard) done(id string) {
	if resolvedID(q.Source.Snapshot(), id) {
		return
	}
	q.act(func() error { return q.Source.Done(id) })
}

// enter is the Enter key: the typed text, else the picked choice, else the
// recommended one; for a to-do, done. A blocked menu takes no Enter, so a
// stray key never approves a permission (D51).
func (q *QuestionsCard) enter(t NeedTab, snap agentstate.Snapshot) {
	switch t.Kind {
	case NeedQuestion:
		if q.text != "" {
			q.answer(t.Key, q.text)
			return
		}
		o, ok := q.question(snap)
		if !ok {
			return
		}
		opts := CardOptions(o)
		switch {
		case q.picked >= 0 && q.picked < len(opts):
			q.answer(t.Key, opts[q.picked].Text)
		case len(opts) > 0 && opts[0].Recommended:
			q.answer(t.Key, opts[0].Text)
		}
	case NeedTodo:
		q.done(t.Key)
	}
}

// digit is a number key: a question picks the choice, a blocked menu sends it.
// The keys reach the first nine choices.
func (q *QuestionsCard) digit(t NeedTab, snap agentstate.Snapshot, pos int) {
	switch t.Kind {
	case NeedQuestion:
		if o, ok := q.question(snap); ok && pos < len(CardOptions(o)) {
			q.picked = pos
			q.redraw()
		}
	case NeedBlocked:
		q.choose(t, pos)
	}
}

// findBlocked finds the worker of a blocked item.
func findBlocked(snap agentstate.Snapshot, name string) (agents.Worker, bool) {
	for _, w := range append(append([]agents.Worker(nil), snap.Orchestrators...), snap.Workers...) {
		if w.Name == name {
			return w, true
		}
	}
	return agents.Worker{}, false
}

// watchBlocked runs while the card of a blocked worker builds. It reads the
// worker's screen when its turn or message changed (event driven: it follows
// the snapshot, with no timer). The item goes from the chips when the worker
// is no longer blocked.
func (q *QuestionsCard) watchBlocked(snap agentstate.Snapshot, t NeedTab) {
	w, ok := findBlocked(snap, t.Key)
	if !ok || w.Status != "blocked" {
		return
	}
	stamp := strconv.FormatUint(w.Turn, 10) + "|" + w.Msg
	if q.reading || stamp == q.stamp {
		return
	}
	q.stamp, q.reading = stamp, true
	q.Run.Do(func() func() {
		p, ok, err := q.readMenu(t)
		return func() {
			q.reading = false
			if q.key != t.Key {
				return
			}
			q.menu = nil
			if ok {
				q.menu = &p
			}
			if err != nil {
				q.err = err.Error()
			}
			q.redraw()
		}
	})
}

// readMenu reads the screen of the worker of t and parses its menu.
func (q *QuestionsCard) readMenu(t NeedTab) (prompt.Prompt, bool, error) {
	screen, err := q.Source.Read(t.Address, promptLines)
	if err != nil {
		return prompt.Prompt{}, false, err
	}
	p, ok := prompt.Parse(t.Command, t.BlockKind, screen)
	return p, ok, nil
}

// choose sends choice i of the menu of t. A choice that asks for typed text
// focuses the worker. Otherwise the screen is read and parsed again, and the
// keys go only when the menu is the one the owner saw (D51).
func (q *QuestionsCard) choose(t NeedTab, i int) {
	if q.busy || q.menu == nil || i >= len(q.menu.Choices) {
		return
	}
	seen := *q.menu
	ch := seen.Choices[i]
	if ch.Text {
		q.focusWorker(t)
		return
	}
	q.busy = true
	q.Run.Do(func() func() {
		now, ok, err := q.readMenu(t)
		changed := err == nil && !(ok && now.Equal(seen))
		if err == nil && !changed {
			err = q.Source.SendKeys(t.Address, ch.Keys)
		}
		return func() {
			q.busy = false
			if q.key != t.Key {
				return
			}
			q.err = ""
			switch {
			case err != nil:
				q.err = err.Error()
			case changed:
				q.err = QuestionsPromptChanged
				q.menu = nil
				if ok {
					q.menu = &now
				}
			}
			q.redraw()
		}
	})
}

func (q *QuestionsCard) focusWorker(t NeedTab) {
	if q.Select != nil {
		q.Select(t.Address, t.Orchestrator)
	}
	q.Close()
}
