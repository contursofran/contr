package herdr

// The reader: keeps one connection to herdr's API socket, follows its events
// and sends the connection state and the tree to the UI. It only reads; it
// never writes to herdr.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// NotRunningLine is shown in place of the terminal while herdr is not running.
const NotRunningLine = "herdr is not running · start it with `herdr`"

const (
	// callTimeout is the longest wait for one reply: ping, snapshot and the
	// subscription ack.
	callTimeout = 2 * time.Second
	// coalesce: after the first event not yet in a snapshot, wait this long
	// for the burst to end, then snapshot.
	coalesce = 50 * time.Millisecond
	// snapshotGap is the least time between two snapshots while events keep
	// arriving: a busy stream gets at most four snapshots per second.
	snapshotGap = 250 * time.Millisecond
	// refreshEvery is the snapshot interval. herdr sends no event for a pane
	// label or cwd change, so without this those fields would go stale.
	refreshEvery = 10 * time.Second
	baseRetry    = 500 * time.Millisecond
	maxRetry     = 5 * time.Second
	// incompatibleRetry is the wait before a retry after herdr replied in a
	// form contr does not read.
	incompatibleRetry = 5 * time.Second
)

// ConnKind is the kind of a [ConnState].
type ConnKind uint8

// The kinds of connection state.
const (
	Connecting ConnKind = iota
	Connected
	NotRunning
	// Incompatible: herdr replied in a form contr does not read.
	Incompatible
)

// ConnState is the state of the connection, as the UI shows it. It can be
// compared with ==. Only the field that fits the kind is set.
type ConnState struct {
	Kind ConnKind
	// ServerVersion is set for Connected.
	ServerVersion string
	// Reason is set for NotRunning.
	Reason string
	// Message is set for Incompatible. It is for the user.
	Message string
}

// Line returns the line the terminal area shows for this state, and false
// when connected.
func (s ConnState) Line() (string, bool) {
	switch s.Kind {
	case Connecting:
		return "connecting to herdr…", true
	case NotRunning:
		return NotRunningLine, true
	case Incompatible:
		return s.Message, true
	}
	return "", false
}

// Update is one message from the reader to the UI. The UI applies them in
// order. Tree is nil for a state update; it is not nil for a tree update.
type Update struct {
	State ConnState
	Tree  *HerdrTree
}

// LiveHerdr is the UI's copy of the connection. Apply each [Update] in order.
type LiveHerdr struct {
	State ConnState
	Tree  *HerdrTree
}

// NewLiveHerdr returns the copy before any update: Connecting, with no tree.
func NewLiveHerdr() LiveHerdr { return LiveHerdr{State: ConnState{Kind: Connecting}} }

// Apply takes in one update. Any state other than Connected drops the tree,
// so the UI never shows a tree from a connection that has ended.
func (l *LiveHerdr) Apply(u Update) {
	if u.Tree != nil {
		l.Tree = u.Tree
		return
	}
	if u.State.Kind != Connected {
		l.Tree = nil
	}
	l.State = u.State
}

// Target tells which herdr to follow.
type Target struct {
	// Socket is the API socket path. When empty, the target is this user's
	// herdr and the path is looked up at every attempt.
	Socket string
}

// LocalTarget follows this user's herdr.
func LocalTarget() Target { return Target{} }

// SocketTarget follows the herdr at the socket path.
func SocketTarget(path string) Target { return Target{Socket: path} }

// resolve returns the socket to dial: the target's own path, or local() for
// the local target.
func resolve(t Target, local func() (string, bool)) (string, bool) {
	if t.Socket != "" {
		return t.Socket, true
	}
	return local()
}

// Reader follows herdr on its own goroutine. Stop it with [Reader.Stop].
type Reader struct {
	ctl  *control
	done chan struct{}
}

// StartReader starts the reader goroutine and returns at once. The goroutine
// sends Connecting, then each change of state and tree on out, until the
// reader is stopped or ctx ends. Sends block, so the receiver must drain out
// or cancel ctx. The goroutine does not close out.
func StartReader(ctx context.Context, target Target, out chan<- Update) *Reader {
	return startReader(ctx, target, out, refreshEvery)
}

