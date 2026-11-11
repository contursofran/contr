package projectsview_test

// The ProjectsModal part of the spec sheet (D207, S6): the states that scripts/see.sh cannot
// reach. The states come from export_test.go. The part is in this outside test package because
// its last cell is the hide toast that the frame draws (app.Frame.ToastView).

import (
	"bytes"
	"fmt"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
)

const (
	pmW, pmH = 600, 520 // size of one state, in points
	pmGap    = 16
	pmCaptH  = 24
	pmCols   = 2
	pmRows   = 5 // the toast state is the last cell
)

// pmImage renders one state and returns it as a bitmap for ui.Image. An
// encode or decode error panics: the view runs inside ui.Render, where there
// is no testing.T, and a panic fails the test (a blank cell would hide it).
func pmImage(draw func(c *ui.Context)) *ui.Bitmap {
	img := ui.Render(func(c *ui.Context) {
		theme.Apply(c)
		ui.Column(c).Width(pmW).Height(pmH).Background(theme.Current().Ground).Font(theme.FontFamilySans).
			FontSize(theme.FontSize).Children(func() { draw(c) })
	}, pmW, pmH, 1)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		panic(fmt.Sprintf("projects modal sheet: encode a state: %v", err))
	}
	bm, err := ui.DecodeBitmap(b.Bytes())
	if err != nil {
		panic(fmt.Sprintf("projects modal sheet: decode a state: %v", err))
	}
	return bm
}

// pmToast draws the hide toast of the frame, with no modal.
func pmToast(c *ui.Context) {
	f := &app.Frame{Toasts: state.NewToasts(nil)}
	f.Toasts.ShowAction(projectsview.HiddenToast("api"), projectsview.ProjectsUndo, func() {})
	f.ToastView(c)
}

// projectsModalSheet draws every state under its caption.
func projectsModalSheet(c *ui.Context) {
	type cell struct {
		caption string
		bm      *ui.Bitmap
	}
	var cells []cell
	for _, s := range projectsview.SheetStates {
		m := projectsview.SheetModal()
		s.Set(m)
		cells = append(cells, cell{s.Caption, pmImage(m.View)})
	}
	cells = append(cells, cell{"hide toast with Undo", pmImage(pmToast)})
	ui.Column(c).Gap(pmGap).Children(func() {
		for r := 0; r < len(cells); r += pmCols {
			ui.Row(c).Gap(pmGap).Children(func() {
				for _, k := range cells[r:min(r+pmCols, len(cells))] {
					ui.Column(c).Width(pmW).Children(func() {
						ui.Text(c, k.caption).Height(pmCaptH).TextColor(theme.Current().Ink64)
						if k.bm != nil {
							ui.Image(c, k.bm)
						}
					})
				}
			})
		}
	})
}

// TestSpecSheet draws the ProjectsModal part (see apptest.SpecSheet). It has no spec image yet:
// the spec is docs/spec/ProjectsModal.md.
func TestSpecSheet(t *testing.T) {
	apptest.SpecSheet(t, []apptest.SheetPart{{Name: "ProjectsModal", W: pmCols*pmW + (pmCols-1)*pmGap,
		H: pmRows*(pmH+pmCaptH) + (pmRows-1)*pmGap, View: projectsModalSheet}}, []string{"ProjectsModal"})
}
