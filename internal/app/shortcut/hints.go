package shortcut

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// HintDelay is how long ⌘ must be held alone before the key hints show (docs/spec/KeyHints.md).
const HintDelay = 400 * time.Millisecond

// Hints tells when the key chips show: while ⌘ is held alone for HintDelay or longer, with no
// other key since it went down, in the active window. The zero value is ready. UI thread.
type Hints struct {
	since   time.Time
	armed   bool
	spoiled bool
}

// Step takes the state of one frame: the modifiers held, whether another key is held, whether
// the window is active, and the frame's time. It returns whether the chips show, and how long
// until they show while ⌘ waits (0 when nothing waits): the caller asks for one frame then
// (c.After), so no frame comes while nothing changes (D55).
func (h *Hints) Step(mods ui.Modifiers, otherKey, active bool, now time.Time) (show bool, wait time.Duration) {
	if mods != ui.Cmd || !active {
		*h = Hints{}
		return false, 0
	}
	if otherKey {
		h.spoiled = true
	}
	if h.spoiled {
		return false, 0
	}
	if !h.armed {
		h.armed, h.since = true, now
	}
	if el := now.Sub(h.since); el < HintDelay {
		return false, HintDelay - el
	}
	return true, 0
}

// Used hides the chips until ⌘ is released: a shortcut ran. A menu takes its key before the
// window sees it, so the menu handlers call this.
func (h *Hints) Used() { h.spoiled = true }

// Frame runs Step for the frame of c and asks for the frame that shows the chips. It returns
// whether the chips show.
func (h *Hints) Frame(c *ui.Context) bool {
	show, wait := h.Step(c.Modifiers(), len(c.KeysDown()) > 0, c.WindowActive(), c.Now())
	if wait > 0 {
		c.After(wait)
	}
	return show
}
