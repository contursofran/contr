// Ported from the Rust contr module board_panel.

package app

// The keys of the board panel: up and down move the pick, enter sends the
// card's default action, and 1 to 4 send an option. The keys act only while
// the panel has focus (decision D23). On the Report tab they do nothing.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/board"
)

// keyOptions are the digit keys; the key at index i sends option place i.
var keyOptions = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4}

// bindKeys declares the panel's keys on the root element.
func (p *BoardPanel) bindKeys(root ui.Element) ui.Element {
	root = root.OnShortcut(0, ui.KeyUp, func() { p.stepPick(false) }).
		OnShortcut(0, ui.KeyDown, func() { p.stepPick(true) }).
		OnShortcut(0, ui.KeyEnter, p.sendDefault)
	for pos, key := range keyOptions {
		root = root.OnShortcut(0, key, func() { p.sendOption(pos) })
	}
	return root
}

// stepPick moves the pick one item up or, with down, down. Nothing moves on
// the Report tab or without a snapshot.
func (p *BoardPanel) stepPick(down bool) {
	if p.tab == TabReport {
		return
	}
	s := p.deps.Board.View().Snapshot
	if s == nil {
		return
	}
	shown, ok := p.shownPick(s)
	var shownPtr *BoardPick
	if ok {
		shownPtr = &shown
	}
	if pick, moved := SteppedPick(p.shownItems(s), shownPtr, down); moved {
		p.SetPick(pick)
	}
}

// sendForCardItem sends what keyAction gives for the item that the card
// shows, as the card's buttons send theirs. Nothing is sent on the Report
// tab, when the card shows no item, or when the board takes no actions.
func (p *BoardPanel) sendForCardItem(keyAction func(board.NeedItem) (board.Action, bool)) {
	if p.tab == TabReport {
		return
	}
	s := p.deps.Board.View().Snapshot
	if s == nil {
		return
	}
	shown, ok := p.shownPick(s)
	if !ok {
		return
	}
	item := CardItem(p.shownItems(s), &shown)
	if item == nil {
		return
	}
	if action, ok := keyAction(*item); ok {
		p.sendAction(action, "")
	}
}

// sendDefault is enter: the card's default action. See [EnterAction].
func (p *BoardPanel) sendDefault() { p.sendForCardItem(EnterAction) }

// sendOption is the digit key for pos (0 for "1"): the option at that place
// in the card's order.
func (p *BoardPanel) sendOption(pos int) {
	p.sendForCardItem(func(item board.NeedItem) (board.Action, bool) { return OptionAction(item, pos) })
}
