// Ported from the Rust contr module board_work.

package app

// The work section of the board panel: a header with the five stage heads,
// then one row per issue or pull request, in the coordinator's order.
//
// MyGo rebuilds a view on every redraw, so the Rust "one cached view per
// row" becomes, as in the agents list (decision D12): every row is a cached
// layer, the working icon draws its own turn, and [WorkList.Sync] tells what
// changed.

import (
	"reflect"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/board"
)

// Labels of the work section.
const (
	LabelWorkSection = "work section"
	LabelWorkHeader  = "work header"
	LabelWorkRows    = "work rows"
	LabelWorkEmpty   = "No workers yet."
)

// WorkChange tells what [WorkList.Sync] changed.
type WorkChange struct {
	// Rebuilt: the keys changed (added, removed or reordered), so the whole
	// list changes.
	Rebuilt bool
	// Rows are the indexes of the rows that show other data, when the keys
	// are the same.
	Rows []int
}

// Any tells whether the section needs a redraw.
func (c WorkChange) Any() bool { return c.Rebuilt || len(c.Rows) > 0 }

// WorkList is the work section. Methods run on the UI thread.
type WorkList struct {
	rows   []board.WorkRow
	picked string
	onPick func(key string)
}

// NewWorkList returns the section. onPick (may be nil) is called with the
// slot key of a clicked row.
func NewWorkList(onPick func(key string)) *WorkList { return &WorkList{onPick: onPick} }

// Rows returns the rows shown, in the coordinator's order.
func (l *WorkList) Rows() []board.WorkRow { return l.rows }

// Sync takes rows and tells what changed. The same rows change nothing.
func (l *WorkList) Sync(rows []board.WorkRow) WorkChange {
	var change WorkChange
	if !sameKeys(l.rows, rows) {
		change.Rebuilt = true
	} else {
		for i := range rows {
			if !reflect.DeepEqual(l.rows[i], rows[i]) {
				change.Rows = append(change.Rows, i)
			}
		}
	}
	l.rows = rows
	return change
}

func sameKeys(a, b []board.WorkRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key {
			return false
		}
	}
	return true
}

// SetPicked shows the row with key as picked ("" for none). It returns
// whether the pick changed.
func (l *WorkList) SetPicked(key string) bool {
	if l.picked == key {
		return false
	}
	l.picked = key
	return true
}

// Pick is a click on a row.
func (l *WorkList) Pick(key string) {
	if l.onPick != nil {
		l.onPick(key)
	}
}

// View builds the section: the header, then the rows or the empty line. The
// section is at most half the panel tall, and the rows scroll inside it.
// onClear is the "clear done" control, or nil when the board takes no
// actions; it shows only when clearRows is true.
func (l *WorkList) View(c *ui.Context, clearRows bool, onClear ActionClick) {
	ui.Column(c).Label(LabelWorkSection).FillWidth().Shrink(0).MaxHeightPercent(50).Children(func() {
		l.header(c, clearRows, onClear)
		if len(l.rows) == 0 {
			TextLine(c, LabelWorkEmpty, LabelWorkEmpty, theme.TextDim)
			return
		}
		ui.Scroll(c).Label(LabelWorkRows).FillWidth().MinHeight(0).Children(func() {
			for i, row := range l.rows {
				l.row(c, row, i)
			}
		})
	})
}

// header adds "Work", the "clear done" control when clearRows, and the five
// stage heads on the right.
func (l *WorkList) header(c *ui.Context, clearRows bool, onClear ActionClick) {
	ui.Row(c).Label(LabelWorkHeader).FillWidth().Height(theme.BoardLineHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.PanelPadX).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(theme.BoardGap).Children(func() {
			ui.Text(c, "Work").TextColor(theme.Color(theme.TextDim))
			if clearRows {
				ClearDone(c, LabelClearWork, onClear)
			}
		})
		for _, head := range board.StageHeads {
			ui.Row(c).Width(theme.StageColWidth).Shrink(0).Justify(ui.Center).Children(func() {
				ui.Text(c, head).TextColor(theme.Color(theme.TextDim))
			})
		}
	})
}

