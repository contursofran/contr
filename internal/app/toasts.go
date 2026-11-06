package app

// Toasts (D126): the short messages of an action, such as a failed save, show as MyGo toasts near
// the bottom of the window. The v0.2 status bar showed them before. Any goroutine can queue a message in
// [state.Toasts]; the view gives each to MyGo, which times and stacks them.

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// toastMaxWidth is the widest a toast grows; a longer message wraps.
const toastMaxWidth = 420

// Labels of the toasts.
const (
	LabelToastViewport = "toasts"
	LabelToastClose    = "Close message"
)

// toastView adds the queued messages and builds the toasts without MyGo's look: glass-float with
// float-line, as the other floating parts (docs/spec/README.md).
func (f *Frame) toastView(c *ui.Context) {
	if f.Toasts != nil {
		for _, m := range f.Toasts.Take() {
			c.AddToast(ui.Toast{ID: m, Title: m})
		}
	}
	f.queueUpdateToast(c)
	p := theme.Current()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Label(LabelToastViewport).Padding(0, 0, theme.Space16, 0).AlignItems(ui.Center).Gap(theme.Space8)
		for _, t := range toasts {
			if _, ok := t.Data.(updateToastData); ok {
				f.updateToast(c, t)
				continue
			}
			toast := ui.ToastBase(c, t)
			toast.Root.Row().AlignItems(ui.Center).Gap(theme.Space10).MaxWidth(toastMaxWidth).
				Padding(theme.Space8, theme.Space8, theme.Space8, theme.Space16).Radius(theme.Radius12).
				Background(p.GlassFloat).Border(1, p.FloatLine)
			for _, l := range p.ShadowFloat {
				if !l.Inset {
					toast.Root.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
				}
			}
			toast.Root.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{}})
			toast.Root.Children(func() {
				ui.Text(c, t.Title).Grow(1).MinWidth(0).TextColor(p.Ink85)
				toast.CloseButton().Label(LabelToastClose).Size(kit.SizeIconButtonSmall, kit.SizeIconButtonSmall).
					Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Radius(theme.Radius8).TextColor(p.Ink50).
					Cursor(ui.CursorPointer).Children(func() {
					if s := icons.Line("close"); s != nil {
						ui.Icon(c, s).Size(12, 12)
					}
				})
			})
		}
	})
}
