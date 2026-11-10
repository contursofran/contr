//go:build e2e

package main

// The automation plugin lets the e2e test list, click and capture the
// window. It is in e2e builds only.
import (
	"os"
	"strconv"

	_ "github.com/egoist/mygo/plugins/automation"

	"github.com/contursofran/contr/internal/app"
)

// CONTR_E2E_WIDTH opens the window at that width, and lets it be that narrow,
// so a test can capture the icon column of the sidebar (D128). The automation
// plugin cannot resize a window. Release builds do not read it.
//
// CONTR_E2E_HINTS=1 shows the key chips at all times (docs/spec/KeyHints.md): the automation
// plugin cannot hold ⌘ alone.
//
// CONTR_E2E_PICK_FOLDER is the folder that `+ new` gets in place of the system
// dialog, which a test cannot drive (D233).
func init() {
	if d := os.Getenv("CONTR_E2E_PICK_FOLDER"); d != "" {
		folderPicker = func() (string, error) { return d, nil }
	}
	if os.Getenv("CONTR_E2E_HINTS") == "1" {
		app.ForceKeyHints = true
	}
	if w, err := strconv.Atoi(os.Getenv("CONTR_E2E_WIDTH")); err == nil && w > 0 {
		windowWidth, minWidth, fillScreen = w, min(w, minWidth), false
	}
}
