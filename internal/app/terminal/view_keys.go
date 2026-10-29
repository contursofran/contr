package terminal

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// Keys, paste and OSC 52.

// onKey sends a key press to the focused pane. It is false when the key is
// the app's (Cmd chords). A key that makes no herdr event is still taken: its
// text arrives through the IME client, and MyGo's own focus and activation
// handling must not see Space or Tab.
func (v *View) onKey(ev ui.InputEvent) bool {
	if ev.Mods&ui.Super != 0 {
		return false
	}
	if key, ok := KeyEvent(ev.Key, ev.Mods); ok {
		v.send([]endpoint.InputEvent{key}, ev.Time)
	}
	return true
}

// paste sends the clipboard text as one paste. An empty clipboard sends
// nothing. Cmd-V and the Edit menu's Paste both come here.
func (v *View) paste() {
	text := v.services.ReadClipboard()
	if text == "" {
		return
	}
	v.send([]endpoint.InputEvent{endpoint.PasteEvent{Text: text}}, v.now)
}

// commit sends text from the IME as text. Empty text sends nothing.
func (v *View) commit(text string) {
	v.ensureLink()
	if text != "" {
		v.send([]endpoint.InputEvent{endpoint.TextEvent{Text: text}}, v.now)
	}
}

// send sends events to the focused pane, then sends the size again when herdr
// shows another one (Rust D12). Nothing is sent without a pane or a link.
func (v *View) send(events []endpoint.InputEvent, now time.Time) {
	pane := v.focusedPane()
	if pane == "" {
		slog.Debug("terminal input not sent: no focused pane")
		return
	}
	// D12: the size is sent again only when the input was queued.
	if v.sendTo(pane, events) {
		v.reassertSize(now)
	}
}

// reassertSize sends the sent size again when herdr shows another size, at
// most once a second (Rust D12).
func (v *View) reassertSize(now time.Time) {
	if !v.hasSent || v.surface == nil || v.surface.Size == v.sent.size {
		return
	}
	if reassertDue(v.reassertedAt, now) {
		v.reassertedAt = now
		v.sendResize(v.sent)
	}
}

// writeClipboard puts a program's OSC 52 text on the system clipboard. Each
// update is one write. Before the first Build there are no services, so the
// text waits for it.
func (v *View) writeClipboard(text string) {
	if !v.hasServices {
		slog.Debug("terminal clipboard write waits for the first frame")
		v.pendingClip, v.hasPending = text, true
		return
	}
	v.services.WriteClipboard(text)
}
