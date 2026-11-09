//go:build e2e

package main

import (
	"context"
	"os"

	"github.com/contursofran/contr/internal/app/settings"
)

// updateService is MyGo's updater. With CONTR_E2E_UPDATE set to a version, e2e builds get a fake
// updater that finds that version and installs it at once, so a test can show the update pill
// (D176) without a release.
func updateService() settings.Updates {
	if v := os.Getenv("CONTR_E2E_UPDATE"); v != "" {
		return &fakeUpdate{version: v}
	}
	return &mygoUpdates{}
}

type fakeUpdate struct{ version string }

func (u *fakeUpdate) Enabled() bool { return true }

func (u *fakeUpdate) Check(context.Context) (*settings.UpdateInfo, error) {
	return &settings.UpdateInfo{Version: u.version, Notes: "## " + u.version + "\n- Restart to update pill in the title bar\n- Faster tab switching\n- Light theme fixes"}, nil
}

func (u *fakeUpdate) Install(context.Context, func(done, total int64)) error { return nil }
