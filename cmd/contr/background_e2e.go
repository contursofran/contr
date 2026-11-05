//go:build e2e

package main

import (
	"os"

	"github.com/egoist/mygo"
)

// E2e builds run in the background, so a test run does not take the focus
// from the owner (D148): no Dock icon, and the window shows without
// activating the app. CONTR_E2E_FOREGROUND=1 opens it in front instead, to
// watch a run by eye.
func foreground() bool { return os.Getenv("CONTR_E2E_FOREGROUND") == "1" }

// The policy is set before App.Run, so AppKit starts the app as an accessory
// and never activates it at launch. Switching it while the app runs would
// activate it.
func init() {
	if !foreground() {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	}
}

func backgroundOptions(opts *mygo.WindowOptions) {
	// No glass in e2e builds (D150): over the glass, captures take the color of
	// what is behind the window, and Dark came out washed grey. Without it the
	// frame stands on the theme's ground token, so a capture shows the palette.
	opts.Vibrancy = mygo.VibrancyNone
	if foreground() {
		return
	}
	opts.Hidden = true
}

// The window shows inactive, fully transparent and deaf to the mouse: macOS
// orders even an inactive window over the windows of other apps, and MyGo has
// no way to order it behind them. The page still draws and the automation
// plugin still captures it, clicks and types by element, not by screen
// position.
func backgroundShow(win *mygo.Window) {
	if foreground() {
		return
	}
	win.SetOpacity(0)
	win.SetIgnoreMouseEvents(true)
	win.ShowInactive()
}