// row adds one work row: the label, the title over who holds it, and the
// five stage cells. A joined row shows its join instead of stages. A picked
// row draws as a selected card.
func (l *WorkList) row(c *ui.Context, row board.WorkRow, index int) {
	el := ui.ButtonBase(c).Label(WorkRowLabel(row)).FillWidth().Height(theme.WorkRowHeight).Shrink(0).
		AlignItems(ui.Center).PaddingX(theme.PanelPadX).Radius(theme.CardRadius).
		Cursor(ui.CursorPointer).Cache().OnClick(func() { l.Pick(row.Key) })
	if row.Key == l.picked {
		el = selected(el)
	}
	el.Children(func() {
		ui.Text(c, row.Label).Width(theme.WorkLabelWidth).Shrink(0).TextColor(theme.Color(theme.TextDim))
		textColumn(c, row)
		if row.Joined == nil {
			for column := range board.StageHeads {
				ui.Row(c).Width(theme.StageColWidth).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					icons.Sized(c, board.StageStatus(row.Stage(column)), theme.IconSizeSmall)
				})
			}
		}
	})
}

// textColumn adds the title over who holds the row. A joined row shows its
// join, dim and struck through.
func textColumn(c *ui.Context, row board.WorkRow) {
	ui.Column(c).Grow(1).MinWidth(0).Justify(ui.Center).Children(func() {
		if row.Joined != nil {
			ui.Text(c, "joined "+*row.Joined).Height(theme.LineHeight).SingleLine().Ellipsis("…").MinWidth(0).
				TextColor(theme.Color(theme.TextDim)).Strikethrough()
			return
		}
		ui.Text(c, row.Title).Height(theme.LineHeight).SingleLine().Ellipsis("…").MinWidth(0).
			TextColor(theme.Color(TitleColour(row)))
		whoLine(c, row)
	})
}

// whoLine adds the second line: a "you" chip when the owner holds the row,
// else the text in its colour.
func whoLine(c *ui.Context, row board.WorkRow) {
	ui.Row(c).Height(theme.LineHeight).Shrink(0).AlignItems(ui.Center).MinWidth(0).Children(func() {
		if row.Who.Kind == board.WhoYou {
			ui.Row(c).Shrink(0).PaddingX(theme.BadgePadX).Radius(theme.BadgeRadius).
				Background(theme.Color(theme.YouBg)).Children(func() {
				ui.Text(c, row.Who.Text).Bold().TextColor(theme.Color(theme.YouText))
			})
			return
		}
		ui.Text(c, row.Who.Text).SingleLine().Ellipsis("…").MinWidth(0).TextColor(theme.Color(WhoColour(row.Who.Kind)))
	})
}

// TitleColour is dim when nothing runs or waits on the row.
func TitleColour(row board.WorkRow) uint32 {
	if row.Finished {
		return theme.TextDim
	}
	return theme.Text
}

// WhoColour: "dim" is quieter than "text". Any other kind reads as "text".
func WhoColour(kind board.WhoKind) uint32 {
	if kind == board.WhoDim {
		return theme.TextDim2
	}
	return theme.Text2
}

// WorkRowLabel is the text a screen reader gets for a row: its label, title,
// the five stages and who holds it. A joined row has no stages, so it reads
// its join.
func WorkRowLabel(row board.WorkRow) string {
	if row.Joined != nil {
		return row.Label + " joined " + *row.Joined
	}
	stages := make([]string, len(board.StageHeads))
	for i, head := range board.StageHeads {
		stages[i] = head + " " + board.StageStatus(row.Stage(i)).Label()
	}
	who := ""
	if row.Who.Text != "" {
		who = ", " + row.Who.Text
	}
	return row.Label + " " + row.Title + ", " + strings.Join(stages, ", ") + who
}
