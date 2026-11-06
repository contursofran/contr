package app

// The Settings modal (D115, D126): the contr and hc versions, "Check for updates", the notes of a
// newer version, "Install and restart", and the errors. The menu item "Settings…" (Cmd-,) and the
// Settings button of the sidebar open it. The checks and the install run on goroutines (D63); the
// modal reads their results on the UI thread. StartChecks checks at launch and every CheckEvery
// while the window shows; a newer version puts a dot on the Settings button (see
// [SpacesList.UpdateReady]). contr never installs without a click.

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/contursofran/contr"
	"github.com/contursofran/contr/internal/app/theme"
)

// Version is contr's version, shown in Settings. It comes from the VERSION file (D107).
var Version = contr.Version()

// Labels of the Settings modal.
const (
	LabelSettings      = "settings modal"
	LabelSettingsNotes = "update notes"
	SettingsEsc        = "esc"
	SettingsDone       = "Done"
	SettingsCheck      = "Check for updates"
	SettingsInstall    = "Install and restart"
	// LabelAppearance names the Appearance row (D119); its segments are named by AppearanceNames.
	LabelAppearance = "Appearance"
	// LabelBackground, LabelPanels and TintReset name the transparency sliders and their reset button (D162).
	LabelBackground = "Background"
	LabelPanels     = "Panels"
	TintReset       = "Reset"
)

// AppearanceSources are the choices of the Appearance row, in order (D119).
var AppearanceSources = [...]mygo.ThemeSource{mygo.ThemeSystem, mygo.ThemeDark, mygo.ThemeLight}

// AppearanceNames are the texts of the segments, in the order of AppearanceSources.
var AppearanceNames = [...]string{"System", "Dark", "Light"}

// The texts of the update status line.
const (
	TextUpdatesOff = "updates are off: no update key in this build"
	TextChecking   = "checking…"
	TextUpToDate   = "contr is up to date"
	TextInstalling = "installing…"
	TextCoreOff    = "hc not connected"
)

// CheckEvery is the time between the automatic update checks (D115).
const CheckEvery = 4 * time.Hour

// UpdateInfo is a newer version of contr.
type UpdateInfo struct {
	Version string
	// Notes are the release notes in Markdown.
	Notes string
}

// Updates is the update service. cmd/contr gives it mygo.Updater; tests give it a fake.
type Updates interface {
	// Enabled tells whether this build can update itself. It may touch the disk.
	Enabled() bool
	// Check returns the newer version, or nil when contr is up to date.
	Check(ctx context.Context) (*UpdateInfo, error)
	// Install installs the version that Check found and reports the bytes it got.
	Install(ctx context.Context, progress func(done, total int64)) error
}

// Settings is the modal. Its methods run on the UI thread, but for StartChecks.
type Settings struct {
	Updates Updates
	// Version is contr's version.
	Version string
	// CoreVersion gives the hc core's version, and whether contr follows the core. Nil shows
	// that contr is not connected.
	CoreVersion func() (string, bool)
	Run         Runner
	// Relaunch restarts contr after an install. It runs on the UI thread.
	Relaunch func()
	// Invalidate redraws the window; may be nil.
	Invalidate func()
	// Visible tells whether the window shows; nil means it does. Hidden windows get no checks.
	Visible func() bool
	// Every is the time between automatic checks; zero means CheckEvery.
	Every time.Duration
	// Now is the clock; nil means time.Now. Tests give it a fake.
	Now func() time.Time
	// Status shows a short message; may be nil.
	Status func(string)
	// Appearance returns the theme source in use; nil means System. SetAppearance applies and
	// stores a new one when the owner picks a segment; nil makes the row do nothing (D119).
	Appearance    func() mygo.ThemeSource
	SetAppearance func(mygo.ThemeSource)
	// Tint returns the window and panel alpha in percent for the current theme; SetTint applies
	// new ones and ResetTint puts the token defaults back (D162). Nil makes the rows do nothing.
	Tint      func() (window, panel float64)
	SetTint   func(window, panel float64)
	ResetTint func()

	// tintWin and tintPan are the values the sliders edit; the Seen ones are what the last build showed.
	tintWin, tintPan, tintSeenWin, tintSeenPan float64

	open bool
	// enabled is known once the first Enabled call returns.
	enabledKnown, enabled bool
	checking, installing  bool
	// lastCheck is when the last check started; zero when none did.
	lastCheck time.Time
	upToDate  bool
	found     *UpdateInfo
	err       string
	// percent is the install progress, 0 to 100, set from the install goroutine.
	percent atomic.Int32
	md      markdown.Options
}

