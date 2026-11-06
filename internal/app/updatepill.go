package app

// The update pill (docs/spec/UpdatePill.md, D176): when a background install is ready, the title
// bar shows "Restart to update" left of the diff summary. A click opens a panel under it
// (PopoverBase) with the version, the release notes and "Restart now". The first time a version
// is ready, one toast drops under the pill. The methods run on the UI thread.

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
)

// Labels and texts of the update pill, its panel and its toast.
const (
	LabelUpdatePill  = "Restart to update"
	LabelUpdatePanel = "update panel"
	LabelUpdateToast = "update toast"
	UpdateLater      = "Later"
	UpdateWhatsNew   = "What's new"
	UpdateToastBody  = "It downloaded in the background. Restart when you like: agents keep running."
	UpdateAgentsNote = "Agents keep running. Their terminals live in hc; contr opens them again after the restart, in about 2 s."
)

// updateWidth is the width of the update panel and toast.
const updateWidth = 340

// updateNotesMax is the most release-note lines the panel lists.
const updateNotesMax = 6

// updateToastData marks the update toast among the toasts, so the viewport draws it under the pill.
type updateToastData struct{ Version string }

// updateUI holds the pill's state.
type updateUI struct {
	open bool
	// toasted holds the versions whose toast showed: one toast per version.
	toasted map[string]bool
	// pending is a version whose toast waits for the next build.
	pending string
	// pill is the pill of this build; zero when it does not show.
	pill ui.Element
}

// updateReady is Settings.OnReady: it queues the one toast of the version.
func (f *Frame) updateReady(info UpdateInfo) {
	if f.upd.toasted == nil {
		f.upd.toasted = map[string]bool{}
	}
	if f.upd.toasted[info.Version] {
		return
	}
	f.upd.toasted[info.Version] = true
	f.upd.pending = info.Version
}

// updatePill builds the pill and its panel while an update is ready.
func (f *Frame) updatePill(c *ui.Context) {
	f.upd.pill = ui.Element{}
	if f.Settings == nil {
		return
	}
	info, at, ok := f.Settings.Installed()
	if !ok {
		return
	}
	p := theme.Current()
	pill := ui.ButtonBase(c).Label(LabelUpdatePill).Height(theme.SizeIconButton).Shrink(0).
		PaddingX(theme.Space10).Gap(theme.Space6).AlignItems(ui.Center).Radius(theme.SizeIconButton/2).
		Border(1, p.FloatLine).Cursor(ui.CursorPointer).MarginEnd(theme.Space8).
		OnClick(func() { f.upd.open = !f.upd.open })
	if pill.Hovered() || f.upd.open {
		pill = pill.Background(p.Hover)
	}
	pill.TextColor(p.Ink100).Children(func() {
		if s := icons.Line("download"); s != nil {
			ui.Icon(c, s).Size(14, 14).Shrink(0)
		}
		ui.Text(c, LabelUpdatePill).NoWrap()
	})
	f.upd.pill = pill
	ui.PopoverBase(c, pill, &f.upd.open, func(panel ui.Element) {
		panel.Label(LabelUpdatePanel).AttachTo(pill, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(theme.Space8, 0, 0, 0)
		floatCard(panel)
		if panel.OverlayShortcut(0, ui.KeyEnter) {
			f.Settings.RestartNow()
		}
		panel.Children(func() { f.updatePanel(c, info, at) })
	})
}

// floatCard gives a floating card its look: glass-float, float-line and shadow-float.
func floatCard(el ui.Element) {
	p := theme.Current()
	el.Column().Width(updateWidth).Radius(theme.Radius12).Background(p.GlassFloat).Border(1, p.FloatLine)
	for _, l := range p.ShadowFloat {
		if !l.Inset {
			el.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
}

// updatePanel is the panel body: head, What's new, the agents note and the foot.
func (f *Frame) updatePanel(c *ui.Context, info UpdateInfo, at time.Time) {
	p := theme.Current()
	meta := theme.StyleMeta
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space14, theme.Space16, theme.Space10, theme.Space16).Children(func() {
		ui.Text(c, "contr "+info.Version).Bold().Grow(1).TextColor(p.Ink95)
		ui.Text(c, "from "+f.Settings.Version).Font(meta.Stack()).FontSize(meta.Size).TextColor(p.Ink50)
	})
	if notes := noteLines(info.Notes); len(notes) > 0 {
		ui.Column(c).Gap(theme.Space4).Padding(0, theme.Space16, theme.Space12, theme.Space16).Children(func() {
			ui.Text(c, UpdateWhatsNew).TextColor(p.Ink50)
			for _, n := range notes {
				ui.Row(c).Gap(theme.Space6).Children(func() {
					ui.Text(c, "·").Shrink(0).TextColor(p.Ink50)
					ui.Text(c, n).Grow(1).MinWidth(0).TextColor(p.Ink85)
				})
			}
		})
	}
	ui.Row(c).Gap(theme.Space8).Margin(0, theme.Space16, theme.Space12, theme.Space16).Padding(theme.Space8, theme.Space10).
		Radius(theme.Radius8).Background(p.Hover).TextColor(p.Ink62).Children(func() {
		if s := icons.Line("server"); s != nil {
			ui.Icon(c, s).Size(14, 14).Shrink(0)
		}
		ui.Text(c, UpdateAgentsNote).Grow(1).MinWidth(0)
	})
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space10, theme.Space16).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		ui.Text(c, downloadedText(time.Now(), at)).NoWrap().Grow(1).TextColor(p.Ink50)
		settingsButton(c, UpdateLater, ButtonPlain, func() { f.upd.open = false })
		settingsButton(c, SettingsRestart+" ⏎", ButtonFilled, f.Settings.RestartNow)
	})
}

