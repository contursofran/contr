package app

import (
	"time"

	gterm "github.com/egoist/mygo/plugins/terminal"

	"github.com/contursofran/contr/internal/prompt"
)

// nextDelay is how long after output the terminal's screen is read for the
// suggested next prompt: the plugin applies the output first, and a burst of
// output gives one read.
const nextDelay = 300 * time.Millisecond

// nextTail is how much of the end of the screen's escape sequences is read:
// the input line is near the bottom.
const nextTail = 256 << 10

// checkNext reads Claude Code's suggested next prompt (D281) from the
// terminal in screen a little after output came, off the UI thread, and
// gives a change to the frame for the composer. current tells, on the UI
// thread, that the terminal is still the one shown. Any goroutine.
func (f *Frame) checkNext(screen interface{ Load() *gterm.Terminal }, current func() bool) {
	if !f.nextCheck.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(nextDelay, func() {
		f.nextCheck.Store(false)
		t := screen.Load()
		if t == nil {
			return
		}
		snap := t.Snapshot()
		next := prompt.Suggestion(snap[max(0, len(snap)-nextTail):])
		f.post(func() {
			if current() && f.next != next {
				f.next = next
				f.invalidate()
			}
		})
	})
}

// nextPrompt is the suggested next prompt of the terminal at addr for the
// composer, "" when that terminal is not the one shown. UI thread.
func (f *Frame) nextPrompt(addr string) string {
	if addr != f.termAddr {
		return ""
	}
	return f.next
}
