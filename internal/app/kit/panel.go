package kit

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// PanelOpts describes a Panel (docs/spec/Panel.md).
type PanelOpts struct {
	// Name is the head text. It is lower-case where it is a name, and clips
	// with an ellipsis. The panel is a group named by it.
	Name string
	// Head builds the icon buttons at the right of the head. Nil for none.
	Head func()
	// Footer builds the 24px icon buttons at the right of the footer. Nil
	// for no footer.
	Footer func()
	// Body builds the content, which scrolls inside the panel.
	Body func()
	// Bare draws only the box: no head, no padding and no scroll. The body
	// fills the box and clips to its corners. For a body that has its own
	// scroll, such as the terminal or a list.
	Bare bool
}

// Panel builds a rounded area inside the window glass: a head, the content in
// a ui.Scroll, and a footer. Only the content scrolls.
func Panel(c *ui.Context, o PanelOpts) ui.Element {
	p := theme.Current()
	if o.Bare {
		return ui.Column(c).Label(o.Name).Grow(1).MinWidth(0).MinHeight(0).Clip().
			Background(p.Panel).Border(1, p.PanelLine).Radius(theme.Radius12).Children(func() {
			if o.Body != nil {
				o.Body()
			}
		})
	}
	return ui.Column(c).Label(o.Name).Grow(1).MinWidth(0).MinHeight(0).Padding(theme.Space12, theme.Space10, theme.Space10).
		Background(p.Panel).Border(1, p.PanelLine).Radius(theme.Radius12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0).Padding(theme.Space2, theme.Space4, theme.Space4, theme.Space10).Children(func() {
			ui.Text(c, o.Name).Label(o.Name + " name").SingleLine().Grow(1).MinWidth(0).
				TextColor(p.Ink45).FontSize(theme.StyleLabel.Size).FontWeight(theme.StyleLabel.Weight)
			if o.Head != nil {
				o.Head()
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			if o.Body != nil {
				o.Body()
			}
		})
		if o.Footer != nil {
			ui.Box(c).Height(1).Shrink(0).Background(p.Hairline)
			ui.Row(c).AlignItems(ui.Center).Justify(ui.End).Gap(theme.Space2).Shrink(0).Padding(theme.Space6, 0, 0).Children(func() {
				o.Footer()
			})
		}
	})
}
