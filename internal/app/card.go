// Ported from the Rust contr module board_card.

package app

// The answer card: the fixed 168 px frame at the foot of the board panel. It
// shows what the pick names: a needs-you item with its actions, or a work row
// with its Focus, Report and Open PR buttons. The card only draws. The panel
// resolves the pick, builds the faces and makes the clicks.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
	"github.com/contursofran/contr/internal/herdr"
)

// The labels of the card buttons that do not change with the face.
const (
	LabelCard      = "answer card"
	AnswerInChat   = "Answer in chat"
	CardDone       = "Done"
	CardOpenLink   = "Open link"
	CardDismiss    = "Dismiss"
	CardUndo       = "undo"
	CardReport     = "Report"
	CardOpenPR     = "Open PR"
	CardSending    = "· sending"
	CardInertNote  = "coordinator mod takes no actions"
	cardAnswerMark = "✓ "
)

// ClickFn is a click on a card button.
type ClickFn func()

// PickKind says what a pick names.
type PickKind int

// The kinds of pick.
const (
	PickRow PickKind = iota
	PickItem
)

// BoardPick is what the owner picked: a work row or a needs-you item, by
// its slot key.
type BoardPick struct {
	Kind PickKind
	Key  string
}

// RowPick picks the work row with key.
func RowPick(key string) BoardPick { return BoardPick{PickRow, key} }

// ItemPick picks the needs-you item with key.
func ItemPick(key string) BoardPick { return BoardPick{PickItem, key} }

// NamesOnBoard is true when pick names a row or an item of snap.
func NamesOnBoard(snap *board.Snapshot, pick BoardPick) bool {
	switch pick.Kind {
	case PickRow:
		for _, r := range snap.Rows {
			if r.Key == pick.Key {
				return true
			}
		}
	case PickItem:
		for _, it := range snap.Items {
			if it.Key == pick.Key {
				return true
			}
		}
	}
	return false
}

// EffectivePick is the pick the card shows on snap: pick when it names
// something on the board, else the first open item (the mod's default). ok
// is false when no item is open either.
func EffectivePick(snap *board.Snapshot, pick *BoardPick) (BoardPick, bool) {
	if pick != nil && NamesOnBoard(snap, *pick) {
		return *pick, true
	}
	for _, it := range snap.Items {
		if it.Open {
			return ItemPick(it.Key), true
		}
	}
	return BoardPick{}, false
}

// CardItem is the item that a card key acts on: the item the card shows. It
// is nil for a work row, or with no pick.
func CardItem(items []board.NeedItem, shown *BoardPick) *board.NeedItem {
	if shown == nil || shown.Kind != PickItem {
		return nil
	}
	for i := range items {
		if items[i].Key == shown.Key {
			return &items[i]
		}
	}
	return nil
}

// SteppedPick is the pick that an up or down key moves to, from the pick the
// card shows. Down goes to the next item in slot order, up to the one
// before, and the ends stop. Down from a work row, or with no pick, goes to
// the first item. Up from a work row, or with no pick, does not move. ok is
// false when the key does not move the pick.
func SteppedPick(items []board.NeedItem, shown *BoardPick, down bool) (BoardPick, bool) {
	index := -1
	if shown != nil && shown.Kind == PickItem {
		for i := range items {
			if items[i].Key == shown.Key {
				index = i
				break
			}
		}
	}
	target := -1
	switch {
	case index >= 0 && down:
		target = index + 1
	case index >= 0:
		target = index - 1
	case down:
		target = 0
	}
	if target < 0 || target >= len(items) {
		return BoardPick{}, false
	}
	return ItemPick(items[target].Key), true
}

// EnterAction is the action that enter sends for item, the same as its main
// button: the recommended option of an open decision (none when no option is
// recommended), Done for an open to-do, Dismiss for open news. ok is false
// for a resolved item.
func EnterAction(item board.NeedItem) (board.Action, bool) {
	if !item.Open {
		return board.Action{}, false
	}
	switch item.Kind {
	case board.ItemAsk:
		if opts := board.CardOptions(item); len(opts) > 0 && opts[0].Recommended {
			return answerFor(item, opts[0]), true
		}
		return board.Action{}, false
	case board.ItemDo:
		return board.Done(item.Key), true
	}
	return board.Dismiss(item.Key), true
}

