// The surface link: one background connection to herdr's endpoint socket.
// One goroutine connects, keeps the connection up and reads what herdr sends.
// A second goroutine per connection writes what the UI sends. The UI gets
// LinkUpdates through a callback and calls Handle.TakeLatest. It never waits
// on the socket.
// Written from herdr's wire layout; see NOTICE.

package endpoint

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"
)

const (
	// linkHandshake is the longest wait for a handshake. It is also the write
	// timeout of the socket.
	linkHandshake = 4 * time.Second
	// linkOutbox is the number of frames queued for the writer. A full queue
	// refuses input with ErrLinkFull.
	linkOutbox = 256
	// baseRetry is the wait after the first failed attempt. Each further
	// failure doubles it, up to maxRetry.
	baseRetry = 500 * time.Millisecond
	maxRetry  = 5 * time.Second
	// refusedWait is the wait after herdr refused contr or sent a frame contr
	// cannot read. A retry sooner would fail the same way.
	refusedWait = 30 * time.Second
)

// LinkStateKind is the kind of a [LinkState].
type LinkStateKind int

const (
	// LinkConnecting: connecting, or reconnecting.
	LinkConnecting LinkStateKind = iota + 1
	// LinkLive: connected.
	LinkLive
	// LinkNotRunning: herdr is not running or closed the connection. Message
	// is for the log.
	LinkNotRunning
	// LinkIncompatible: herdr refused contr, or sent what contr cannot read.
	// Message is for the user.
	LinkIncompatible
)

// LinkState is the connection state, as the UI shows it. Compare with ==.
type LinkState struct {
	Kind LinkStateKind
	// ServerVersion is set for LinkLive.
	ServerVersion string
	// Untested is set for LinkLive when contr has not been tested with this
	// herdr version.
	Untested bool
	// Message is set for LinkNotRunning and LinkIncompatible.
	Message string
}

// LinkUpdate is what the link tells the UI. The UI applies them in order.
// It is one of [StateUpdate], [SurfaceUpdate], [SnapshotUpdate] and
// [ClipboardUpdate].
type LinkUpdate interface{ linkUpdate() }

// StateUpdate reports a new connection state. The link sends a state only
// when it changes.
type StateUpdate struct{ State LinkState }

// SurfaceUpdate says the newest surface changed. Call [Handle.TakeLatest] for
// it: it can be newer than the Surface here, because updates coalesce. Treat
// Surface as read-only.
type SurfaceUpdate struct{ Surface *Surface }

// SnapshotUpdate carries a shell snapshot.
type SnapshotUpdate struct{ Snapshot *ShellSnapshot }

// ClipboardUpdate carries text a program wrote to the clipboard with OSC 52.
// The UI writes it to the system clipboard. Each update is one write, in the
// order herdr sent them.
type ClipboardUpdate struct{ Text string }

func (StateUpdate) linkUpdate()     {}
func (SurfaceUpdate) linkUpdate()   {}
func (SnapshotUpdate) linkUpdate()  {}
func (ClipboardUpdate) linkUpdate() {}

// LinkTarget says where the link connects. The zero value is herdr's
// endpoint socket at the path herdr uses for this user.
type LinkTarget struct {
	// Socket is a given socket path. Empty means the local default.
	Socket string
}

// path returns the socket to connect to. For the local default, env finds it
// the way herdr does. It is false when that needs HOME and HOME is not set.
func (t LinkTarget) path(env func(string) string) (string, bool) {
	if t.Socket != "" {
		return t.Socket, true
	}
	return EndpointPathFrom(env)
}

// Errors of a send that did not queue. Nothing was sent.
var (
	// ErrLinkNotConnected: no live connection. Input sent now is not kept.
	ErrLinkNotConnected = errors.New("endpoint link is not connected")
	// ErrLinkFull: the writer is behind and its queue is full.
	ErrLinkFull = errors.New("endpoint link outbox is full")
)

