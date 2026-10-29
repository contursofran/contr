package app

// The herdr connection as the UI holds it. The reader goroutine in
// internal/herdr sends updates on a channel; a goroutine here applies them
// in order. Nothing here blocks the UI thread. Ported from the Rust contr
// module herdr.

import (
	"context"
	"log/slog"
	"sync"

	"github.com/contursofran/contr/internal/herdr"
)

// updateQueue is how many updates the reader may queue ahead of the
// applying goroutine. When it falls this far behind, the reader waits.
const updateQueue = 64

// readerHandle is the part of [herdr.Reader] that Herdr uses.
type readerHandle interface {
	SetVisible(visible bool)
	Visible() bool
	Stop()
}

// startFunc starts a reader that sends on out.
type startFunc func(ctx context.Context, target herdr.Target, out chan<- herdr.Update) (readerHandle, error)

func startReal(ctx context.Context, target herdr.Target, out chan<- herdr.Update) (readerHandle, error) {
	return herdr.StartReader(ctx, target, out), nil
}

// Herdr is the app's view of the herdr connection. It is safe to use from
// several goroutines. Views read it with [Herdr.Live] on the UI thread.
type Herdr struct {
	onChange func()
	reader   readerHandle
	cancel   context.CancelFunc

	mu      sync.Mutex
	live    herdr.LiveHerdr
	visible bool
	// stale: live changed while the window was hidden, and onChange has not
	// run for it yet.
	stale bool
}

// NewHerdr follows the herdr at target. visible is the window state now.
// onChange runs, on a goroutine of its own, when the state or the tree
// changed and the window is shown; it should only ask the window to redraw
// (Window.Update or Invalidate). While the window is hidden, changes are
// kept and announced once when it is shown again. Wire the window with
// [Herdr.SetVisible], and end the connection with [Herdr.Close].
func NewHerdr(ctx context.Context, target herdr.Target, visible bool, onChange func()) *Herdr {
	return newHerdr(ctx, target, visible, onChange, startReal)
}

func newHerdr(ctx context.Context, target herdr.Target, visible bool, onChange func(), start startFunc) *Herdr {
	ctx, cancel := context.WithCancel(ctx)
	out := make(chan herdr.Update, updateQueue)
	reader, live := readerOrNotRunning(start(ctx, target, out))
	h := &Herdr{onChange: onChange, reader: reader, cancel: cancel, live: live, visible: visible}
	// The reader starts with the window shown and gets this value only after
	// the start. Harmless: a connect always takes a snapshot.
	if reader != nil {
		reader.SetVisible(visible)
	}
	go h.drain(ctx, out)
	return h
}

// readerOrNotRunning returns the reader and the state to start from. When
// the reader did not start, the state is NotRunning, so the UI shows the
// same line as for a herdr that is not running.
func readerOrNotRunning(r readerHandle, err error) (readerHandle, herdr.LiveHerdr) {
	if err != nil {
		slog.Warn("herdr reader did not start", "err", err)
		return nil, herdr.LiveHerdr{State: herdr.ConnState{Kind: herdr.NotRunning, Reason: err.Error()}}
	}
	return r, herdr.NewLiveHerdr()
}

// drain applies the reader's updates in order until ctx ends.
func (h *Herdr) drain(ctx context.Context, in <-chan herdr.Update) {
	for {
		select {
		case u := <-in:
			h.apply(u)
		case <-ctx.Done():
			return
		}
	}
}

// Live returns the latest state and tree. The tree is shared and must not
// be changed.
func (h *Herdr) Live() herdr.LiveHerdr {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.live
}

// SetVisible tells Herdr, and the reader, whether the window is shown. On
// show, it announces the changes kept while the window was hidden.
func (h *Herdr) SetVisible(visible bool) {
	h.mu.Lock()
	h.visible = visible
	notify := visible && h.stale
	if notify {
		h.stale = false
	}
	h.mu.Unlock()
	if h.reader != nil {
		h.reader.SetVisible(visible)
	}
	if notify {
		h.notify()
	}
}

// Close stops the reader and the applying goroutine. It does not wait.
func (h *Herdr) Close() {
	h.cancel()
	if h.reader != nil {
		h.reader.Stop()
	}
}

// apply takes in one update. onChange runs only when live changed, and only
// while the window is shown; a hidden change is announced when it shows.
func (h *Herdr) apply(u herdr.Update) {
	h.mu.Lock()
	before := h.live
	h.live.Apply(u)
	changed := !sameLive(before, h.live)
	notify := changed && h.visible
	if changed && !h.visible {
		h.stale = true
	}
	h.mu.Unlock()
	if notify {
		h.notify()
	}
}

func (h *Herdr) notify() {
	if h.onChange != nil {
		h.onChange()
	}
}

func sameLive(a, b herdr.LiveHerdr) bool {
	if a.State != b.State || (a.Tree == nil) != (b.Tree == nil) {
		return false
	}
	return a.Tree == nil || a.Tree.Equal(*b.Tree)
}