// OptionAction is the action that the digit key for position (0 for "1")
// sends for item: the option at that place in the card's order, the same as
// its option button. Only an open decision has options. ok is false when
// there is no such option.
func OptionAction(item board.NeedItem, position int) (board.Action, bool) {
	if !item.Open || item.Kind != board.ItemAsk {
		return board.Action{}, false
	}
	opts := board.CardOptions(item)
	if position < 0 || position >= len(opts) {
		return board.Action{}, false
	}
	return answerFor(item, opts[position]), true
}

func answerFor(item board.NeedItem, o board.CardOption) board.Action {
	return board.Answer(item.Key, uint64(o.Index), o.Text)
}

// ItemCardLabel is the accessible name of an item card: "{key} {title},
// {kind label}". A decision adds its option labels after "options", in the
// card's order.
func ItemCardLabel(item board.NeedItem, actions ItemActions) string {
	base := item.Key + " " + item.Title + ", " + board.ItemKindLabel(item)
	if actions.Kind != ActionsDecide {
		return base
	}
	labels := make([]string, len(actions.Options))
	for i, o := range actions.Options {
		labels[i] = o.Label
	}
	return base + ", options " + strings.Join(labels, ", ")
}

// TabLabelForPane is the label of the tab that holds the pane paneID. ok is
// false when no tab of tree holds it.
func TabLabelForPane(tree *herdr.HerdrTree, paneID string) (string, bool) {
	if tree == nil {
		return "", false
	}
	for _, w := range tree.Workspaces {
		for _, t := range w.Tabs {
			for _, p := range t.Panes {
				if p.ID == paneID {
					return t.Label, true
				}
			}
		}
	}
	return "", false
}

// CardFace is what the card shows. The data is owned, so the card does not
// borrow the panel's snapshot. It is nil, *ItemFace or *RowFace; nil draws
// the frame with nothing in it (nothing picked and no item open).
type CardFace interface{ cardFace() }

// ItemFace is a needs-you item on the card.
type ItemFace struct {
	// Item is the item as the card draws it. An item with an answer queued
	// reads as resolved (see ShownItem).
	Item    board.NeedItem
	Actions ItemActions
	// NextKey is the key of the next open item, with NextClick that picks
	// it. Empty when no other item is open.
	NextKey   string
	NextClick ClickFn
	// Inert is true when the board takes no actions: the foot says so, and
	// the item buttons do nothing.
	Inert bool
}

// ActionsKind is the kind of an item's actions line.
type ActionsKind int

// The kinds of actions line.
const (
	// ActionsDecide is an open decision: one button per option in card
	// order, then "Answer in chat".
	ActionsDecide ActionsKind = iota
	// ActionsDo is an open to-do: "Done", then "Open link" when the item has
	// a link.
	ActionsDo
	// ActionsKnow is open news: "Dismiss".
	ActionsKnow
	// ActionsResolved is a resolved item, or one with an answer queued:
	// "✓ {answer}", then "undo" while the answer can still be taken back,
	// then "· sending" once it is written.
	ActionsResolved
)

// ItemActions is the actions line of an item. A click that is nil means the
// board takes no actions: the button is drawn dim and does nothing. Link and
// Undo are drawn only when set.
type ItemActions struct {
	Kind ActionsKind
	// Options are the decision's options (ActionsDecide), with Chat.
	Options []OptionFace
	Chat    ClickFn
	// Done and Link are for ActionsDo, Dismiss for ActionsKnow.
	Done    ClickFn
	Link    ClickFn
	Dismiss ClickFn
	// Answer, Undo and Sending are for ActionsResolved.
	Answer  string
	Undo    ClickFn
	Sending bool
}

// OptionFace is one option of a decision. Recommended makes it the filled
// button.
type OptionFace struct {
	Label       string
	Recommended bool
	Click       ClickFn
}

// ButtonStyle says how a card button is drawn.
type ButtonStyle int

// The button styles.
const (
	// ButtonFilled is white, with bold dark text: the owner acts.
	ButtonFilled ButtonStyle = iota
	// ButtonOutlined is outlined grey, with the text colour.
	ButtonOutlined
	// ButtonPlain has no border; its text is underlined on hover.
	ButtonPlain
)

// OptionStyle is the style of an option button: the recommended option is
// filled, the others are outlined.
func OptionStyle(recommended bool) ButtonStyle {
	if recommended {
		return ButtonFilled
	}
	return ButtonOutlined
}