// startReader is [StartReader] with the snapshot interval given, so a test
// can use a short one.
func startReader(ctx context.Context, target Target, out chan<- Update, every time.Duration) *Reader {
	ctx, cancel := context.WithCancel(ctx)
	ctl := &control{ctx: ctx, cancel: cancel}
	ctl.visible.Store(true)
	r := &reader{out: out, ctl: ctl, every: every}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.run(target)
	}()
	return &Reader{ctl: ctl, done: done}
}

// SetVisible tells the reader whether the window is shown. While it is
// hidden, the reader keeps its connection and applies events, but takes no
// snapshot on the refresh timer. When the window is shown again, the reader
// takes one snapshot. This never blocks.
func (r *Reader) SetVisible(visible bool) {
	was := r.ctl.visible.Swap(visible)
	if visible && !was {
		r.ctl.wake()
	}
}

// Visible returns the value last given to [Reader.SetVisible]. It is true at
// start.
func (r *Reader) Visible() bool { return r.ctl.visible.Load() }

// Stop asks the goroutine to end and does not wait for it, so it never blocks
// the UI thread. A blocked event read and a retry wait end at once; a call in
// flight ends within the call timeout. Stop may be called more than once.
func (r *Reader) Stop() { r.ctl.stop() }

// Done is closed when the goroutine has ended.
func (r *Reader) Done() <-chan struct{} { return r.done }

// control is what the UI side shares with the reader goroutine. The goroutine
// never wakes on a timer to look at it: stop and wake wake the goroutine.
type control struct {
	ctx     context.Context
	cancel  context.CancelFunc
	visible atomic.Bool
	// reads counts the event reads started. Tests use it to show that a quiet
	// stream does not wake the goroutine.
	reads atomic.Int64

	mu sync.Mutex
	// sub is the open subscription, while there is one. Wake on it ends the
	// goroutine's blocked read.
	sub *Subscription
	// woken is set when wake ended sub.
	woken bool
}

func (c *control) stop() {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sub != nil {
		c.logIfErr("wake on stop", c.sub.Wake())
	}
}

// wake ends the open subscription, if any, so the goroutine subscribes again
// and takes a snapshot. With no subscription open, the goroutine takes a
// snapshot before its next read anyway.
func (c *control) wake() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sub != nil {
		c.woken = true
		c.logIfErr("wake on show", c.sub.Wake())
		c.sub = nil
	}
}

func (c *control) logIfErr(what string, err error) {
	if err != nil {
		slog.Debug("herdr reader: "+what, "err", err)
	}
}

// follow keeps sub for stop and wake. It returns false when a stop came
// first: then nothing will wake a read on it.
func (c *control) follow(sub *Subscription) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.woken = false
	if c.ctx.Err() != nil {
		return false
	}
	c.sub = sub
	return true
}

// unfollow forgets the subscription. It returns true when wake ended it.
func (c *control) unfollow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sub = nil
	woken := c.woken
	c.woken = false
	return woken
}

// retryDelay is the wait before retry attempt (0 is the first): 500 ms,
// doubling, capped at 5 s.
func retryDelay(attempt uint32) time.Duration {
	if attempt >= 4 {
		return maxRetry
	}
	return min(baseRetry<<attempt, maxRetry)
}

type endKind uint8

const (
	// endNotRunning: nothing answers at the socket. Retry with backoff.
	endNotRunning endKind = iota
	// endIncompatible: herdr answered in a form contr does not read. Retry
	// after a fixed wait.
	endIncompatible
	// endStop: the reader was stopped, or ctx ended.
	endStop
)

// ended says why a connection ended.
type ended struct {
	kind endKind
	text string
}

// reader is the goroutine's side of the connection. state and tree are the
// last values sent, so nothing is sent or logged twice.
type reader struct {
	out   chan<- Update
	ctl   *control
	every time.Duration

	state         *ConnState
	tree          *HerdrTree
	serverVersion string
	attempt       uint32
}

