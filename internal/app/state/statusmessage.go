package state

import (
	"sync"
	"time"
)

// MessageTTL is how long a message stays in the bar before it clears.
const MessageTTL = 6 * time.Second

// StatusMessage is the status bar's message slot: one short line of action
// feedback that clears itself. Each message has one timer; there is no poll
// loop. It is safe to use from several goroutines, because the timer fires
// on its own goroutine. onChange runs without the lock held, so it should
// only ask the window to redraw (Window.Update or Invalidate).
type StatusMessage struct {
	onChange  func()
	afterFunc func(d time.Duration, f func()) stopper

	mu    sync.Mutex
	text  string
	has   bool
	timer stopper
	gen   uint64
}

type stopper interface{ Stop() bool }

// NewStatusMessage returns an empty slot. onChange may be nil. It runs when
// the shown text changes.
func NewStatusMessage(onChange func()) *StatusMessage {
	return &StatusMessage{
		onChange:  onChange,
		afterFunc: func(d time.Duration, f func()) stopper { return time.AfterFunc(d, f) },
	}
}

// Text returns the message, or false when the slot is empty.
func (m *StatusMessage) Text() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.text, m.has
}

// Show replaces the message and restarts the [MessageTTL] countdown.
// onChange runs only when the text changes, so a repeated message repaints
// nothing.
func (m *StatusMessage) Show(text string) {
	m.mu.Lock()
	changed := !m.has || m.text != text
	m.text, m.has = text, true
	// Stopping the old timer and counting a new generation means only the
	// newest countdown clears the message.
	if m.timer != nil {
		m.timer.Stop()
	}
	m.gen++
	gen := m.gen
	m.timer = m.afterFunc(MessageTTL, func() { m.clear(gen) })
	m.mu.Unlock()
	if changed {
		m.notify()
	}
}

func (m *StatusMessage) clear(gen uint64) {
	m.mu.Lock()
	if gen != m.gen || !m.has {
		m.mu.Unlock()
		return
	}
	m.text, m.has, m.timer = "", false, nil
	m.mu.Unlock()
	m.notify()
}

func (m *StatusMessage) notify() {
	if m.onChange != nil {
		m.onChange()
	}
}
