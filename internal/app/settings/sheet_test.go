package settings

// The SettingsUpdates part of the spec sheet (D179, D277): the Updates tab after an hc update
// that restarted the core, then the tab while an hc update runs, then the confirm of "Update hc"
// in the middle of the sheet.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
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
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "SettingsUpdates", W: sheetUpdatesW, H: sheetUpdatesH, View: sheetUpdates},
	}, []string{"SettingsUpdates"})
}