// downloadedText is the foot text: "Downloaded just now", "Downloaded 3 min ago" or "Downloaded 2 h ago".
func downloadedText(now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case at.IsZero() || d < time.Minute:
		return "Downloaded just now"
	case d < time.Hour:
		return "Downloaded " + strconv.Itoa(int(d/time.Minute)) + " min ago"
	}
	return "Downloaded " + strconv.Itoa(int(d/time.Hour)) + " h ago"
}

// noteLines turns the Markdown release notes into a short list: the list items, or the plain
// lines when there are none, without headings, at most updateNotesMax.
func noteLines(md string) []string {
	var items, plain []string
	for _, l := range strings.Split(md, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case l == "" || strings.HasPrefix(l, "#"):
		case strings.HasPrefix(l, "- "), strings.HasPrefix(l, "* "):
			items = append(items, strings.TrimSpace(l[2:]))
		default:
			plain = append(plain, l)
		}
	}
	if len(items) == 0 {
		items = plain
	}
	if len(items) > updateNotesMax {
		items = items[:updateNotesMax]
	}
	return items
}

// queueUpdateToast adds the update toast once, when a version became ready.
func (f *Frame) queueUpdateToast(c *ui.Context) {
	v := f.upd.pending
	if v == "" {
		return
	}
	f.upd.pending = ""
	c.AddToast(ui.Toast{ID: "update " + v, Title: "contr " + v + " is ready", Description: UpdateToastBody,
		Timeout: -1, Data: updateToastData{Version: v}})
}

// updateToast draws the update toast under the pill: title, text, Restart now, Later and close.
func (f *Frame) updateToast(c *ui.Context, t ui.Toast) {
	p := theme.Current()
	toast := ui.ToastBase(c, t)
	root := toast.Root.Label(LabelUpdateToast)
	if f.upd.pill.Valid() {
		root = root.AttachTo(f.upd.pill, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(theme.Space8, 0, 0, 0)
	}
	floatCard(root)
	root.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -8}, Exit: &ui.Motion{}})
	root.Padding(theme.Space12, theme.Space12, theme.Space12, theme.Space16).Gap(theme.Space6).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Children(func() {
			ui.Text(c, t.Title).Bold().Grow(1).MinWidth(0).TextColor(p.Ink95)
			toast.CloseButton().Label(LabelToastClose).Size(kit.SizeIconButtonSmall, kit.SizeIconButtonSmall).
				Shrink(0).AlignItems(ui.Center).Justify(ui.Center).Radius(theme.Radius8).TextColor(p.Ink50).
				Cursor(ui.CursorPointer).Children(func() {
				if s := icons.Line("close"); s != nil {
					ui.Icon(c, s).Size(12, 12)
				}
			})
		})
		ui.Text(c, t.Description).TextColor(p.Ink62)
		ui.Row(c).Justify(ui.End).Gap(theme.Space8).Margin(theme.Space4, 0, 0, 0).Children(func() {
			settingsButton(c, UpdateLater, ButtonPlain, func() { c.CloseToast(t.ID) })
			settingsButton(c, SettingsRestart, ButtonFilled, func() {
				c.CloseToast(t.ID)
				f.Settings.RestartNow()
			})
		})
	})
}
