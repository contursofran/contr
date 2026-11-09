package state

import "sync"

// Toasts holds the short messages that the window shows as toasts (D126). A
// message can come from any goroutine: Show keeps it and asks the window to
// redraw, and the frame takes the messages in its view with Take and gives
// each to MyGo's toast. MyGo owns the timing of a toast.
type Toasts struct {
	onChange func()

	mu      sync.Mutex
	pending []string
	actions []ToastAction
}

// ToastAction is a message with a button, such as Undo. OnAction runs on the
// UI thread when the owner presses the button.
type ToastAction struct {
	Text, Action string
	OnAction     func()
}

// NewToasts returns an empty queue. onChange may be nil. It runs after each
// Show, without the lock held, so it should only ask the window to redraw
// (Window.Update or Invalidate).
func NewToasts(onChange func()) *Toasts { return &Toasts{onChange: onChange} }

// Show queues a message. An empty text is ignored.
func (t *Toasts) Show(text string) {
	if text == "" {
		return
	}
	t.mu.Lock()
	t.pending = append(t.pending, text)
	t.mu.Unlock()
	if t.onChange != nil {
		t.onChange()
	}
}

// Pending returns the queued messages, oldest first, and leaves them queued.
func (t *Toasts) Pending() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.pending...)
}

// Take returns the queued messages, oldest first, and empties the queue.
func (t *Toasts) Take() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.pending
	t.pending = nil
	return out
}

// ShowAction queues a message with a button that runs undo (D207). An empty
// text is ignored.
func (t *Toasts) ShowAction(text, action string, undo func()) {
	if text == "" {
		return
	}
	t.mu.Lock()
	t.actions = append(t.actions, ToastAction{Text: text, Action: action, OnAction: undo})
	t.mu.Unlock()
	if t.onChange != nil {
		t.onChange()
	}
}

// TakeActions returns the queued messages with a button, oldest first, and
// empties that queue.
func (t *Toasts) TakeActions() []ToastAction {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.actions
	t.actions = nil
	return out
}
