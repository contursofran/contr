package chatview

// The ChatView part of the spec sheet (D179, D246): a terminal-sized panel in Chat mode with a
// prompt, an answer with a table, a folded work group, an open one, a note that needs the owner,
// the live line of a working agent and the composer (D252). ChatViewNeedsYou shows the needs-you
// line with a menu, and ChatViewWide a wide panel with a table wider than the column.

import (
	"strconv"
	"strings"
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
	working := sheetChat(t, "working", lPrompt, lHook, lSaid, lCall1, lNote1, lCall2, lRes1, lNote2, lSaid2, lCall1, lRes1, lCall2)
	blocked := sheetChat(t, "blocked", lPrompt, lSaid2)
	wide := sheetChat(t, "idle", lPrompt, `{"type":"assistant","uuid":"w1","message":{"content":[{"type":"text","text":`+
		strconv.Quote("Prose stays in the column.\n\n"+wideTable)+`}]}}`)
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "ChatView", W: sheetW, H: sheetH, View: working},
		{Name: "ChatViewNeedsYou", W: sheetW, H: 360, View: blocked},
		{Name: "ChatViewWide", W: 1500, H: 420, View: wide},
	}, []string{"ChatView", "ChatViewNeedsYou", "ChatViewWide"})
}

var wideTable = "| one | two | three | four |\n|---|---|---|---|\n| " + strings.Repeat(strings.Repeat("a long cell text ", 6)+" | ", 4)

// sheetChat is the view of a chat whose agent is in state status, with the transcript lines.
func sheetChat(t *testing.T, status string, lines ...string) func(c *ui.Context) {
	s := newSession(t, status)
	s.write(lines...)
	// Open the work groups.
	for _, row := range s.ch.model.Rows() {
		if row.Kind == RowGroup {
			open := true
			s.ch.folds[row.ID] = &open
		}
	}
	return func(c *ui.Context) {
		pal := theme.Current()
		ui.Box(c).Fill().Background(pal.Stage).Border(1, pal.PanelLine).
			Radius(theme.Radius12).Children(func() {
			ui.Row(c).Absolute().Top(theme.Space8).Right(10).Children(func() { s.ch.Switch(c, term) })
			s.ch.View(c, func() {})
		})
	}
}
