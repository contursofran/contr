package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// flatRow is the token look of a sidebar row (docs/spec/ProjectRow.md,
// docs/spec/ServerRow.md): row-height, radius-8, a hover fill and no card.
// Selected turns the name and the icon ink-100 instead of drawing a fill.
// The name takes NameInk (ink-60 by default) and NameStyle (body); Meta
// sits at the right in MetaInk (ink-38) and the small style.
func flatRow(c *ui.Context, o RowOpts) {
	p := theme.Current()
	label := o.Label
	if label == "" {
		label = o.Text
	}
	nameInk, metaInk := o.NameInk, o.MetaInk
	if nameInk == (ui.Color{}) {
		nameInk = p.Ink60
	}
	if metaInk == (ui.Color{}) {
		metaInk = p.Ink38
	}
	if o.Selected {
		nameInk = p.Ink100
	}
	style := o.NameStyle
	if style.Size == 0 {
		style = theme.StyleBody
	}
	row := ui.ButtonBase(c).Label(label).Height(theme.SizeRowHeight).Shrink(0).AlignItems(ui.Center).
		Gap(theme.Space10).PaddingX(theme.Space10).MarginX(theme.Space6).Radius(theme.Radius8).
		TextColor(nameInk).Cursor(ui.CursorPointer).Selected(o.Selected)
	if o.Expanded != nil {
		row = row.Expanded(*o.Expanded)
	}
	// ButtonBase paints a selected button in the accent color; the explicit
	// background keeps it flat: only the ink shows the selection.
	if row.Hovered() {
		row = row.Background(p.Hover)
	} else {
		row = row.Background(ui.Transparent)
	}
	if o.OnClick != nil {
		row = row.OnClick(o.OnClick)
	}
	row.Children(func() {
		if o.Lead != nil {
			o.Lead()
		}
		ui.Text(c, o.Text).Label(label + " text").Grow(1).MinWidth(0).SingleLine().
			Font(style.Family()).FontSize(style.Size).FontWeight(style.Weight).TextColor(nameInk)
		if o.Meta != "" {
			ui.Text(c, o.Meta).SingleLine().Shrink(0).MaxWidthPercent(60).
				Font(theme.StyleSmall.Family()).FontSize(theme.StyleSmall.Size).TextColor(metaInk)
		}
		if o.Trail != nil {
			o.Trail()
		}
	})
}
