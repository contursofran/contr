package settings

// The Setup tab of Settings (D278): a read-only checklist of what contr needs. The checks run off
// the UI thread when Settings opens, when the app becomes active while the tab shows, and on
// Recheck; never while Settings is closed. Rows with a step show a line to copy. The contr plugin
// row also gets Install or Enable (D283, settings_plugin.go); no other row runs a command.

import (
	"context"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/setup"
)

// Texts of the Setup tab.
const (
	TextSetupGood     = "Good to go"
	TextSetupChecking = "Checking…"
	SetupRecheck      = "Recheck"
	SetupCopy         = "Copy"
	SetupCopied       = "Copied"
	// LabelSetupDot names the D146 dot of the Setup tab.
	LabelSetupDot = "setup dot"
)

// setupLimit is the time limit of one pass of all checks.
const setupLimit = 30 * time.Second

// copiedFor is how long a Copy button says Copied.
const copiedFor = 1500 * time.Millisecond

// setupState is the state of the Setup tab.
type setupState struct {
	latest view.Latest
	// results are the checks of the last pass; nil before the first one ends.
	results []setup.Result
	// copied is the id of the row whose Copy button says Copied, until copiedUntil on the
	// frame clock; a zero copiedUntil is set by the next build.
	copied      string
	copiedUntil time.Time
	// write puts text on the clipboard; nil uses the system clipboard. Tests give a fake.
	write func(string)
	// passDone runs on the UI thread once, when the next pass ends (D280).
	passDone func()
}

// SetupSteps is the number of required checks that are not ready, the core included. It is 0
// before the first pass ends.
func (s *Settings) SetupSteps() int {
	if s.su.results == nil {
		return 0
	}
	return setup.Required(s.setupResults())
}

// NeedsDot tells whether the Settings button shows the D146 dot: an update waits, or a required
// check of the last pass is not ready.
func (s *Settings) NeedsDot() bool { return s.Ready() || s.SetupSteps() > 0 }

// setupResults are the rows: the last pass, with the core check from the live connection
// after hc.
func (s *Settings) setupResults() []setup.Result {
	v, ok := "", false
	if s.CoreVersion != nil {
		v, ok = s.CoreVersion()
	}
	core := setup.Core(v, ok)
	if len(s.su.results) == 0 {
		return []setup.Result{core}
	}
	// The core row comes after the hc row.
	rs := append([]setup.Result{s.su.results[0], core}, s.su.results[1:]...)
	return rs
}

// recheck runs all checks off the UI thread. A newer pass drops the one in flight.
func (s *Settings) recheck() {
	if s.Setup == nil {
		return
	}
	env, run := *s.Setup, s.SetupRun
	if run == nil {
		run = setup.Run
	}
	s.su.latest.Reset()
	s.su.latest.Read(s.Env, setupLimit, func(ctx context.Context) func() {
		rs := run(ctx, env)
		return func() {
			s.su.results = rs
			if done := s.su.passDone; done != nil {
				s.su.passDone = nil
				done()
			}
			s.Env.Redraw()
		}
	})
	s.Env.Redraw()
}

// FirstSetup runs the checks once at start, off the UI thread, so the D146 dot shows without
// opening Settings (D280). done runs on the UI thread when the pass ends, with the number of
// required checks that are not ready, the hc core not counted: the core can still be connecting.
// Without Setup it does nothing.
func (s *Settings) FirstSetup(done func(steps int)) {
	if s.Setup == nil {
		return
	}
	s.su.passDone = func() { done(setup.Required(s.su.results)) }
	s.recheck()
}

// OpenSetup opens the modal on the Setup tab, without a new pass.
func (s *Settings) OpenSetup() {
	s.tab = TabSetup
	if !s.open {
		s.open = true
		s.ag = agentsState{}
		s.probe()
	}
	s.Env.Redraw()
}

// copyLine puts a line on the clipboard and makes its button say Copied for a short time.
func (s *Settings) copyLine(id, text string) {
	if s.su.write != nil {
		s.su.write(text)
	} else {
		mygo.Clipboard.WriteText(text)
	}
	s.su.copied, s.su.copiedUntil = id, time.Time{}
	s.Env.Redraw()
}

