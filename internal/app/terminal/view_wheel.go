package terminal

import "github.com/egoist/mygo/ui"

// Wheel and trackpad. M5.7a holds the stub; M5.7d fills it.

// onScroll sends a wheel or trackpad scroll to the pane under the pointer. It
// is false when the scroll is not the terminal's.
func (v *View) onScroll(ev ui.InputEvent) bool { return false }
