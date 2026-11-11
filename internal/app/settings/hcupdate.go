package settings

// The herdr-core block of the Updates tab (D277): the hc version and an "Update hc" button that
// runs `hc update --json` off the UI thread. hc has no check flag, so contr does not know of a
// newer hc before the update. `hc update` puts the new hc in place and restarts the core by
// itself; the terminals keep running. contr never runs it without a click.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc"
)

// SettingsHCUpdate is the label of the button that updates hc.
const SettingsHCUpdate = "Update hc"

// HCUpdateTimeout is the time limit of `hc update`: a download and a core restart. Tests
// make it short.
var HCUpdateTimeout = 5 * time.Minute

// The texts of the hc update line.
const (
	TextHCUpdating = "updating hc…"
	TextHCUpToDate = "hc is up to date"
)

// HCRunner runs `hc update --json` and returns what it wrote on stdout. Tests give a fake.
type HCRunner func(ctx context.Context) ([]byte, error)

// hcResult is the JSON that `hc update --json` writes.
type hcResult struct {
	Was       string `json:"was"`
	Now       string `json:"now"`
	Restarted bool   `json:"restarted"`
	Updated   *bool  `json:"updated"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// hcState is the state of the hc block.
type hcState struct {
	busy bool
	line string
	err  string
}

// RunHCUpdate runs `hc update --json` with the hc that contr connects to (CONTR_HC, else
// hc.Find).
func RunHCUpdate(ctx context.Context) ([]byte, error) {
	home, _ := os.UserHomeDir()
	path, err := hc.Find(os.Getenv("CONTR_HC"), home, nil)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, "update", "--json")
	cmd.Env = hc.CleanEnv(cmd.Environ())
	return cmd.Output()
}

// UpdateHC runs HCUpdate off the UI thread. Only a click on "Update hc" calls it.
func (s *Settings) UpdateHC() {
	if s.HCUpdate == nil || s.hc.busy {
		return
	}
	s.hc = hcState{busy: true, line: TextHCUpdating}
	s.Env.Redraw()
	run := s.HCUpdate
	s.Env.Do(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), HCUpdateTimeout)
		defer cancel()
		out, err := run(ctx)
		if ctx.Err() == context.DeadlineExceeded {
			err = errors.New("no answer after " + HCUpdateTimeout.String())
		}
		line, msg := hcOutcome(out, err)
		return func() {
			s.hc = hcState{line: line}
			if msg != "" {
				s.hc.err = "hc update failed: " + msg
				s.Env.Show(s.hc.err)
			}
			s.Env.Redraw()
		}
	})
}

// hcOutcome reads the output of `hc update --json`: the status line, or an error message.
func hcOutcome(out []byte, err error) (line, msg string) {
	var r hcResult
	jerr := json.Unmarshal(out, &r)
	switch {
	case jerr == nil && r.Error != nil:
		return "", r.Error.Message
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(strings.TrimSpace(string(ee.Stderr))) > 0 {
			return "", strings.TrimSpace(string(ee.Stderr))
		}
		return "", err.Error()
	case jerr != nil:
		return "", "unexpected output: " + strings.TrimSpace(string(out))
	case r.Updated != nil && !*r.Updated:
		return TextHCUpToDate, ""
	case r.Was == r.Now:
		return r.Now + " is the newest: the core restarted on it", ""
	case r.Restarted:
		return r.Now + " installed: the core restarted on it, terminals kept running", ""
	}
	return r.Now + " installed: restart the core to use it", ""
}

// hcRow is the hc line of Versions, with "Update hc" on its right, then its status line.
func (s *Settings) hcRow(c *ui.Context) {
	p := theme.Current()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Gap(14).Children(func() {
		ui.Text(c, s.coreText()).Grow(1).TextColor(p.Ink88)
		if s.HCUpdate != nil {
			var click kit.ClickFn
			if !s.hc.busy {
				click = s.UpdateHC
			}
			SettingsButton(c, SettingsHCUpdate, kit.ButtonOutlined, click)
		}
	})
	if s.hc.line != "" {
		ui.Text(c, s.hc.line).TextColor(p.Ink64)
	}
	if s.hc.err != "" {
		ui.Text(c, s.hc.err).TextColor(p.Blocked)
	}
}
