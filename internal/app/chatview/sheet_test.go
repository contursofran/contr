package chatview

// The ChatView part of the spec sheet (D179, D246): a terminal-sized panel in Chat mode with a
// prompt, an answer with a table, open work groups with a background task and a message of another session, notes that need the owner
// and the dock.

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
)

const (
	sheetW = 1000 - 2*apptest.SheetPad
	sheetH = 720
)

// TestSpecSheet draws the parts of package chatview (see apptest.SpecSheet).
func TestSpecSheet(t *testing.T) {
	r := newRig(t)
	r.savePath(term)
	r.selectTerm(term)
	r.ch.SetOn(term, true)
	r.write(lPrompt, lHook, lSaid, lCall1, lNote1, lCall2, lRes1, lNote2, lSaid2, lCall1, lRes1, lTaskOK, lAgent, lNudge, lCall2, lTaskBad)
	// Open the work groups.
	for _, row := range r.ch.model.Rows() {
		if row.Kind == RowGroup {
			open := true
			r.ch.folds[row.ID] = &open
		}
	}
	view := func(c *ui.Context) {
		pal := theme.Current()
		ui.Box(c).Width(sheetW).Height(sheetH).Background(pal.Stage).Border(1, pal.PanelLine).
			Radius(theme.Radius12).Children(func() {
			ui.Row(c).Absolute().Top(theme.Space8).Right(10).Children(func() { r.ch.Switch(c, term) })
			r.ch.View(c, func() {})
		})
	}
	apptest.SpecSheet(t, []apptest.SheetPart{{Name: "ChatView", W: sheetW, H: sheetH, View: view}}, []string{"ChatView"})
}
