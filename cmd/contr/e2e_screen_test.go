//go:build e2e

package main

import (
	"os/exec"
	"testing"
)

// guardScreen stops the test at once while the screen is locked (#608, D230).
// macOS then shows no window, MyGo holds the frames of the app, and the
// automation tree keeps the first frame: "This Mac, offline" and no workers.
// The test would only fail after its waits, with a wrong cause. When ioreg
// cannot tell, the guard logs it and the test goes on.
func guardScreen(t *testing.T) {
	t.Helper()
	out, err := exec.Command("ioreg", "-n", "Root", "-d1", "-a").Output()
	if err != nil {
		t.Logf("screen guard: ioreg: %v", err)
		return
	}
	locked, err := screenLocked(out)
	if err != nil {
		t.Logf("screen guard: %v", err)
		return
	}
	if locked {
		t.Fatal("the screen is locked: e2e tests and scripts/see.sh need an unlocked screen with the display on, " +
			"because the app draws no frames while its window does not show (D230)")
	}
}