// ShownItem is the item as the card and the needs-you rows draw it. An item
// with an answer queued reads as resolved, with that answer, until the mod
// takes the action or refuses it. queued nil changes nothing.
func ShownItem(item board.NeedItem, queued *string) board.NeedItem {
	if queued != nil {
		answer := *queued
		item.Open = false
		item.Answer = &answer
	}
	return item
}

// ResolvedAnswer is the answer that a resolved item shows: its answer, or
// "done" when it has none.
func ResolvedAnswer(item board.NeedItem) string {
	if item.Answer != nil {
		return *item.Answer
	}
	return "done"
}

// LinkURL is the URL that a to-do's link opens. A link without "://" gets
// "http://" in front.
func LinkURL(link string) string {
	if strings.Contains(link, "://") {
		return link
	}
	return "http://" + link
}

// RowFace is a work row on the card. A button is drawn only when its click
// is set.
type RowFace struct {
	Row board.WorkRow
	// FocusTab is the label of the tab that holds the row's pane, with
	// FocusClick: the "Focus {tab}" button.
	FocusTab   string
	FocusClick ClickFn
	// ReportClick: set when the row has a report.
	ReportClick ClickFn
	// OpenPRClick: set when the row has a URL.
	OpenPRClick ClickFn
}

func (*ItemFace) cardFace() {}
func (*RowFace) cardFace()  {}

// ItemMeta is the "from · ago" text of an item. A part the item lacks is
// left out. The ago part reads "now" for a time under a minute old.
func ItemMeta(item board.NeedItem, nowMS uint64) string {
	var parts []string
	if item.From != nil && *item.From != "" {
		parts = append(parts, *item.From)
	}
	if item.At != nil {
		if s := board.Ago(nowMS, *item.At); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// FocusLabel is the label of a row's Focus button.
func FocusLabel(tab string) string { return "Focus " + tab }

// NextLabel is the label of the card's Next button.
func NextLabel(key string) string { return "Next: " + key }

// AnswerCard builds the card with what face shows. nowMS dates the ago part
// of an item's meta line.
func AnswerCard(c *ui.Context, face CardFace, nowMS uint64) {
	label := LabelCard
	var head func()
	var text *string
	var actions, foot func()
	switch f := face.(type) {
	case *ItemFace:
		label = ItemCardLabel(f.Item, f.Actions)
		head = func() {
			headLine(c, cardPtr(board.ItemStatus(f.Item)), f.Item.Key+"  "+f.Item.Title, !f.Item.Open, ItemMeta(f.Item, nowMS))
		}
		text = f.Item.Text
		actions = func() { itemActions(c, f.Actions) }
		foot = func() { itemFoot(c, f) }
	case *RowFace:
		head = func() { headLine(c, nil, f.Row.Label+"  "+f.Row.Title, false, "") }
		text = f.Row.Now
		actions = func() { rowButtons(c, f) }
	}
	ui.Column(c).Label(label).Margin(0, theme.AnswerMargin, theme.AnswerMargin).
		Height(theme.AnswerCardHeight).Shrink(0).Gap(theme.AnswerGap).Padding(theme.AnswerPad).
		Background(theme.Color(theme.BgAnswer)).Border(1, theme.Color(theme.AnswerBorder)).
		Radius(theme.AnswerRadius).Children(func() {
		if head != nil {
			head()
		}
		cardText(c, text)
		ui.Row(c).Shrink(0).Wrap().Gap(theme.AnswerButtonGap).Children(func() {
			if actions != nil {
				actions()
			}
		})
		ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).Children(func() {
			if foot != nil {
				foot()
			}
		})
	})
}

func cardPtr[T any](v T) *T { return &v }

// headLine is the head line: a status icon (items only), the title in bold
// (cut with an ellipsis, struck through when resolved), and meta in dim text
// on the right.
func headLine(c *ui.Context, icon *icons.Status, title string, resolved bool, meta string) {
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).Gap(theme.NeedGap).Children(func() {
		if icon != nil {
			ui.Box(c).Shrink(0).Children(func() { icons.Sized(c, *icon, theme.IconSizeSmall) })
		}
		t := ui.Text(c, title).Bold().SingleLine().Grow(1).MinWidth(0).TextColor(theme.Color(theme.Text))
		if resolved {
			t.Strikethrough()
		}
		if meta != "" {
			ui.Text(c, meta).Shrink(0).TextColor(theme.Color(theme.TextDim))
		}
	})
}