// shared is what the UI and the link goroutines share.
type shared struct {
	stop     chan struct{}
	stopOnce sync.Once

	mu sync.Mutex // guards every field below
	// size is the size and cell the next hello carries. Resize keeps it current.
	size    GridSize
	cell    CellPx
	outbox  chan ClientMsg // the live connection's outbox; nil while there is none
	latest  *Surface       // the newest surface not yet taken
	pending bool           // true while a SurfaceUpdate is queued or not yet taken
}

// Handle is the UI's side of the link. Its methods never block, and are safe
// to call from any goroutine.
type Handle struct{ s *shared }

// SendInput queues a shell.input for paneID.
func (h Handle) SendInput(paneID string, events []InputEvent) error {
	return h.push(InputMsg{PaneID: paneID, Events: slices.Clone(events)})
}

// Resize stores the size for the next connection, and queues a resize if one
// is live. It returns ErrLinkNotConnected when none is.
func (h Handle) Resize(size GridSize, cell CellPx) error {
	h.s.mu.Lock()
	h.s.size, h.s.cell = size, cell
	h.s.mu.Unlock()
	return h.push(ResizeMsg{Size: size, Cell: cell})
}

// SetFocus queues the window's focus state for the server.
func (h Handle) SetFocus(focused bool) error {
	return h.push(FocusMsg{Focused: focused})
}

// TakeLatest takes the newest surface that has not been taken yet, or nil.
// Taking it clears the pending SurfaceUpdate notice.
func (h Handle) TakeLatest() *Surface {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	h.s.pending = false
	surface := h.s.latest
	h.s.latest = nil
	return surface
}

func (h Handle) push(msg ClientMsg) error {
	h.s.mu.Lock()
	defer h.s.mu.Unlock()
	if h.s.outbox == nil {
		return ErrLinkNotConnected
	}
	select {
	case h.s.outbox <- msg:
		return nil
	default:
		return ErrLinkFull
	}
}

// closeOutbox ends the live outbox, if any: the writer sends what is still
// queued and then exits. With detach, a Detach is queued first, best effort.
func (s *shared) closeOutbox(detach bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.outbox == nil {
		return
	}
	if detach {
		select {
		case s.outbox <- DetachMsg{}:
		default:
		}
	}
	close(s.outbox)
	s.outbox = nil
}

// Link is the running link.
type Link struct {
	s    *shared
	done chan struct{}
}

// Stop stops the link. It does not block, and is safe to call more than
// once. A Detach is queued first, best effort. The link goroutines end soon
// after, unless the link is in a handshake, which takes up to 4 s. Wait on
// [Link.Done] to know they ended. After Done the link calls notify no more.
func (l *Link) Stop() {
	l.s.stopOnce.Do(func() { close(l.s.stop) })
	l.s.closeOutbox(true)
}

// Done is closed when every goroutine of the link has ended.
func (l *Link) Done() <-chan struct{} { return l.done }

// SpawnLink starts the link. It connects at once and keeps trying.
//
// notify gets every update, in order, from the link's goroutine. It must not
// block: forward the update with Window.Update and return. It may call
// [Handle.TakeLatest]. Surfaces coalesce: while a SurfaceUpdate is not taken,
// newer surfaces send no new one.
func SpawnLink(target LinkTarget, size GridSize, cell CellPx, notify func(LinkUpdate)) (*Link, Handle) {
	s := &shared{stop: make(chan struct{}), size: size, cell: cell}
	l := &Link{s: s, done: make(chan struct{})}
	r := &runner{target: target, s: s, notify: notify}
	go func() {
		defer close(l.done)
		r.run()
	}()
	return l, Handle{s}
}

// runner is the link goroutine's state.
type runner struct {
	target LinkTarget
	s      *shared
	notify func(LinkUpdate)
	// state is the last state sent. A state is sent only when it changes.
	state *LinkState
	// failures counts failed attempts in a row. The first surface of a
	// connection resets it. A connection that closes before that is a failure.
	failures uint
}

// endKind tells how a live connection ended.
type endKind int