// Toggle opens the modal, or closes it when it is open. The menu item and its shortcut call it.
func (s *Settings) Toggle() {
	if s.open {
		s.Close()
		return
	}
	s.open = true
	s.probe()
	s.redraw()
}

// Close hides the modal.
func (s *Settings) Close() {
	s.open = false
	s.redraw()
}

// Opened tells whether the modal shows.
func (s *Settings) Opened() bool { return s.open }

// Ready tells whether a newer version waits for the owner to install it. The Settings button
// shows a dot while it does.
func (s *Settings) Ready() bool { return s.found != nil }

func (s *Settings) redraw() {
	if s.Invalidate != nil {
		s.Invalidate()
	}
}

// probe asks the service once whether this build can update. Enabled touches the disk, so it
// runs off the UI thread.
func (s *Settings) probe() {
	if s.enabledKnown || s.Updates == nil {
		return
	}
	s.Run.Do(func() func() {
		on := s.Updates.Enabled()
		return func() {
			s.enabled, s.enabledKnown = on, true
			s.redraw()
		}
	})
}

// StartChecks is the one place that holds the check schedule (owner answer D33): one check at
// launch, then one every Every (4 hours). A check does not run while the window is hidden; when the
// window shows after a missed check, SetVisible runs it. A check that fails stays quiet. The timer sleeps between checks, so an idle contr uses no CPU.
// The checks run off the UI thread. It returns the function that stops the schedule.
func (s *Settings) StartChecks() (stop func()) {
	if s.Updates == nil {
		return func() {}
	}
	every := s.interval()
	done := make(chan struct{})
	post := func(fn func()) {
		if s.Run.Post == nil {
			fn()
			return
		}
		s.Run.Post(fn)
	}
	go func() {
		t := time.NewTimer(0)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				post(s.autoCheck)
				t.Reset(every)
			}
		}
	}()
	var once atomic.Bool
	return func() {
		if once.CompareAndSwap(false, true) {
			close(done)
		}
	}
}

func (s *Settings) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Settings) interval() time.Duration {
	if s.Every > 0 {
		return s.Every
	}
	return CheckEvery
}

// SetVisible tells Settings that the window shows or hides. It runs on the UI thread. When the
// window shows and the last check is older than the interval (or none ran), it checks at once, so
// a check that fell while the window was hidden is not lost. The schedule goes on as before.
func (s *Settings) SetVisible(visible bool) {
	if !visible || s.Updates == nil {
		return
	}
	if !s.lastCheck.IsZero() && s.now().Sub(s.lastCheck) < s.interval() {
		return
	}
	s.autoCheck()
}

// autoCheck is one scheduled check. It runs on the UI thread, where the window state is known.
func (s *Settings) autoCheck() {
	if s.Visible != nil && !s.Visible() {
		return
	}
	if s.Updates == nil || s.checking || s.installing || s.found != nil {
		return
	}
	s.checking = true
	s.lastCheck = s.now()
	s.Run.Do(func() func() {
		var info *UpdateInfo
		var err error
		on := s.Updates.Enabled()
		if on {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			info, err = s.Updates.Check(ctx)
		}
		return func() {
			s.enabled, s.enabledKnown = on, true
			s.checking = false
			if err != nil {
				log.Printf("contr: update check failed: %v", err)
			} else if info != nil {
				s.found = info
			}
			s.redraw()
		}
	})
}

// Check looks for a newer version. The "Check for updates" button runs it.
func (s *Settings) Check() {
	if s.Updates == nil || !s.enabled || s.checking || s.installing {
		return
	}
	s.checking, s.upToDate, s.found, s.err = true, false, nil, ""
	s.lastCheck = s.now()
	s.redraw()
	s.Run.Do(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := s.Updates.Check(ctx)
		return func() {
			s.checking = false
			switch {
			case err != nil:
				s.err = "update check failed: " + err.Error()
			case info == nil:
				s.upToDate = true
			default:
				s.found = info
			}
			s.redraw()
		}
	})
}

// Install installs the version that the last check found, then restarts contr. Only a click
// on the modal's button runs it.
func (s *Settings) Install() {
	if s.Updates == nil || s.found == nil || s.installing || s.checking {
		return
	}
	s.installing, s.err = true, ""
	s.percent.Store(0)
	s.redraw()
	s.Run.Do(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := s.Updates.Install(ctx, func(done, total int64) {
			if total > 0 {
				s.percent.Store(int32(done * 100 / total))
				s.redraw()
			}
		})
		return func() {
			s.installing = false
			if err != nil {
				s.err = "install failed: " + err.Error()
				if s.Status != nil {
					s.Status(s.err)
				}
				s.redraw()
				return
			}
			s.found = nil
			s.upToDate = true
			s.redraw()
			if s.Relaunch != nil {
				s.Relaunch()
			}
		}
	})
}

