// Ported from the Rust contr modules board_panel and board_card.

package app

// The picks of the board panel and the faces of its answer card. The panel
// resolves the pick, reads the queue, and builds the card's face with its
// clicks. The card (card.go) only draws.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// shownItems are the items as the panel draws them: an item with an answer
// queued reads as resolved, with that answer. Every other item is as the
// board has it.
func (p *BoardPanel) shownItems(s *board.Snapshot) []board.NeedItem {
	items := make([]board.NeedItem, len(s.Items))
	for i, item := range s.Items {
		items[i] = ShownItem(item, p.queuedAnswer(item.Key))
	}
	return items
}

// queuedAnswer is the answer queued for key, or nil when nothing is queued.
func (p *BoardPanel) queuedAnswer(key string) *string {
	if text, _, ok := p.pendingAnswer(key); ok {
		return &text
	}
	return nil
}

// shownPick is the pick that the card shows on s: the pick, or else the first
// open item.
func (p *BoardPanel) shownPick(s *board.Snapshot) (BoardPick, bool) {
	return EffectivePick(s, p.pick)
}

// SetPick picks pick: the card shows it, and the picked work row draws as
// selected.
func (p *BoardPanel) SetPick(pick BoardPick) {
	p.pick = &pick
	p.syncPicks()
	p.invalidate()
}

// dropGonePick drops a pick that is no longer on the board, so that the card
// falls back to the first open item.
func (p *BoardPanel) dropGonePick(s *board.Snapshot) {
	if s != nil && p.pick != nil && !NamesOnBoard(s, *p.pick) {
		p.pick = nil
	}
}

// syncPicks marks the work row that the card names as picked, and no other.
// The needs section draws its own picked item, so only rows are set here.
func (p *BoardPanel) syncPicks() {
	key := ""
	if s := p.deps.Board.View().Snapshot; s != nil {
		if shown, ok := p.shownPick(s); ok && shown.Kind == PickRow {
			key = shown.Key
		}
	}
	// The return value only says if the pick changed. The caller redraws the
	// panel in any case, so it is not needed here.
	p.work.SetPicked(key)
}

// pickedItemKey is the key of the picked item, or "" when the shown pick is a
// row or nothing.
func (p *BoardPanel) pickedItemKey(s *board.Snapshot) string {
	if shown, ok := p.shownPick(s); ok && shown.Kind == PickItem {
		return shown.Key
	}
	return ""
}

// pickClick is a click that picks pick, as a click on a row does.
func (p *BoardPanel) pickClick(pick BoardPick) ClickFn {
	return func() { p.SetPick(pick) }
}

// sendsClick is a click that sends action through the queue, with no status
// line.
func (p *BoardPanel) sendsClick(action board.Action) ClickFn {
	return func() { p.sendAction(action, "") }
}

// undoAnswer cancels the answer, done or dismiss queued for key, when it is
// still held.
func (p *BoardPanel) undoAnswer(key string) {
	if p.deps.Outbox != nil {
		p.deps.Outbox.Undo(key)
	}
}

// chat is "Answer in chat": it sends chat for key, then focuses the bridge
// pane, so that the owner lands in the coordinator's prompt. Nothing is
// focused when the send fails or the board has no bridge pane.
func (p *BoardPanel) chat(key string) {
	if !p.sendAction(board.Chat(key), "") {
		return
	}
	if bridge := p.deps.Board.View().Bridge; bridge != nil {
		p.focus(bridge.PaneID)
	}
}

// focus focuses pane through the panel's Focus call.
func (p *BoardPanel) focus(pane string) {
	focus := p.deps.Focus
	if focus == nil {
		focus = FocusInBackground
	}
	focus(pane, p.deps.Status)
}

// open opens url through the panel's Open call, or the browser.
func (p *BoardPanel) open(c *ui.Context, url string) {
	if p.deps.Open != nil {
		p.deps.Open(c, url)
		return
	}
	c.OpenURL(url)
}

// openClick is a click that opens url.
func (p *BoardPanel) openClick(c *ui.Context, url string) ClickFn {
	return func() { p.open(c, url) }
}

