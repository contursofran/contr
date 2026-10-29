package terminal

import "github.com/egoist/mygo/ui"

// Pointer: pane focus click, selection and copy. M5.7a holds the stubs;
// M5.7c fills them.

// onPointer handles a press (after the grid took the focus), a move or a
// release.
func (v *View) onPointer(ev ui.InputEvent) bool { return false }

// copySelection writes the selected text to the clipboard. It is false when
// nothing is selected.
func (v *View) copySelection() bool { return false }

// highlights are the selected cells to paint.
func (v *View) highlights() []Highlight { return nil }

// retireSelection drops a released selection that the surface no longer holds.
func (v *View) retireSelection() {}
