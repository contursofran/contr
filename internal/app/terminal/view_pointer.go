// Adapted from Herdr UI (Apache-2.0); changes: one surface and one pane selection, no scrollback or copy on select; the pointer gesture and its order are adapted; ported to Go.

package terminal

import (
	"log/slog"

	"github.com/egoist/mygo/ui"
)

// Pointer: pane focus click, selection and copy. Nothing here sends input to
// herdr. A selection is client-local, and a copy writes the clipboard only on
// Cmd-C or the Edit menu's Copy (Rust D83).

// onPointer handles a press (after the grid took the focus), a move or a
// release. MyGo sends the move and the release to the element that took the
// press, wherever the pointer is, and sends a release when the window loses
// focus.
func (v *View) onPointer(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		v.focusClickedPane(ev)
		// A Cmd-click is the link gesture (Rust #104): it takes no selection
		// and leaves the one on screen alone.
		if ev.Mods&ui.Super != 0 {
			return true
		}
		v.beginSelection(ev)
		return true
	case ui.InputPointerMove:
		// A move with the left button down follows the pointer, clamped to
		// the pane of the press. Button is -1 when none is held.
		if ev.Button != 0 {
			return false
		}
		x, y, ok := v.gridPosition(ev)
		if !ok || v.selection == nil {
			return false
		}
		if v.selection.Extend(v.surface, v.metrics, x, y) {
			v.invalidate()
		}
		return true
	case ui.InputPointerUp:
		v.endDrag()
		return true
	}
	return false
}

// endDrag ends the drag. Nothing is copied. A press that chose no cell leaves
// no selection; a released selection stays, so Cmd-C can copy it later.
func (v *View) endDrag() {
	if v.surface == nil || v.selection == nil {
		return
	}
	if !v.selection.Release(v.surface) {
		return
	}
	if len(v.selection.Rows(v.surface)) == 0 {
		v.selection = nil
		v.invalidate()
	}
}

// gridPosition is the pointer in pixels from the top-left corner of cell
// (0, 0), as the selection model takes it. ok is false before the first
// layout and with no surface. Pointer coordinates are relative to the grid
// box. Rust needs only the origin here; Go also needs the surface, and every
// caller checks it first.
func (v *View) gridPosition(ev ui.InputEvent) (x, y float32, ok bool) {
	if !v.hasMetrics || v.surface == nil {
		return 0, 0, false
	}
	return ev.X - v.metrics.Pad, ev.Y - v.metrics.Pad, true
}

// beginSelection starts a selection at a press: the half-cell under it, the
// word for a double click, or the row for a triple click. A press that lands
// on no pane clears the one on screen.
func (v *View) beginSelection(ev ui.InputEvent) {
	var next *Selection
	if x, y, ok := v.gridPosition(ev); ok {
		next = BeginSelection(v.surface, v.metrics, x, y, ev.Clicks)
	}
	v.setSelection(next)
}

// setSelection shows next as the selection. Only a change invalidates, so a
// drag repaints on a new cell only.
func (v *View) setSelection(next *Selection) {
	if v.selection.Equal(next) {
		return
	}
	v.selection = next
	v.invalidate()
}

// retireSelection drops a released selection once the program writes over its
// rows, or its pane moves, changes size or goes. A drag is left alone: it
// follows the pointer and copies from the rows on screen.
func (v *View) retireSelection() {
	if v.surface == nil || v.selection == nil || v.selection.Dragging() {
		return
	}
	if len(v.selection.Rows(v.surface)) == 0 || !v.selection.TextUnchanged(v.surface) {
		v.selection = nil
	}
}

// copySelection writes the selected text to the clipboard. It is false when
// nothing is selected, or the selection is stale, so the key falls through to
// the app.
func (v *View) copySelection() bool {
	if v.selection == nil || v.surface == nil {
		return false
	}
	text, err := v.selection.Text(v.surface)
	if err != nil {
		slog.Debug("terminal selection not copied", "err", err)
		return false
	}
	if text == "" {
		return false
	}
	v.services.WriteClipboard(text)
	return true
}

// highlights are the selected cells to paint.
func (v *View) highlights() []Highlight {
	if v.selection == nil || v.surface == nil {
		return nil
	}
	rows := v.selection.Rows(v.surface)
	out := make([]Highlight, len(rows))
	for i, r := range rows {
		out[i] = Highlight{Row: r.Row, Start: r.Start, End: r.End}
	}
	return out
}

// focusClickedPane asks herdr to focus the pane under the press, when that
// pane is not the focused one. A point on a border, outside the panes, or with
// no surface asks for nothing.
func (v *View) focusClickedPane(ev ui.InputEvent) {
	x, y, ok := v.gridPosition(ev)
	if !ok {
		return
	}
	pane := PaneAt(v.surface, v.metrics, x, y)
	if pane == nil || pane.PaneID == v.focusedPane() || v.opts.FocusPane == nil {
		return
	}
	v.opts.FocusPane(pane.PaneID)
}