const (
	// endStopped: the link was stopped.
	endStopped endKind = iota + 1
	// endOutOfSync: the surface no longer follows herdr's patches. Reconnect
	// for a full surface.
	endOutOfSync
	// endGone: herdr closed the connection or shut down. The reason is for
	// the log.
	endGone
	// endUnreadable: herdr sent a frame contr cannot read. The message is for
	// the user.
	endUnreadable
)

type ended struct {
	kind endKind
	text string
}

func (r *runner) run() {
	r.setState(LinkState{Kind: LinkConnecting})
	for !r.stopping() && r.connectOnce() {
	}
}

// connectOnce makes one attempt: connect, then serve the connection. False
// when the link must stop.
func (r *runner) connectOnce() bool {
	path, ok := r.target.path(os.Getenv)
	if !ok {
		r.setState(LinkState{Kind: LinkNotRunning, Message: "no HOME"})
		return r.retry()
	}
	r.s.mu.Lock()
	size, cell := r.s.size, r.s.cell
	r.s.mu.Unlock()
	conn, err := Open(path, size, cell, linkHandshake)
	if err != nil {
		var ce *ConnError
		if !errors.As(err, &ce) {
			r.setState(LinkState{Kind: LinkNotRunning, Message: err.Error()})
			return r.retry()
		}
		switch ce.Kind {
		case Incompatible, Codec:
			return r.refuse(ce.Error())
		default:
			r.setState(LinkState{Kind: LinkNotRunning, Message: ce.Error()})
			return r.retry()
		}
	}
	return r.serve(conn)
}

// serve runs one connection until it ends. False when the link must stop.
func (r *runner) serve(conn *Conn) bool {
	version := conn.Welcome.ServerVersion
	live := LinkState{Kind: LinkLive, ServerVersion: version, Untested: !conn.Version.Tested()}
	end := r.session(conn, live, version)
	switch end.kind {
	case endStopped:
		return false
	// The next two wait with the backoff: a connection that ends before its
	// first surface counts as a failure, so a herdr that accepts and closes
	// at once is not retried every baseRetry.
	case endOutOfSync:
		r.setState(LinkState{Kind: LinkConnecting})
		return r.retry()
	case endGone:
		r.setState(LinkState{Kind: LinkNotRunning, Message: end.text})
		return r.retry()
	default:
		return r.refuse(end.text)
	}
}

// session starts the writer, announces live, and reads until the connection
// ends. It returns after the writer ended and the socket is closed.
func (r *runner) session(conn *Conn, live LinkState, version string) ended {
	// No read timeout: Stop and a failed write close the socket, which ends
	// the read. So the reader never wakes to check for a stop.
	conn.SetReadTimeout(0)
	outbox := make(chan ClientMsg, linkOutbox)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		writeFrames(conn, outbox)
	}()
	r.s.mu.Lock()
	r.s.outbox = outbox
	r.s.mu.Unlock()
	if r.stopping() {
		// Stop came between the handshake and here: queue the Detach it missed.
		r.s.closeOutbox(true)
	}
	r.setState(live)
	end := r.read(conn, version)
	// Closing the outbox lets the writer send what is still queued, a Detach
	// for one, then close the socket and exit.
	r.s.closeOutbox(false)
	<-writerDone
	_ = conn.Close()
	r.s.mu.Lock()
	r.s.latest = nil
	r.s.mu.Unlock()
	return end
}

// read reads and publishes server messages until the connection ends.
func (r *runner) read(conn *Conn, version string) ended {
	var held *Surface
	for {
		msg, err := conn.Recv()
		if r.stopping() {
			return ended{kind: endStopped}
		}
		if err != nil {
			return readFailure(err, version)
		}
		switch m := msg.(type) {
		case nil:
			// The read timeout passed. Read again.
		case ControlMsg:
			if m.Kind != SnapshotKind {
				continue
			}
			var snapshot ShellSnapshot
			if err := json.Unmarshal([]byte(m.Data), &snapshot); err != nil {
				slog.Debug("herdr snapshot not readable", "err", err)
				continue
			}
			r.notify(SnapshotUpdate{Snapshot: &snapshot})
		case SurfaceMsg:
			// The connection works: the next failure starts the backoff again.
			r.failures = 0
			surface := m.Frame.Surface
			held = &surface
			r.publish(held)
		case PatchMsg:
			if held == nil || held.ApplyPatch(m.Patch) != nil {
				slog.Warn("surface patch out of sync; reconnecting")
				return ended{kind: endOutOfSync}
			}
			r.publish(held)
		case ClipboardMsg:
			r.notify(ClipboardUpdate{Text: m.Text})
		case ShutdownMsg:
			reason := "herdr stopped"
			if m.Reason != nil {
				reason = *m.Reason
			}
			return ended{kind: endGone, text: reason}
		case ShellErrorMsg:
			slog.Warn("herdr shell error", "message", m.Text)
		}
	}
}

