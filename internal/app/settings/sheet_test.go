package settings

// The SettingsSetup part of the spec sheet (D278): the Setup tab with each row state. It has no
// spec image yet: the spec is docs/spec/SettingsSetup.md. The SettingsSetupPlugin part is the
// contr plugin row after a failed Install, with the confirm of Install open (D283).
//
// The SettingsUpdates part of the spec sheet (D179, D277): the Updates tab after an hc update
// that restarted the core, then the tab while an hc update runs, then the confirm of "Update hc"
// in the middle of the sheet.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/setup"
)

const (
	sheetUpdatesW = int(theme.ModalWidth)
	sheetUpdatesH = 3*230 + 2*12
)

func sheetUpdates(c *ui.Context) {
	tabs := []*Settings{
		{Updates: &fakeUpdates{}, Version: "0.4.2", CoreVersion: func() (string, bool) { return "0.1.19", true }, HCUpdate: RunHCUpdate,
			enabledKnown: true, enabled: true, upToDate: true,
			hc: hcState{line: "hc 0.1.19 installed: the core restarted on it, terminals kept running"}},
		{Updates: &fakeUpdates{}, Version: "0.4.2", CoreVersion: func() (string, bool) { return "0.1.18", true }, HCUpdate: RunHCUpdate,
			enabledKnown: true, enabled: true, hc: hcState{busy: true, line: TextHCUpdating}},
		{Updates: &fakeUpdates{}, Version: "0.4.2", CoreVersion: func() (string, bool) { return "0.1.18", true }, HCUpdate: RunHCUpdate,
			enabledKnown: true, enabled: true, upToDate: true, hc: hcState{confirm: true}},
	}
	p := theme.Current()
	ui.Column(c).Gap(12).Children(func() {
		for _, s := range tabs {
			ui.Column(c).Width(theme.ModalWidth).Height(230).Padding(12, 0, 0, 0).Background(p.Ground).
				Border(1, p.FloatLine).Radius(theme.ModalRadius).Children(func() { s.updatesTab(c) })
			s.hcConfirm(c)
		}
	})
}

// TestSpecSheet draws the parts of package settings (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	s := &Settings{Setup: &setup.Env{}, CoreVersion: func() (string, bool) { return "0.1.18", true }}
	s.su.results = []setup.Result{
		{ID: setup.IDHC, Name: "hc", Required: true, Detail: "hc 0.1.18"},
		{ID: setup.IDPlugin, Name: "contr plugin", Required: true, State: setup.Action, Detail: "disabled", Copy: setup.CopyEnable},
		{ID: setup.IDMeter, Name: "Claude meter", Required: true, State: setup.Action, Detail: "last data 3 days ago", Copy: setup.CopyMeter},
		{ID: setup.IDGH, Name: "gh", Required: true, Detail: "signed in"},
		{ID: setup.IDCodex, Name: "codex", State: setup.Optional, Detail: "not installed", Copy: setup.CopyCodex},
		{ID: setup.IDContr, Name: "contr command", Detail: "linked"},
	}
	// The plugin row after a failed Install, with the confirm of the next click open (D283).
	pl := &Settings{Setup: &setup.Env{}, CoreVersion: s.CoreVersion,
		pl: pluginState{err: "marketplace contr not found", confirm: setup.CopyPlugin}}
	pl.su.results = []setup.Result{
		{ID: setup.IDHC, Name: "hc", Required: true, Detail: "hc 0.1.18"},
		{ID: setup.IDPlugin, Name: "contr plugin", Required: true, State: setup.Missing, Detail: "not installed", Copy: setup.CopyPlugin},
	}
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "SettingsSetupPlugin", W: int(theme.ModalWidth), H: 360, View: func(c *ui.Context) { pl.setupTab(c); pl.pluginConfirm(c) }},
		{Name: "SettingsSetup", W: int(theme.ModalWidth), H: 620, View: func(c *ui.Context) { s.setupTab(c) }},
		{Name: "SettingsUpdates", W: sheetUpdatesW, H: sheetUpdatesH, View: sheetUpdates},
	}, []string{"SettingsSetup", "SettingsSetupPlugin", "SettingsUpdates"})
}