// tree is the herdr tree now, or nil.
func (p *BoardPanel) tree() *herdr.HerdrTree {
	if p.deps.Tree == nil {
		return nil
	}
	return p.deps.Tree()
}

// currentFace is the face that the card draws now. It is nil without a
// snapshot, off the Board tab, or with nothing picked and no item open.
func (p *BoardPanel) currentFace(c *ui.Context) CardFace {
	s := p.deps.Board.View().Snapshot
	if s == nil || p.tab != TabBoard {
		return nil
	}
	shown, ok := p.shownPick(s)
	if !ok {
		return nil
	}
	canAct := p.CanAct()
	switch shown.Kind {
	case PickItem:
		items := p.shownItems(s)
		for _, item := range items {
			if item.Key == shown.Key {
				return p.itemFace(c, item, items, canAct)
			}
		}
	case PickRow:
		for _, row := range s.Rows {
			if row.Key == shown.Key {
				return p.rowFace(c, row)
			}
		}
	}
	return nil
}

// itemFace is the face of the item card for item, one of items as shown. The
// actions follow the item's state: an answer queued for it, then an open
// decision, to-do or news item, else a resolved item. With canAct false the
// buttons that send an action have no click. Undo, Open link and Next still
// work, because they do not send.
func (p *BoardPanel) itemFace(c *ui.Context, item board.NeedItem, items []board.NeedItem, canAct bool) *ItemFace {
	var actions ItemActions
	_, undoable, queued := p.pendingAnswer(item.Key)
	switch {
	case queued:
		actions = ItemActions{Kind: ActionsResolved, Answer: ResolvedAnswer(item), Sending: !undoable}
		if undoable {
			key := item.Key
			actions.Undo = func() { p.undoAnswer(key) }
		}
	case !item.Open:
		actions = ItemActions{Kind: ActionsResolved, Answer: ResolvedAnswer(item)}
	case item.Kind == board.ItemAsk:
		actions = ItemActions{Kind: ActionsDecide}
		for _, o := range board.CardOptions(item) {
			face := OptionFace{Label: o.Text, Recommended: o.Recommended}
			if canAct {
				face.Click = p.sendsClick(board.Answer(item.Key, uint64(o.Index), o.Text))
			}
			actions.Options = append(actions.Options, face)
		}
		if canAct {
			key := item.Key
			actions.Chat = func() { p.chat(key) }
		}
	case item.Kind == board.ItemDo:
		actions = ItemActions{Kind: ActionsDo}
		if canAct {
			actions.Done = p.sendsClick(board.Done(item.Key))
		}
		if item.Link != nil {
			actions.Link = p.openClick(c, LinkURL(*item.Link))
		}
	default:
		actions = ItemActions{Kind: ActionsKnow}
		if canAct {
			actions.Dismiss = p.sendsClick(board.Dismiss(item.Key))
		}
	}
	face := &ItemFace{Item: item, Actions: actions, Inert: !canAct}
	if next := board.NextOpen(items, item.Key, func(o board.NeedItem) bool { return o.Open }); next != nil {
		face.NextKey = next.Key
		face.NextClick = p.pickClick(ItemPick(next.Key))
	}
	return face
}

// pendingAnswer is the answer queued for key, with whether it can still be
// undone. ok is false when nothing is queued.
func (p *BoardPanel) pendingAnswer(key string) (text string, undoable, ok bool) {
	if p.deps.Outbox == nil {
		return "", false, false
	}
	return p.deps.Outbox.PendingAnswer(key)
}

// rowFace is the face of a work row. Focus needs a pane that the tree holds
// in a tab, Report needs a report, and Open PR needs a URL.
func (p *BoardPanel) rowFace(c *ui.Context, row board.WorkRow) *RowFace {
	face := &RowFace{Row: row}
	if row.PaneID != nil {
		if tab, ok := TabLabelForPane(p.tree(), *row.PaneID); ok {
			pane := *row.PaneID
			face.FocusTab = tab
			face.FocusClick = func() { p.focus(pane) }
		}
	}
	if row.Report != nil {
		key := row.Key
		face.ReportClick = func() { p.ShowReport(key) }
	}
	if row.URL != nil {
		face.OpenPRClick = p.openClick(c, *row.URL)
	}
	return face
}
