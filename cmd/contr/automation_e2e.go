//go:build e2e

package main

// The automation plugin lets the e2e test list, click and capture the
// window. It is in e2e builds only.
import (
	"os"
	"strconv"

	_ "github.com/egoist/mygo/plugins/automation"
)

// CONTR_E2E_WIDTH opens the window at that width, and lets it be that narrow,
// so a test can capture the icon column of the sidebar (D128). The automation
// plugin cannot resize a window. Release builds do not read it.
func init() {
	if w, err := strconv.Atoi(os.Getenv("CONTR_E2E_WIDTH")); err == nil && w > 0 {
		windowWidth, minWidth = w, min(w, minWidth)
	}
}