// coreText is the hc line: "hc 0.9.3", or "hc not connected" while contr does not follow a core.
func (s *Settings) coreText() string {
	if s.CoreVersion != nil {
		if v, ok := s.CoreVersion(); ok {
			return "hc " + v
		}
	}
	return TextCoreOff
}

// status is the line under the version.
func (s *Settings) status() string {
	switch {
	case s.Updates == nil || (s.enabledKnown && !s.enabled):
		return TextUpdatesOff
	case s.installing:
		return TextInstalling + " " + strconv.Itoa(int(s.percent.Load())) + "%"
	case s.checking:
		return TextChecking
	case s.found != nil:
		return "contr " + s.found.Version + " is available"
	case s.upToDate:
		return TextUpToDate
	}
	return ""
}

// View builds the modal, or nothing while it is closed.
func (s *Settings) View(c *ui.Context) {
	if !s.open {
		return
	}
	open := true
	p := theme.Current()
	d := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		// The veil is the ground of the theme, so Light stays light behind the card.
		backdrop.Background(p.Ground.Alpha(0.72)).Justify(ui.Start).AlignItems(ui.Center).Padding(theme.ModalTop, 0, 0, 0)
		panel.Label(LabelSettings).Column().Width(theme.ModalWidth).Background(p.Ground).
			Border(1, p.FloatLine).Radius(theme.ModalRadius)
		for _, l := range p.ShadowFloat {
			if !l.Inset {
				panel.Shadow(l.X, l.Y, l.Blur, l.Wide, l.Color)
			}
		}
		panel.Children(func() { s.body(c) })
	})
	if d.Dismissed() || !open {
		s.open = false
		s.redraw()
	}
}

func (s *Settings) body(c *ui.Context) {
	p := theme.Current()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(16, theme.ModalPadX, 12, theme.ModalPadX).Children(func() {
		ui.Text(c, "Settings").Bold().Grow(1).TextColor(p.Ink95)
		settingsButton(c, SettingsEsc, ButtonPlain, s.Close)
	})
	s.appearanceRow(c)
	s.tintRows(c)
	busy := s.checking || s.installing
	live := s.Updates != nil && s.enabled && !busy
	ui.Column(c).Shrink(0).Gap(8).PaddingX(theme.ModalPadX).Children(func() {
		ui.Text(c, "Versions").TextColor(p.Ink50)
		ui.Text(c, "contr "+s.Version).TextColor(p.Ink95)
		ui.Text(c, s.coreText()).TextColor(p.Ink95)
		ui.Text(c, "Updates").Margin(4, 0, 0, 0).TextColor(p.Ink50)
		if st := s.status(); st != "" {
			ui.Text(c, st).TextColor(p.Ink62)
		}
		if s.err != "" {
			ui.Text(c, s.err).TextColor(p.Blocked)
		}
		if s.found != nil && s.found.Notes != "" {
			ui.Scroll(c).Label(LabelSettingsNotes).FillWidth().Height(220).Shrink(0).Padding(8, 10).
				Background(p.Hover).Radius(8).Children(func() {
				markdown.View(c, s.found.Notes, &s.md)
			})
		}
	})
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.End).Gap(14).Padding(12, theme.ModalPadX).Margin(12, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		var check ClickFn
		if live {
			check = s.Check
		}
		settingsButton(c, SettingsCheck, ButtonOutlined, check)
		if s.found != nil {
			var install ClickFn
			if live {
				install = s.Install
			}
			settingsButton(c, SettingsInstall, ButtonFilled, install)
		}
		settingsButton(c, SettingsDone, ButtonPlain, s.Close)
	})
}

