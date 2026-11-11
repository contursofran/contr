//go:build e2e

package main

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/sidebar"
)

// TestSetupFirstLaunch: on the first launch with required steps left, Settings opens on the
// Setup tab once (D280). The next launch shows only the dot on the Settings entry.
func TestSetupFirstLaunch(t *testing.T) {
	h := newHarness(t)
	seeSetupMissing(t, h)
	h.firstLaunch = true
	c := h.launch(t)
	c.waitLabel(t, settings.SetupRecheck)
	c.settle(t)
	h.capture(t, c, "setup-first-launch.png")

	c = h.launch(t)
	c.waitLabel(t, sidebar.LabelSettingsSetup)
	time.Sleep(500 * time.Millisecond)
	if c.hasPrefix(t, settings.SetupRecheck) {
		t.Fatal("Settings opened by itself on the second launch")
	}
}
