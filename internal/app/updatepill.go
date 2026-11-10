package app

// The update pill (docs/spec/UpdatePill.md, D176): when a background install is ready, the title
// bar shows "Restart to update" left of the diff summary. A click opens a panel under it
// (PopoverBase) with the version, the release notes and "Restart now". The first time a version
// is ready, one toast drops under the pill. On the first start of a version that contr installed,
// the same pill says "Updated to X" and its panel opens by itself with What's new (D209). The
// methods run on the UI thread.

import (
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/updated"
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
	// UpdatedPrefix starts the text of the pill after an update: "Updated to 0.4.0" (D209).
	UpdatedPrefix = "Updated to "
	// UpdateGotIt is the button that closes the What's new panel after an update (D209).
	UpdateGotIt = "Got it"
)

// updateWidth is the width of the update panel and toast.
const updateWidth = 340

// updateNotesMax is the most release-note lines the panel lists.
const updateNotesMax = 6

// updateToastData marks the update toast among the toasts, so the viewport draws it under the pill.
type updateToastData struct{ Version string }

// updateUI holds the pill's state.
type updateUI struct {
	// open is true while the panel of a ready update is open. Only the owner opens it (#533).
	open bool
	// afterOpen is true while the What's new panel after an update is open (D209).
	afterOpen bool
	// toasted holds the versions whose toast showed: one toast per version.
	toasted map[string]bool
	// pending is a version whose toast waits for the next build.
	pending string
	// pill is the pill of this build; zero when it does not show.
	pill ui.Element
	// after is the update that the running version came from, while its What's new shows (D209).
	after *updated.Note
	// run saves the note as shown, off the UI thread.
	run view.Runner
}

// loadUpdated reads the note of the last install off the UI thread (D209). When it is of the
// running version and did not show yet, the "Updated to" pill shows with its panel open. A
// missing file shows nothing; a bad one is logged.
func (f *Frame) loadUpdated(env view.Env) {
	f.upd.run = env.Run
	if f.deps.Updated.Load == nil {
		return
	}
	load := f.deps.Updated.Load
	env.Do(func() func() {
		n, err := load()
		if err != nil {
			log.Printf("contr: read the installed update: %v", err)
			return nil
		}
		return func() {
			f.showUpdated(n)
			env.Redraw()
		}
	})
}

// recordInstall is Settings.RecordInstall: it saves the installed update through Deps.Updated.Save,
// not shown yet (D209). It runs off the UI thread.
func (f *Frame) recordInstall(info settings.UpdateInfo, from string, at time.Time) error {
	if f.deps.Updated.Save == nil {
		return nil
	}
	return f.deps.Updated.Save(updated.Note{Version: info.Version, From: from, Notes: info.Notes, InstalledAt: at})
}

// showUpdated opens What's new for the note when it is due for the running version.
func (f *Frame) showUpdated(n updated.Note) {
	if f.Settings == nil || !n.Due(f.Settings.Version) {
		return
	}
	f.upd.after, f.upd.afterOpen = &n, true
}

// updatedShown removes the "Updated to" pill, closes its panel and saves its note as shown, off
// the UI thread. It does nothing when the pill does not show.
func (f *Frame) updatedShown() {
	if f.upd.after == nil {
		return
	}
	n := *f.upd.after
	n.Shown = true
	f.upd.after, f.upd.afterOpen = nil, false
	if f.deps.Updated.Save == nil {
		return
	}
	save := f.deps.Updated.Save
	f.upd.run.Do(func() func() {
		if err := save(n); err != nil {
			log.Printf("contr: mark the installed update shown: %v", err)
		}
		return nil
	})
}

// updateReady is Settings.OnReady: it queues the one toast of the version. The ready pill takes
// the place of the "Updated to" pill: that note is marked shown and its panel closes. The ready
// panel stays closed until the owner opens it (#533).
func (f *Frame) updateReady(info settings.UpdateInfo) {
	f.updatedShown()
	if f.upd.toasted == nil {
		f.upd.toasted = map[string]bool{}
	}
	if f.upd.toasted[info.Version] {
		return
	}
	f.upd.toasted[info.Version] = true
	f.upd.pending = info.Version
}

