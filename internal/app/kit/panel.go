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
	// Title is the head text when it differs from Name, such as the project
	// name over the files. Name stays the label of the group.
	Title string
	// Head builds the icon buttons at the right of the head. Nil for none.
	Head func()
	// Footer builds the 24px icon buttons at the right of the footer. Nil
	// for no footer.
	Footer func()
	// Top builds sections above the head, such as the agent's todos over
	// the files. They do not scroll. Nil for none.
	Top func()
	// Body builds the content, which scrolls inside the panel.
	Body func()
	// Bare draws only the box: no head, no padding and no scroll. The body
	// fills the box and clips to its corners. For a body that has its own
	// scroll, such as the terminal or a list.
	Bare bool
	// Stage draws the box with the `stage` fill and the `panel-line` border:
	// the terminal panel. Without it the panel is a wing: no fill and no
	// border, on the window glass (D250).
	Stage bool
}

// Panel builds a rounded area inside the window glass: a head, the content in
// a ui.Scroll, and a footer. Only the content scrolls.
func Panel(c *ui.Context, o PanelOpts) ui.Element {
	p := theme.Current()
	if o.Bare {
		return box(ui.Column(c).Label(o.Name).Grow(1).MinWidth(0).MinHeight(0).Clip(), o.Stage, 0, 0, 0).Children(func() {
			if o.Body != nil {
				o.Body()
			}
		})
	}
	return box(ui.Column(c).Label(o.Name).Grow(1).MinWidth(0).MinHeight(0), o.Stage, theme.Space12, theme.Space10, theme.Space10).Children(func() {
		if o.Top != nil {
			o.Top()
		}
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space4).Shrink(0).Padding(theme.Space2, theme.Space4, theme.Space4, theme.Space10).Children(func() {
			title := o.Title
			if title == "" {
				title = o.Name
			}
			ui.Text(c, title).Label(title + " name").SingleLine().Grow(1).MinWidth(0).
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
			// No hairline over the footer (D258): its padding keeps it apart.
			ui.Row(c).AlignItems(ui.Center).Justify(ui.End).Gap(theme.Space2).Shrink(0).Padding(theme.Space6, 0, 0).Children(func() {
				o.Footer()
			})
		}
	})
}

// box gives el the panel padding (top, sides, bottom) and the rounded shape.
// The stage also gets its fill and its 1px border. A wing gets 1px more
// padding in place of the border, so its content stays where it is on the
// stage (D250).
func box(el ui.Element, stage bool, top, sides, bottom float32) ui.Element {
	el = el.Radius(theme.Radius12)
	if stage {
		p := theme.Current()
		return el.Padding(top, sides, bottom).Background(p.Stage).Border(1, p.PanelLine)
	}
	return el.Padding(top+1, sides+1, bottom+1)
}