func (r *reader) run(target Target) {
	if r.report(ConnState{Kind: Connecting}) != nil {
		return
	}
	for {
		var end *ended
		if path, ok := resolve(target, APISocketPath); ok {
			end = r.connect(path)
		} else {
			end = &ended{kind: endNotRunning, text: "no HOME"}
		}
		var state ConnState
		var delay time.Duration
		switch end.kind {
		case endNotRunning:
			delay = retryDelay(r.attempt)
			r.attempt++
			state = ConnState{Kind: NotRunning, Reason: end.text}
		case endIncompatible:
			delay = incompatibleRetry
			state = ConnState{Kind: Incompatible, Message: end.text}
		default:
			return
		}
		r.tree = nil
		r.serverVersion = ""
		if r.report(state) != nil || !r.wait(delay) {
			return
		}
	}
}

// connect follows herdr until the connection ends. It always returns the
// reason: a session always ends with one.
func (r *reader) connect(path string) *ended {
	pong, err := Ping(path, callTimeout)
	if err != nil {
		return r.failed(err)
	}
	if pong.Protocol != APIProtocol {
		slog.Warn(fmt.Sprintf("herdr api protocol=%d, contr expects %d", pong.Protocol, APIProtocol))
	}
	slog.Info(fmt.Sprintf("herdr connected server_version=%s protocol=%d", pong.Version, pong.Protocol))
	r.serverVersion = pong.Version
	r.attempt = 0
	if err := r.report(ConnState{Kind: Connected, ServerVersion: pong.Version}); err != nil {
		return stopped()
	}
	var panes []string
	var refused []string
	refusedSet := false
	for {
		// Subscribe before the snapshot, so no change falls between the two.
		sub, end := r.open(path, &panes, &refused, &refusedSet)
		if end != nil {
			return end
		}
		// Kept before the snapshot, so a show during the snapshot is not lost.
		if !r.ctl.follow(sub) {
			closeSub(sub)
			return stopped()
		}
		var watched *ended
		again, end := r.refresh(path, &panes)
		switch {
		case end != nil:
			watched = end
		case again:
			watched = r.watch(path, sub, &panes)
		}
		woken := r.ctl.unfollow()
		closeSub(sub)
		switch {
		case watched == nil:
		case r.isStopped():
			return stopped()
		case watched.kind == endNotRunning && woken:
			// The window was shown: subscribe again, then take a snapshot.
		default:
			return watched
		}
	}
}

func closeSub(sub *Subscription) {
	if err := sub.Close(); err != nil {
		slog.Debug("herdr reader: close subscription", "err", err)
	}
}

func stopped() *ended { return &ended{kind: endStop} }

