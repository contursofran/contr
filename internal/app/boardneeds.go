// Ported from the Rust contr module board_needs.

package app

// The needs-you section of the board panel: the "Needs you N" head with the
// "clear done" control, then one plain row per item in the coordinator's
// order. The icons are static, so the rows need no cache. Read-only: rows
// never move on their own.

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

// Labels of the needs-you section.
const (
	LabelNeedsSection = "needs section"
	LabelNeedsHeader  = "needs header"
	LabelNeedsRows    = "needs rows"
	LabelNeedsEmpty   = "Nothing waits for you."
)

// NeedsSection adds the needs-you section. It takes the rest of the panel's
// height, and its rows scroll inside it. picked is the key of the picked
// item ("" when the pick is not an item). A click on a row calls onPick with
// the row's key. onClear is the "clear done" control of the head (nil when
// the board takes no actions); it shows only when clearItems is true.
func NeedsSection(c *ui.Context, items []board.NeedItem, needCount uint32, clearItems bool, picked string, onPick func(key string), onClear ActionClick) {
	ui.Column(c).Label(LabelNeedsSection).FillWidth().Grow(1).MinHeight(0).Children(func() {
		needsHead(c, needCount, clearItems, onClear)
		ui.Scroll(c).Label(LabelNeedsRows).FillWidth().Grow(1).MinHeight(0).Children(func() {
			if len(items) == 0 {
				TextLine(c, LabelNeedsEmpty, LabelNeedsEmpty, theme.TextDim)
				return
			}
			for _, item := range items {
				needRow(c, item, item.Key == picked, onPick)
			}
		})
	})
}

// NeedsCountLabel is the label of the open count in the head.
func NeedsCountLabel(n uint32) string { return "needs count " + strconv.FormatUint(uint64(n), 10) }

// needsHead adds "Needs you", the open count in bold, then "clear done"
// while the section has resolved items.
func needsHead(c *ui.Context, needCount uint32, clearItems bool, onClear ActionClick) {
	ui.Row(c).Label(LabelNeedsHeader).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(theme.PanelPadX).Children(func() {
		ui.Text(c, "Needs you").TextColor(theme.Color(theme.TextDim))
		ui.Text(c, strconv.FormatUint(uint64(needCount), 10)).Label(NeedsCountLabel(needCount)).
			Bold().TextColor(theme.Color(theme.Text))
		if clearItems {
			ClearDone(c, LabelClearNeed, onClear)
		}
	})
}

// needRow adds one item: the status icon, the key, the title and the kind
// word on the right. A resolved item has its title struck through. A picked
// item draws as a selected card, and a click picks it.
func needRow(c *ui.Context, item board.NeedItem, picked bool, onPick func(key string)) {
	text := NeedText(item)
	el := ui.ButtonBase(c).Label(NeedLabel(item)).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).Gap(theme.NeedGap).PaddingX(theme.PanelPadX).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).OnClick(func() {
		if onPick != nil {
			onPick(item.Key)
		}
	})
	if picked {
		el = selected(el)
	}
	el.Children(func() {
		icons.Sized(c, board.ItemStatus(item), theme.IconSizeSmall)
		ui.Text(c, item.Key).Width(theme.NeedKeyWidth).Shrink(0).TextColor(theme.Color(text))
		title := ui.Text(c, item.Title).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").TextColor(theme.Color(text))
		if !item.Open {
			title.Strikethrough()
		}
		ui.Text(c, board.ItemKindLabel(item)).Shrink(0).TextColor(theme.Color(theme.TextDim))
	})
}

// NeedText: open asks and to-dos wait on the owner, so their key and title
// read in Text. Resolved items and news read in TextDim.
func NeedText(item board.NeedItem) uint32 {
	if item.Open && (item.Kind == board.ItemAsk || item.Kind == board.ItemDo) {
		return theme.Text
	}
	return theme.TextDim
}

// NeedLabel is the accessible name of a row: "{key} {title}, {kind label}".
func NeedLabel(item board.NeedItem) string {
	return item.Key + " " + item.Title + ", " + board.ItemKindLabel(item)
}
