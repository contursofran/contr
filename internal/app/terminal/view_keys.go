package terminal

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Keys, paste and OSC 52. M5.7a holds the stubs; M5.7b fills them.

// onKey sends a key press. It is false when the key is not the terminal's.
func (v *View) onKey(ev ui.InputEvent) bool { return false }

// paste sends the clipboard text as one paste.
func (v *View) paste() {}

// send sends events to the focused pane, then sends the size again when herdr
// shows another one (Rust D12).
func (v *View) send(events []endpoint.InputEvent, now time.Time) {}

// reassertSize sends the sent size again when herdr shows another size, at
// most once a second (Rust D12).
func (v *View) reassertSize(now time.Time) {}

// writeClipboard puts a program's OSC 52 text on the system clipboard.
func (v *View) writeClipboard(text string) {}