// appearanceRow is the Appearance row: System, Dark or Light on SegmentedBase (D119). A pick
// calls SetAppearance, which applies the theme at once. The row reads the current palette on
// each build, like the rest of this modal (D155).
func (s *Settings) appearanceRow(c *ui.Context) {
	p := theme.Current()
	cur := mygo.ThemeSystem
	if s.Appearance != nil {
		cur = s.Appearance()
	}
	sel := slices.Index(AppearanceSources[:], cur)
	if sel < 0 {
		sel = 0
	}
	picked := sel
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(0, theme.ModalPadX, 12, theme.ModalPadX).Children(func() {
		ui.Text(c, LabelAppearance).Grow(1).TextColor(p.Ink50)
		seg := ui.SegmentedBase(c.Key("appearance"), &picked, len(AppearanceNames))
		seg.Track.Label(LabelAppearance).Padding(2).Radius(theme.CardRadius).Background(p.Hover).Children(func() {
			for i, name := range AppearanceNames {
				e := seg.Segment(i).Label(name).Padding(4, 12).Radius(theme.CardRadius)
				text := p.Ink62
				if i == picked {
					e.Background(p.Selected)
					text = p.Ink100
				}
				e.Children(func() { ui.Text(c, name).TextColor(text) })
			}
		})
	})
	if picked != sel && s.SetAppearance != nil {
		s.SetAppearance(AppearanceSources[picked])
	}
}

// settingsButton is a button of the Settings dialog, drawn from the palette of the current theme
// (D155). A nil click is a dim button that does nothing. Filled is ink-100 with text on the ground:
// the owner acts. Plain has no border and underlines on hover. Outlined has the input line.
func settingsButton(c *ui.Context, label string, style ButtonStyle, click ClickFn) {
	p := theme.Current()
	live := click != nil
	el := ui.ButtonBase(c).Label(label).Height(theme.AnswerButtonHeight).Shrink(0).
		PaddingX(theme.AnswerButtonPadX).AlignItems(ui.Center).Radius(theme.AnswerButtonRadius)
	text := p.Ink42
	switch {
	case style == ButtonFilled && live:
		el = el.Background(p.Ink100)
		text = p.Ground
	case style == ButtonPlain:
		if live {
			text = p.Ink62
		}
	default:
		el = el.Border(1, p.InputLine)
		if live {
			text = p.Ink95
		}
	}
	if live {
		el = el.Cursor(ui.CursorPointer).OnClick(click)
	}
	el = el.TextColor(text)
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

// tintRows are the Background and Panels sliders, with a Reset button (D162). Each slider sets
// the alpha, in percent, of glass-window or of panel for the current theme and applies it at
// once; the value shows beside it.
func (s *Settings) tintRows(c *ui.Context) {
	if s.Tint == nil {
		return
	}
	// The sliders write into fields, which keep their address between builds. A value that
	// differs from the one seen at the last build was moved by the owner.
	if (s.tintWin != s.tintSeenWin || s.tintPan != s.tintSeenPan) && s.SetTint != nil {
		s.SetTint(s.tintWin, s.tintPan)
	}
	s.tintWin, s.tintPan = s.Tint()
	s.tintSeenWin, s.tintSeenPan = s.tintWin, s.tintPan
	ui.Column(c).Shrink(0).Gap(8).Padding(0, theme.ModalPadX, 12, theme.ModalPadX).Children(func() {
		tintSlider(c, LabelBackground, &s.tintWin)
		tintSlider(c, LabelPanels, &s.tintPan)
		ui.Row(c).AlignItems(ui.Center).Justify(ui.End).Children(func() {
			var reset ClickFn
			if s.ResetTint != nil {
				reset = s.ResetTint
			}
			settingsButton(c, TintReset, ButtonPlain, reset)
		})
	})
}

// tintSlider is one row: the label, a slider from 0 to 100 on SliderBase drawn from the
// palette, and its value in percent.
func tintSlider(c *ui.Context, label string, v *float64) {
	p := theme.Current()
	const trackH, thumb = 4, 14
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Text(c, label).Width(90).TextColor(p.Ink50)
		sl := ui.SliderBase(c.Key("tint-"+label), v, 0, 100).Step(1).Label(label + " alpha").Grow(1).Height(24).PaddingX(thumb / 2)
		at := float32(*v / 100)
		if c.Direction() == ui.RightToLeft {
			at = 1 - at
		}
		sl.Draw(func(pt *ui.Painter, r ui.Rect) {
			x0, w := r.X+thumb/2, r.W-thumb
			y := r.Y + (r.H-trackH)/2
			pt.Fill(ui.Rect{X: x0, Y: y, W: w, H: trackH}, p.MeterTrack, 2)
			pt.Fill(ui.Rect{X: x0, Y: y, W: w * at, H: trackH}, p.MeterFill, 2)
			pt.Fill(ui.Rect{X: x0 + w*at - thumb/2, Y: r.Y + (r.H-thumb)/2, W: thumb, H: thumb}, p.Ink85, thumb/2)
		})
		ui.Text(c, fmt.Sprintf("%.0f%%", *v)).Width(40).TextAlign(ui.End).
			Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size).TextColor(p.Ink85)
	})
}