// cardText is the text of the card, in Text2, at most three lines. A longer
// text is clipped. The text takes the room that the actions line does not,
// so the actions stay at the bottom.
func cardText(c *ui.Context, text *string) {
	ui.Box(c).Grow(1).MinHeight(0).Clip().Children(func() {
		s := ""
		if text != nil {
			s = *text
		}
		ui.Column(c).MaxHeight(theme.AnswerTextHeight).Clip().Children(func() {
			ui.Text(c, s).TextColor(theme.Color(theme.Text2)).Wrap()
		})
	})
}

// rowButtons adds the buttons of a row, in order: "Focus {tab}", "Report",
// "Open PR". Each is drawn only when set.
func rowButtons(c *ui.Context, f *RowFace) {
	if f.FocusClick != nil {
		cardButton(c, FocusLabel(f.FocusTab), ButtonOutlined, theme.Text, f.FocusClick)
	}
	if f.ReportClick != nil {
		cardButton(c, CardReport, ButtonOutlined, theme.Text, f.ReportClick)
	}
	if f.OpenPRClick != nil {
		cardButton(c, CardOpenPR, ButtonOutlined, theme.Text, f.OpenPRClick)
	}
}

// itemActions adds the actions line of an item, in order. See ItemActions.
func itemActions(c *ui.Context, a ItemActions) {
	switch a.Kind {
	case ActionsDecide:
		for _, o := range a.Options {
			cardButton(c, o.Label, OptionStyle(o.Recommended), theme.Text, o.Click)
		}
		cardButton(c, AnswerInChat, ButtonPlain, theme.TextDim, a.Chat)
	case ActionsDo:
		cardButton(c, CardDone, ButtonFilled, theme.Text, a.Done)
		if a.Link != nil {
			cardButton(c, CardOpenLink, ButtonOutlined, theme.Text, a.Link)
		}
	case ActionsKnow:
		cardButton(c, CardDismiss, ButtonOutlined, theme.Text, a.Dismiss)
	case ActionsResolved:
		ui.Text(c, cardAnswerMark+a.Answer).Shrink(0).TextColor(theme.Color(theme.Text2))
		if a.Undo != nil {
			cardButton(c, CardUndo, ButtonPlain, theme.Text, a.Undo)
		}
		if a.Sending {
			ui.Text(c, CardSending).Shrink(0).TextColor(theme.Color(theme.TextDim))
		}
	}
}

// itemFoot adds the foot of an item card: the inert note on the left when
// the board takes no actions, and "Next: {key}" on the right.
func itemFoot(c *ui.Context, f *ItemFace) {
	if f.Inert {
		ui.Text(c, CardInertNote).Shrink(0).TextColor(theme.Color(theme.TextDim))
	}
	if f.NextClick != nil {
		ui.Box(c).Grow(1)
		cardButton(c, NextLabel(f.NextKey), ButtonPlain, theme.TextDim, f.NextClick)
	}
}

// cardButton is a card button. With click nil the board takes no actions:
// the button is dim and does nothing. The button's accessible name is its
// label. colour is the text colour of a live plain button.
func cardButton(c *ui.Context, label string, style ButtonStyle, colour uint32, click ClickFn) {
	live := click != nil
	el := ui.ButtonBase(c).Label(label).Height(theme.AnswerButtonHeight).Shrink(0).
		PaddingX(theme.AnswerButtonPadX).AlignItems(ui.Center).Radius(theme.AnswerButtonRadius)
	text := uint32(theme.TextDim)
	switch {
	case style == ButtonFilled && live:
		el = el.Background(theme.Color(theme.YouBg))
		text = theme.YouText
	case style == ButtonPlain:
		if live {
			text = colour
		}
	default:
		el = el.Border(1, theme.Color(theme.CardBorder))
		if live {
			text = theme.Text
		}
	}
	if live {
		el = el.Cursor(ui.CursorPointer).OnClick(click)
	}
	el = el.TextColor(theme.Color(text))
	el.Children(func() {
		t := ui.Text(c, label)
		if style == ButtonFilled && live {
			t = t.Bold()
		}
		if style == ButtonPlain && live && el.Hovered() {
			t.Underline()
		}
	})
}