// open subscribes to panes. A pane_not_found means a pane closed after the
// last snapshot: this open subscribes to none, and the next snapshot brings
// the panes that are left. Each open falls back at most once. Herdr refusing
// the same set twice in a row is not a race, so the second refusal ends the
// session. refused is the set of the last refusal (when refusedSet is true);
// a subscription with panes that succeeds clears it.
func (r *reader) open(path string, panes, refused *[]string, refusedSet *bool) (*Subscription, *ended) {
	sub, err := OpenSubscription(path, *panes, callTimeout)
	if err == nil {
		if len(*panes) > 0 {
			*refusedSet = false
			*refused = nil
		}
		return sub, nil
	}
	var we *WireError
	if errors.As(err, &we) && we.Kind == ErrServer && we.Code == "pane_not_found" &&
		len(*panes) > 0 && !(*refusedSet && equalIDs(*refused, *panes)) {
		*refused = append([]string(nil), *panes...)
		*refusedSet = true
		*panes = nil
		sub, err = OpenSubscription(path, *panes, callTimeout)
		if err == nil {
			return sub, nil
		}
	}
	return nil, r.failed(err)
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// refresh takes a snapshot and publishes it. It returns true when the pane
// ids match panes. Otherwise panes becomes the new list and the subscription
// must reopen.
func (r *reader) refresh(path string, panes *[]string) (bool, *ended) {
	snap, err := Snapshot(path, callTimeout)
	if err != nil {
		return false, r.failed(err)
	}
	tree := TreeFromSnapshot(&snap)
	if ids := tree.PaneIDs(); !equalIDs(ids, *panes) {
		*panes = ids
		return false, nil
	}
	if err := r.publish(tree); err != nil {
		return false, stopped()
	}
	return true, nil
}

// watch reads events until the stream fails (a reason) or the pane ids change
// (nil). The stop flag is checked before every read, however busy the stream
// is. A snapshot runs when the stream goes quiet, or coalesce after the first
// event not yet in a snapshot, but never less than snapshotGap after the last
// one. While the window is shown, it also runs every r.every. A read waits
// only until the next snapshot is due; with none due (window hidden, no event
// pending), it waits with no timer until an event, the end of the stream, or
// a stop or show from [control].
func (r *reader) watch(path string, sub *Subscription, panes *[]string) *ended {
	var pending time.Time // first event not yet in a snapshot; zero if none
	refreshed := time.Now()
	for {
		if r.isStopped() {
			return stopped()
		}
		var due time.Time // zero: none due
		if !pending.IsZero() {
			due = later(pending.Add(coalesce), refreshed.Add(snapshotGap))
		}
		if r.ctl.visible.Load() {
			timer := refreshed.Add(r.every)
			if due.IsZero() || timer.Before(due) {
				due = timer
			}
		}
		r.ctl.reads.Add(1)
		wait := WaitForever
		if !due.IsZero() {
			wait = time.Until(due)
		}
		gotEvent, err := sub.NextEvent(wait)
		if err != nil {
			return r.failed(err)
		}
		quiet := !gotEvent
		if gotEvent && pending.IsZero() {
			pending = time.Now()
		}
		// Read again: the window can be hidden while the read waits for the timer.
		visible := r.ctl.visible.Load()
		isDue := (!pending.IsZero() && (quiet ||
			(time.Since(pending) >= coalesce && time.Since(refreshed) >= snapshotGap))) ||
			(visible && time.Since(refreshed) >= r.every)
		if isDue {
			same, end := r.refresh(path, panes)
			if end != nil {
				return end
			}
			if !same {
				return nil
			}
			pending = time.Time{}
			refreshed = time.Now()
		}
	}
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// publish sends tree when it differs from the last one sent, and logs what
// changed. It returns an error when the reader must stop.
func (r *reader) publish(tree HerdrTree) error {
	switch prev := r.tree; {
	case prev != nil && prev.Equal(tree):
		return nil
	case prev != nil && prev.SameShape(tree):
		for _, change := range prev.StatusChanges(tree) {
			slog.Info(change.LogLine())
		}
	default:
		slog.Info(fmt.Sprintf("herdr tree workspaces=%d tabs=%d panes=%d",
			len(tree.Workspaces), tree.TabCount(), tree.PaneCount()))
		for _, line := range tree.LogLines() {
			slog.Info(line)
		}
	}
	r.tree = &tree
	return r.send(Update{Tree: &tree})
}

// report sends state unless it is the state already sent, and logs the
// change. It returns an error when the reader must stop.
func (r *reader) report(state ConnState) error {
	if r.state != nil && *r.state == state {
		return nil
	}
	switch state.Kind {
	case NotRunning:
		slog.Info(fmt.Sprintf("herdr not running (%s); retrying", state.Reason))
	case Incompatible:
		slog.Info("herdr incompatible: " + state.Message)
	}
	r.state = &state
	return r.send(Update{State: state})
}

var errStopped = errors.New("reader stopped")

func (r *reader) send(u Update) error {
	select {
	case r.out <- u:
		return nil
	case <-r.ctl.ctx.Done():
		return errStopped
	}
}

// failed says how a failed call ends the session. A reply that does not
// parse, or an error reply, means an incompatible herdr; anything else means
// none runs.
func (r *reader) failed(err error) *ended {
	var we *WireError
	if errors.As(err, &we) && (we.Kind == ErrUnexpected || we.Kind == ErrServer) {
		return &ended{kind: endIncompatible, text: r.incompatible()}
	}
	return &ended{kind: endNotRunning, text: err.Error()}
}

// incompatible is the message for a reply contr cannot read. It names the
// version once a pong was read.
func (r *reader) incompatible() string {
	if r.serverVersion != "" {
		return fmt.Sprintf("herdr %s replied in an unexpected form · update contr", r.serverVersion)
	}
	return "herdr replied in an unexpected form · update contr"
}

func (r *reader) isStopped() bool { return r.ctl.ctx.Err() != nil }

// wait waits for delay. It returns false when the reader must stop first.
func (r *reader) wait(delay time.Duration) bool {
	if r.isStopped() {
		return false
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		return !r.isStopped()
	case <-r.ctl.ctx.Done():
		return false
	}
}