// setupTab is the Setup tab: a head with the status and Recheck, the Required and Optional rows.
func (s *Settings) setupTab(c *ui.Context) {
	p := theme.Current()
	rs := s.setupResults()
	if s.su.copied != "" {
		if s.su.copiedUntil.IsZero() {
			s.su.copiedUntil = c.Now().Add(copiedFor)
		}
		if left := s.su.copiedUntil.Sub(c.Now()); left > 0 {
			c.After(left)
		} else {
			s.su.copied = ""
		}
	}
	ui.Column(c).Shrink(0).PaddingX(theme.ModalPadX).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 0, 8, 0).Children(func() {
			ui.Text(c, "Setup").Grow(1).TextColor(p.Ink88)
			switch n := setup.Required(rs); {
			case s.su.latest.Busy() || s.su.results == nil && s.Setup != nil:
				ui.Text(c, TextSetupChecking).TextColor(p.Ink48)
			case n == 0:
				ui.Text(c, TextSetupGood).TextColor(p.Ink88)
			default:
				ui.Text(c, stepsLeft(n)).TextColor(p.Ink100)
			}
			var click kit.ClickFn
			if s.Setup != nil && !s.su.latest.Busy() {
				click = s.recheck
			}
			SettingsButton(c, SetupRecheck, kit.ButtonOutlined, click)
		})
		for _, required := range []bool{true, false} {
			head := "Required"
			if !required {
				head = "Optional"
			}
			ui.Text(c, head).Margin(8, 0, 4, 0).TextColor(p.Ink48)
			for _, r := range rs {
				if r.Required == required {
					s.setupRow(c, r)
				}
			}
		}
	})
	ui.Row(c).Shrink(0).Justify(ui.End).Padding(12, theme.ModalPadX).Margin(12, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		SettingsButton(c, SettingsDone, kit.ButtonPlain, s.Close)
	})
}

// stepsLeft is "1 step left" or "N steps left".
func stepsLeft(n int) string {
	if n == 1 {
		return "1 step left"
	}
	return strconv.Itoa(n) + " steps left"
}

// setupRow is one check: the state glyph, the name over the detail, and the line to copy.
func (s *Settings) setupRow(c *ui.Context, r setup.Result) {
	p := theme.Current()
	ui.Column(c).Label("setup "+r.ID).Gap(4).Padding(8, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.Hairline).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			setupGlyph(c, r.State)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, r.Name).TextColor(p.Ink88)
				detail := r.Detail
				if s.su.results == nil && r.ID != setup.IDCore {
					detail = TextSetupChecking
				}
				ui.Text(c, detail).SingleLine().Ellipsis("…").MinWidth(0).TextColor(p.Ink48).
					Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size)
			})
		})
		if r.Copy == "" {
			return
		}
		ui.Row(c).AlignItems(ui.Center).Gap(8).Margin(0, 0, 0, 22).Padding(4, 4, 4, 10).Radius(theme.CardRadius).
			Background(p.Hover).Border(1, p.InputLine).Children(func() {
			ui.Text(c, r.Copy).SingleLine().Ellipsis("…").MinWidth(0).Grow(1).TextColor(p.Ink88).
				Font(theme.StyleValue.Family()).FontSize(theme.StyleValue.Size)
			label := SetupCopy
			if s.su.copied == r.ID {
				label = SetupCopied
			}
			id, text := r.ID, r.Copy
			SettingsButton(c, label, kit.ButtonPlain, func() { s.copyLine(id, text) })
			if a := pluginAction(r); a != "" {
				s.pluginButton(c, a, r.Copy)
			}
		})
		if r.ID == setup.IDPlugin && s.pl.err != "" {
			ui.Text(c, s.pl.err).Margin(0, 0, 0, 22).SingleLine().Ellipsis("…").MinWidth(0).TextColor(p.Blocked)
		}
	})
}

// setupGlyph is the state at the left of a row: a check for ready, a blocked dot for a step, a
// ring for an optional part that is not there.
func setupGlyph(c *ui.Context, st setup.State) {
	p := theme.Current()
	box := ui.Row(c).Size(12, 12).Shrink(0).AlignItems(ui.Center).Justify(ui.Center)
	box.Children(func() {
		switch st {
		case setup.Ready:
			if ic := icons.Line("todo-done"); ic != nil {
				ui.Icon(c, ic).Size(12, 12).TextColor(p.Ink48)
			}
		case setup.Optional:
			ui.Box(c).Size(8, 8).Radius(4).Border(1, p.Ink30)
		default:
			ui.Box(c).Size(8, 8).Radius(4).Background(p.Blocked)
		}
	})
}
