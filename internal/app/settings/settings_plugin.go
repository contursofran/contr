package settings

// The one action of the Setup tab (D283): the contr plugin row gets Install or Enable. A click
// opens a confirm with the exact command; only its Run button runs it, off the UI thread, with
// a time limit. On success a recheck runs; on failure the row shows the last stderr line.

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/setup"
)

// The texts of the plugin action and its confirm.
const (
	SetupInstall          = "Install"
	SetupEnable           = "Enable"
	TextPluginConfirmBody = "contr runs this command:"
	LabelPluginCancel     = "Cancel"
	LabelPluginRun        = "Run"
	// LabelPluginBusy names the spinner while the command runs.
	LabelPluginBusy = "plugin command running"
)

// PluginTimeout is the time limit of the plugin command. Tests make it short.
var PluginTimeout = 2 * time.Minute

// pluginState is the state of the plugin action.
type pluginState struct {
	// confirm is the command the open confirm shows; empty while it is closed.
	confirm string
	busy    bool
	err     string
}

// pluginAction is the button of a row: Install or Enable on the plugin row with a step, else "".
func pluginAction(r setup.Result) string {
	if r.ID != setup.IDPlugin {
		return ""
	}
	switch r.Copy {
	case setup.CopyPlugin:
		return SetupInstall
	case setup.CopyEnable:
		return SetupEnable
	}
	return ""
}

// pluginConfirm builds the confirm while it is open. Cancel and Escape run nothing.
func (s *Settings) pluginConfirm(c *ui.Context) {
	line := s.pl.confirm
	if line == "" {
		return
	}
	open := true
	choice := ui.AlertDialog(c, &open, pluginTitle(line), TextPluginConfirmBody+"\n"+line, LabelPluginCancel, LabelPluginRun)
	switch {
	case choice == 1:
		s.pl.confirm = ""
		s.runPlugin(line)
	case choice == 0 || choice == ui.AlertDismissed || !open:
		s.pl.confirm = ""
		s.Env.Redraw()
	}
}

// pluginTitle is "Install the contr plugin?" or "Enable the contr plugin?".
func pluginTitle(line string) string {
	if line == setup.CopyEnable {
		return "Enable the contr plugin?"
	}
	return "Install the contr plugin?"
}

// runPlugin runs the line off the UI thread; on success it runs a recheck.
func (s *Settings) runPlugin(line string) {
	if s.Setup == nil || s.pl.busy {
		return
	}
	env, run := *s.Setup, s.PluginRun
	if run == nil {
		run = setup.RunPlugin
	}
	s.pl = pluginState{busy: true}
	s.Env.Redraw()
	s.Env.Do(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), PluginTimeout)
		defer cancel()
		err := run(ctx, env, line)
		if ctx.Err() == context.DeadlineExceeded {
			err = errors.New("no answer after " + PluginTimeout.String())
		}
		return func() {
			s.pl = pluginState{}
			if err != nil {
				s.pl.err = setup.LastLine(err.Error())
				s.Env.Redraw()
				return
			}
			s.recheck()
		}
	})
}

// pluginButton is the action at the right of the copy line: a spinner while the command runs.
func (s *Settings) pluginButton(c *ui.Context, label, line string) {
	if s.pl.busy {
		ui.Spinner(c).Label(LabelPluginBusy).Size(14, 14)
		return
	}
	SettingsButton(c, label, kit.ButtonOutlined, func() {
		s.pl.confirm = line
		s.Env.Redraw()
	})
}
