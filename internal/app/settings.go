package app

// The Settings modal (D115): contr's version, "Check for updates", the notes of a newer version,
// "Install and restart", and the errors. The menu item "Settings…" (Cmd-,) opens it. The checks
// and the install run on goroutines (D63); the modal reads their results on the UI thread.
// StartChecks checks at launch and every CheckEvery while the window shows; a newer version
// shows as "update ready · restart" in the status bar. contr never installs without a click.

import (
	"context"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/contursofran/contr/internal/app/theme"
)

// Labels of the Settings modal.
const (
	LabelSettings      = "settings modal"
	LabelSettingsNotes = "update notes"
	SettingsEsc        = "esc"
	SettingsDone       = "Done"
	SettingsCheck      = "Check for updates"
	SettingsInstall    = "Install and restart"
)

// The texts of the update status line.
const (
	TextUpdatesOff = "updates are off: no update key in this build"
	TextChecking   = "checking…"
	TextUpToDate   = "contr is up to date"
	TextInstalling = "installing…"
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
	Run     Runner
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

// Ready tells whether a newer version waits for a click on "update ready · restart".
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
// runs it: the modal's button and the status bar's item.
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
	d := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(8, 8, 8, 0.72)).Justify(ui.Start).AlignItems(ui.Center).Padding(theme.ModalTop, 0, 0, 0)
		panel.Label(LabelSettings).Column().Width(theme.ModalWidth).Background(theme.Color(theme.BgOverlay)).
			Border(1, theme.Color(theme.OverlayBorder)).Radius(theme.ModalRadius).
			Shadow(0, 24, 80, 0, ui.RGBA(0, 0, 0, 0.6))
		panel.Children(func() { s.body(c) })
	})
	if d.Dismissed() || !open {
		s.open = false
		s.redraw()
	}
}

func (s *Settings) body(c *ui.Context) {
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(8).Padding(16, theme.ModalPadX, 12, theme.ModalPadX).Children(func() {
		ui.Text(c, "Settings").Bold().Grow(1).TextColor(theme.Color(theme.Text))
		cardButton(c, SettingsEsc, ButtonPlain, theme.TextDim, s.Close)
	})
	busy := s.checking || s.installing
	live := s.Updates != nil && s.enabled && !busy
	ui.Column(c).Shrink(0).Gap(8).PaddingX(theme.ModalPadX).Children(func() {
		ui.Text(c, "Updates").TextColor(theme.Color(theme.TextDim))
		ui.Text(c, "contr "+s.Version).TextColor(theme.Color(theme.Text))
		if st := s.status(); st != "" {
			ui.Text(c, st).TextColor(theme.Color(theme.Text2))
		}
		if s.err != "" {
			ui.Text(c, s.err).TextColor(theme.Color(theme.Failed))
		}
		if s.found != nil && s.found.Notes != "" {
			ui.Scroll(c).Label(LabelSettingsNotes).FillWidth().Height(220).Shrink(0).Padding(8, 10).
				Background(theme.Color(theme.BgWindow)).Radius(8).Children(func() {
				markdown.View(c, s.found.Notes, &s.md)
			})
		}
	})
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Justify(ui.End).Gap(14).Padding(12, theme.ModalPadX).Margin(12, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(theme.Color(theme.Line)).Children(func() {
		var check ClickFn
		if live {
			check = s.Check
		}
		cardButton(c, SettingsCheck, ButtonOutlined, theme.Text, check)
		if s.found != nil {
			var install ClickFn
			if live {
				install = s.Install
			}
			cardButton(c, SettingsInstall, ButtonFilled, theme.Text, install)
		}
		cardButton(c, SettingsDone, ButtonPlain, theme.Text, s.Close)
	})
}