// updatePill builds the pill and its panel while an update is ready, or the "Updated to" pill
// after an update (D209).
func (f *Frame) updatePill(c *ui.Context) {
	f.upd.pill = ui.Element{}
	if f.Settings == nil {
		return
	}
	info, at, ok := f.Settings.Installed()
	if !ok {
		f.updatedPill(c)
		return
	}
	pill := f.pillButton(c, LabelUpdatePill, f.upd.open, func() { f.upd.open = !f.upd.open })
	ui.PopoverBase(c, pill, &f.upd.open, func(panel ui.Element) {
		panel.Label(LabelUpdatePanel).AttachTo(pill, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(theme.Space8, 0, 0, 0)
		floatCard(kit.PopIn(panel))
		if panel.OverlayShortcut(0, ui.KeyEnter) {
			f.Settings.RestartNow()
		}
		panel.Children(func() { f.updatePanel(c, info, f.Settings.Version, at, false) })
	})
}

// updatedPill is the pill on the first start after an update, with What's new open under it
// (D209). Once the panel closes (Got it, Return, Escape, a press outside or on the pill), the
// pill goes and the note is saved as shown.
func (f *Frame) updatedPill(c *ui.Context) {
	n := f.upd.after
	if n == nil || !f.upd.afterOpen {
		return
	}
	pill := f.pillButton(c, UpdatedPrefix+n.Version, true, f.updatedShown)
	panel := ui.PopoverBase(c, pill, &f.upd.afterOpen, func(panel ui.Element) {
		panel.Label(LabelUpdatePanel).AttachTo(pill, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(theme.Space8, 0, 0, 0)
		floatCard(kit.PopIn(panel))
		if panel.OverlayShortcut(0, ui.KeyEnter) {
			f.updatedShown()
		}
		panel.Children(func() {
			f.updatePanel(c, settings.UpdateInfo{Version: n.Version, Notes: n.Notes}, n.From, n.InstalledAt, true)
		})
	})
	if panel.Dismissed() {
		f.updatedShown()
	}
}

// pillButton is the pill: the download icon and text, on ButtonBase. open gives it the hover
// fill; a click runs click, which opens or closes the panel.
func (f *Frame) pillButton(c *ui.Context, text string, open bool, click func()) ui.Element {
	p := theme.Current()
	pill := ui.ButtonBase(c).Label(text).Height(theme.SizeIconButton).Shrink(0).
		PaddingX(theme.Space10).Gap(theme.Space6).AlignItems(ui.Center).Radius(theme.SizeIconButton/2).
		Border(1, p.FloatLine).Cursor(ui.CursorPointer).MarginEnd(theme.Space8).
		OnClick(click)
	if pill.Hovered() || open {
		pill = pill.Background(p.Hover)
	}
	pill.TextColor(p.Ink100).Children(func() {
		if s := icons.Line("download"); s != nil {
			ui.Icon(c, s).Size(14, 14).Shrink(0)
		}
		ui.Text(c, text).NoWrap()
	})
	f.upd.pill = pill
	return pill
}

// floatCard gives a floating card its look: glass-float, float-line and shadow-float.
func floatCard(el ui.Element) {
	p := theme.Current()
	el.Column().Width(updateWidth).Radius(theme.Radius12).Background(floatCardFill(p)).Border(1, p.FloatLine)
	for _, l := range p.ShadowFloat {
		if !l.Inset {
			el.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
		}
	}
}

// floatCardFill is the fill of the update panel: float-solid, so that the rows behind the
// panel do not show through its text (#524, D212).
func floatCardFill(p *theme.Palette) ui.Color { return p.FloatSolid }

// updatePanel is the panel body: head, What's new, the agents note and the foot. After an update
// (D209) it has no agents note, and its foot has only Got it.
func (f *Frame) updatePanel(c *ui.Context, info settings.UpdateInfo, from string, at time.Time, after bool) {
	p := theme.Current()
	meta := theme.StyleMeta
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space14, theme.Space16, theme.Space10, theme.Space16).Children(func() {
		ui.Text(c, "contr "+info.Version).Bold().Grow(1).TextColor(p.Ink95)
		ui.Text(c, "from "+from).Font(meta.Stack()).FontSize(meta.Size).TextColor(p.Ink50)
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
	if !after {
		ui.Row(c).Gap(theme.Space8).Margin(0, theme.Space16, theme.Space12, theme.Space16).Padding(theme.Space8, theme.Space10).
			Radius(theme.Radius8).Background(p.Hover).TextColor(p.Ink62).Children(func() {
			if s := icons.Line("server"); s != nil {
				ui.Icon(c, s).Size(14, 14).Shrink(0)
			}
			ui.Text(c, UpdateAgentsNote).Grow(1).MinWidth(0)
		})
	}
	ui.Row(c).AlignItems(ui.Center).Gap(theme.Space8).Padding(theme.Space10, theme.Space16).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		if after {
			ui.Text(c, sinceText("Updated", time.Now(), at)).NoWrap().Grow(1).TextColor(p.Ink50)
			settings.SettingsButton(c, UpdateGotIt+" ⏎", kit.ButtonFilled, f.updatedShown)
			return
		}
		ui.Text(c, sinceText("Downloaded", time.Now(), at)).NoWrap().Grow(1).TextColor(p.Ink50)
		settings.SettingsButton(c, UpdateLater, kit.ButtonPlain, func() { f.upd.open = false })
		settings.SettingsButton(c, settings.SettingsRestart+" ⏎", kit.ButtonFilled, f.Settings.RestartNow)
	})
}

// sinceText is the foot text: "<verb> just now", "<verb> 3 min ago" or "<verb> 2 h ago".
func sinceText(verb string, now, at time.Time) string {
	d := now.Sub(at)
	switch {
	case at.IsZero() || d < time.Minute:
		return verb + " just now"
	case d < time.Hour:
		return verb + " " + strconv.Itoa(int(d/time.Minute)) + " min ago"
	}
	return verb + " " + strconv.Itoa(int(d/time.Hour)) + " h ago"
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

// updateToastID is the ID of the update toast. All versions share it, so the toast of a newer
// version takes the place of the toast of the ready one (D226).
const updateToastID = "update"

// queueUpdateToast adds the update toast once, when a version became ready.
func (f *Frame) queueUpdateToast(c *ui.Context) {
	v := f.upd.pending
	if v == "" {
		return
	}
	f.upd.pending = ""
	c.AddToast(ui.Toast{ID: updateToastID, Title: "contr " + v + " is ready", Description: UpdateToastBody,
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
			settings.SettingsButton(c, UpdateLater, kit.ButtonPlain, func() { c.CloseToast(t.ID) })
			settings.SettingsButton(c, settings.SettingsRestart, kit.ButtonFilled, func() {
				c.CloseToast(t.ID)
				f.Settings.RestartNow()
			})
		})
	})
}