// publish makes a copy of surface the newest one, and tells the UI unless a
// notice is already pending. The copy is needed because a patch changes the
// held surface in place. Rows are never changed in place, so the copy shares
// them. It never blocks.
func (r *runner) publish(surface *Surface) {
	newest := *surface
	newest.Rows = slices.Clone(surface.Rows)
	newest.Panes = slices.Clone(surface.Panes)
	r.s.mu.Lock()
	r.s.latest = &newest
	wasPending := r.s.pending
	r.s.pending = true
	r.s.mu.Unlock()
	if !wasPending {
		r.notify(SurfaceUpdate{Surface: &newest})
	}
}

// setState sends the state if it changed.
func (r *runner) setState(state LinkState) {
	if r.state != nil && *r.state == state {
		return
	}
	slog.Info("surface link state", "kind", state.Kind, "version", state.ServerVersion, "message", state.Message)
	r.state = &state
	r.notify(StateUpdate{State: state})
}

// refuse shows the refusal and waits before the next attempt. False when the
// link must stop.
func (r *runner) refuse(message string) bool {
	r.setState(LinkState{Kind: LinkIncompatible, Message: message})
	return r.pause(refusedWait)
}

// retry waits before the next attempt, longer after each failure in a row.
// False when the link must stop.
func (r *runner) retry() bool {
	wait := retryDelay(r.failures)
	r.failures++
	return r.pause(wait)
}

// pause waits for total, or for a stop. False when the link must stop.
func (r *runner) pause(total time.Duration) bool {
	timer := time.NewTimer(total)
	defer timer.Stop()
	select {
	case <-r.s.stop:
		return false
	case <-timer.C:
		return true
	}
}

func (r *runner) stopping() bool {
	select {
	case <-r.s.stop:
		return true
	default:
		return false
	}
}

// writeFrames is the writer goroutine: it sends queued messages in order
// until the outbox closes or a write fails. It then closes the socket, so the
// reader sees the end too.
func writeFrames(conn *Conn, outbox <-chan ClientMsg) {
	defer func() { _ = conn.Close() }()
	w := conn.Writer()
	for msg := range outbox {
		if err := w.Send(msg); err != nil {
			slog.Warn("herdr endpoint write failed", "err", err)
			return
		}
	}
}

// readFailure says what a failed read means for the connection.
func readFailure(err error, version string) ended {
	var ce *ConnError
	switch {
	case !errors.As(err, &ce):
		return ended{kind: endGone, text: err.Error()}
	case ce.Kind == Codec, errors.Is(ce.Err, ErrFrameTooLarge):
		return ended{kind: endUnreadable, text: unreadable(version)}
	case errors.Is(ce.Err, io.ErrUnexpectedEOF):
		return ended{kind: endGone, text: "herdr closed the endpoint"}
	default:
		return ended{kind: endGone, text: ce.Error()}
	}
}

func unreadable(version string) string {
	return "herdr " + version + " sent a surface contr cannot read · update contr"
}

// retryDelay is the wait after failures failed attempts in a row: baseRetry,
// doubled each time, at most maxRetry.
func retryDelay(failures uint) time.Duration {
	wait := baseRetry
	for range failures {
		wait *= 2
		if wait >= maxRetry {
			return maxRetry
		}
	}
	return wait
}
