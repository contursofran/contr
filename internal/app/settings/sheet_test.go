package settings

// The SettingsSetup part of the spec sheet (D278): the Setup tab with each row state. It has no
// spec image yet: the spec is docs/spec/SettingsSetup.md.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/setup"
)

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
	apptest.SpecSheet(t, []apptest.SheetPart{{Name: "SettingsSetup", W: int(theme.ModalWidth), H: 620,
		View: func(c *ui.Context) { s.setupTab(c) }}}, []string{"SettingsSetup"})
}
