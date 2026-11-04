// Package ghostty shows hc terminals with MyGo's terminal plugin
// (libghostty-vt): the plugin emulates and draws, hc keeps the program.
package ghostty

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

// HistoryLines is how many lines of scrollback an attach asks for.
const HistoryLines = 1000

// maxHeld is the most input bytes Conn keeps while it has no live stream.
const maxHeld = 64 << 10

// The wait after a lost connection is baseRetry, doubled by each further
// failure up to maxRetry, as for the herdr endpoint link. firstWait is how
// long the first attach waits for the view's size. They are variables so
// tests can shorten them.
var (
	baseRetry = 500 * time.Millisecond
	maxRetry  = 5 * time.Second
	firstWait = 500 * time.Millisecond
)

// ErrNotRunning is what Write returns when the terminal's program ended, or
// the held input is full.
var ErrNotRunning = errors.New("terminal not running")

// Stream is one attached hc terminal. *hc.Stream is one.
type Stream interface {
	Events() <-chan hc.StreamEvent
	Err() error
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Focus(focused bool) error
	Close() error
}

// Source attaches to one hc terminal. Attach fills in the terminal address of
// the params. It returns an *hc.Error of kind "incompatible" or
// "needs_framed" when the core cannot do framed attach, and "finished" or
// "not_found" when the terminal is not running. Watch gives the events of
// that terminal; it can be nil. If Source has a Close method, Conn.Close calls
// it.
type Source interface {
	Attach(ctx context.Context, p hc.AttachParams) (Stream, error)
	Watch(ctx context.Context) (events <-chan hc.Event, stop func(), err error)
}

// StateKind is the kind of a [State].
type StateKind int

const (
	// Connecting: no attach has finished yet.
	Connecting StateKind = iota + 1
	// Live: attached.
	Live
	// NotRunning: the program ended, or the connection is lost and Conn
	// tries again unless the end is final.
	NotRunning
	// Incompatible: the core refused contr, or cannot do framed attach.
	Incompatible
)

// State is the connection state, as the frame shows it. Compare with ==.
type State struct {
	Kind StateKind
	// Message is set for NotRunning and Incompatible.
	Message string
}

// Options configure a Conn.
type Options struct {
	// OnState gets each change of the state, on any goroutine. It must not
	// wait for the UI thread.
	OnState func(State)
	// Cols and Rows are the size of the first attach: 80 by 24 when zero.
	// The view's size replaces it as soon as the view tells it.
	Cols, Rows uint16
}

// Conn is a terminal.Options.Conn over an hc terminal: the plugin reads the
// program's output from it, writes what is typed to it, and tells it the
// size. Unlike a plain stream it outlives a lost connection: it attaches
// again with backoff and with Resumed, and holds what is typed meanwhile. Its
// Read ends only when Close is called, so the plugin keeps its screen.
type Conn struct {
	src Source
	on  func(State)
	ctx context.Context
	end context.CancelFunc
	wg  sync.WaitGroup
	out chan []byte // output from the pump to Read

	rmu sync.Mutex // Read's leftover
	buf []byte

	mu        sync.Mutex
	stream    Stream // nil unless attached
	live      bool   // attached and the held input is flushed
	held      []byte // input typed while not live
	attaching bool
	tried     bool // an attach was started
	resumed   bool
	noRetry   bool // the last attach failed for good: only an event retries
	visible   bool // only then may a timer retry
	failures  uint // lost connections in a row, for the backoff
	timer     *time.Timer
	first     *time.Timer
	stopped   bool
	focused   bool
	cols      uint16
	rows      uint16
	cell      [2]int
	state     State
}

// NewConn returns a Conn for src. It never blocks: the attach and the watch
// run on goroutines. The first attach waits for the view's size, or
// firstWait. Close ends it.
func NewConn(src Source, o Options) *Conn {
	ctx, end := context.WithCancel(context.Background())
	c := &Conn{src: src, on: o.OnState, ctx: ctx, end: end, out: make(chan []byte),
		visible: true, cols: o.Cols, rows: o.Rows, state: State{Kind: Connecting}}
	if c.cols == 0 || c.rows == 0 {
		c.cols, c.rows = 80, 24
	}
	c.first = time.AfterFunc(firstWait, func() {
		c.mu.Lock()
		tried := c.tried
		c.mu.Unlock()
		if !tried { // no size came; a later failure waits for the backoff
			c.retry(false)
		}
	})
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.watch()
	}()
	return c
}

// State is the current connection state.
func (c *Conn) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Conn) setState(s State) {
	c.mu.Lock()
	same := c.state == s
	c.state = s
	c.mu.Unlock()
	if !same && c.on != nil {
		c.on(s)
	}
}

// Read gives the program's output. It waits through a lost connection and
// returns io.EOF only after Close.
func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.buf) == 0 {
		select {
		case b := <-c.out:
			c.buf = b
		case <-c.ctx.Done():
			return 0, io.EOF
		}
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// Write sends typed input to the program. While Conn has no live stream it
// keeps the input, in order, and sends it first once attached; it also tries
// an attach. After the program ended it returns ErrNotRunning.
func (c *Conn) Write(p []byte) (int, error) {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if !c.live {
		if c.noRetry || len(c.held)+len(p) > maxHeld {
			c.mu.Unlock()
			return 0, ErrNotRunning
		}
		c.held = append(c.held, p...)
		c.mu.Unlock()
		c.retry(false)
		return len(p), nil
	}
	s := c.stream
	c.mu.Unlock()
	return s.Write(p)
}

// ResizePixels tells hc the size the view fits. The size is kept for the next
// attach too, and with it the size of a cell for the core's size queries. The
// first size starts the first attach, so that it has the view's size.
func (c *Conn) ResizePixels(cols, rows, cellW, cellH int) error {
	if cols <= 0 || rows <= 0 || cols > 0xFFFF || rows > 0xFFFF {
		return nil
	}
	c.mu.Lock()
	changed := c.cols != uint16(cols) || c.rows != uint16(rows)
	c.cols, c.rows, c.cell = uint16(cols), uint16(rows), [2]int{cellW, cellH}
	s, tried := c.stream, c.tried
	c.mu.Unlock()
	if s == nil {
		if !tried { // the first size starts the first attach; later ones wait for the backoff
			c.retry(false)
		}
		return nil
	}
	if !changed {
		return nil
	}
	return s.Resize(uint16(cols), uint16(rows))
}

// SetFocus tells the core whether the window has the focus. It is sent again
// after an attach.
func (c *Conn) SetFocus(focused bool) error {
	c.mu.Lock()
	c.focused = focused
	s := c.stream
	c.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Focus(focused)
}

// SetVisible tells Conn whether the window shows. While it is hidden no retry
// timer runs. Showing the window tries at once if Conn is down and the
// failure was not final.
func (c *Conn) SetVisible(visible bool) {
	c.mu.Lock()
	if c.visible == visible {
		c.mu.Unlock()
		return
	}
	c.visible = visible
	if !visible && c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if visible {
		c.retry(false)
	}
}

// Close ends the reader, the watcher and the stream, and waits for them. The
// program keeps running in hc. It also closes the source when that has a
// Close method.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return nil
	}
	c.stopped = true
	s := c.stream
	for _, t := range []*time.Timer{c.timer, c.first} {
		if t != nil {
			t.Stop()
		}
	}
	c.timer = nil
	c.mu.Unlock()
	c.end()
	var err error
	if s != nil {
		err = s.Close()
	}
	c.wg.Wait()
	if cl, ok := c.src.(io.Closer); ok {
		_ = cl.Close()
	}
	return err
}

// watch follows the terminal's events until Conn closes: "restarted"
// attaches again. It dials, so it runs on its own goroutine.
func (c *Conn) watch() {
	events, stop, err := c.src.Watch(c.ctx)
	if err != nil {
		slog.Debug("terminal events not watched", "err", err)
		return
	}
	if events == nil {
		return
	}
	defer stop()
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return
			}
			if e.Method == "event" && e.Type == "restarted" {
				c.retry(true)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// scheduleRetry waits with the backoff, then attaches again. It does nothing
// while the window is hidden: SetVisible retries when it shows.
func (c *Conn) scheduleRetry() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || !c.visible || c.timer != nil {
		return
	}
	wait := retryDelay(c.failures)
	c.failures++
	c.timer = time.AfterFunc(wait, func() {
		c.mu.Lock()
		c.timer = nil
		c.mu.Unlock()
		c.retry(false)
	})
}

// retryDelay is the wait after failures lost connections in a row: baseRetry,
// doubled each time, at most maxRetry.
func retryDelay(failures uint) time.Duration {
	wait := baseRetry
	for range failures {
		wait *= 2
		if wait >= maxRetry {
			return maxRetry
		}
	}
	return min(wait, maxRetry)
}

// retry starts an attach if none runs and none is live. force also tries
// after a failure that is final until the terminal restarts.
func (c *Conn) retry(force bool) {
	c.mu.Lock()
	if c.stopped || c.attaching || c.stream != nil || (c.noRetry && !force) {
		c.mu.Unlock()
		return
	}
	c.attaching, c.tried = true, true
	c.wg.Add(1) // under the lock: Close sets stopped there, then waits
	c.mu.Unlock()
	go func() {
		defer c.wg.Done()
		c.run()
	}()
}

// run attaches, then pumps the stream's output to Read until it ends.
func (c *Conn) run() {
	c.mu.Lock()
	p := hc.AttachParams{Cols: c.cols, Rows: c.rows, History: HistoryLines, Resumed: c.resumed}
	if c.cell[0] > 0 && c.cell[1] > 0 {
		cell := c.cell
		p.Display = &hc.Display{Cell: &cell}
	}
	c.mu.Unlock()
	s, err := c.src.Attach(c.ctx, p)
	if err != nil {
		c.mu.Lock()
		c.attaching = false
		c.mu.Unlock()
		c.attachFailed(err)
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.attaching = false
		c.mu.Unlock()
		_ = s.Close()
		return
	}
	c.stream, c.attaching, c.resumed, c.noRetry = s, false, true, false
	cols, rows, focused := c.cols, c.rows, c.focused
	c.mu.Unlock()
	if cols != p.Cols || rows != p.Rows { // the view's size changed during the attach
		_ = s.Resize(cols, rows)
	}
	if focused {
		_ = s.Focus(true)
	}
	c.flushHeld(s)
	c.setState(State{Kind: Live})
	c.pump(s)

	c.mu.Lock()
	if c.stream == s {
		c.stream = nil
	}
	c.live = false
	stopped := c.stopped
	c.mu.Unlock()
	if stopped {
		_ = s.Close()
		return
	}
	var end *hc.StreamEnd
	msg := "terminal stream ended"
	if err := s.Err(); err != nil {
		msg = err.Error()
		if !errors.As(err, &end) {
			slog.Debug("terminal stream failed", "err", err)
		}
	}
	_ = s.Close()
	final := end != nil // the program ended; "restarted" attaches again
	c.mu.Lock()
	c.noRetry = final
	c.mu.Unlock()
	c.setState(State{Kind: NotRunning, Message: msg})
	if !final {
		c.scheduleRetry() // a lost connection
	}
}

// flushHeld writes the input typed before the stream was live, in order,
// before any later input: Write keeps holding until held is empty, and that
// check and the switch to live happen under one lock.
func (c *Conn) flushHeld(s Stream) {
	for {
		c.mu.Lock()
		b := c.held
		c.held = nil
		if len(b) == 0 || c.stopped {
			c.live = true
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		if _, err := s.Write(b); err != nil {
			slog.Debug("held input not written", "err", err)
			c.mu.Lock()
			c.live = true
			c.mu.Unlock()
			return
		}
	}
}

// pump sends the stream's output to Read until the stream or Conn ends. Read
// takes one chunk at a time, so a program that writes faster than the view
// reads stops at the stream's credit.
func (c *Conn) pump(s Stream) {
	events := s.Events()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			c.mu.Lock()
			c.failures = 0 // data came: the connection works
			c.mu.Unlock()
			if len(ev.Data) == 0 {
				continue
			}
			select {
			case c.out <- ev.Data:
			case <-c.ctx.Done():
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Conn) attachFailed(err error) {
	if c.ctx.Err() != nil {
		return
	}
	switch {
	case hc.IsKind(err, hc.KindIncompatible), hc.IsKind(err, "needs_framed"):
		c.giveUp()
		c.setState(State{Kind: Incompatible, Message: err.Error()})
	case hc.IsKind(err, "finished"), hc.IsKind(err, "not_found"), hc.IsKind(err, "machine_unknown"):
		c.giveUp()
		c.setState(State{Kind: NotRunning, Message: err.Error()})
	default: // "unavailable", a lost connection: the backoff tries again
		c.setState(State{Kind: NotRunning, Message: err.Error()})
		c.scheduleRetry()
	}
}

// giveUp marks a failure that is final until the terminal restarts, and drops
// the held input.
func (c *Conn) giveUp() {
	c.mu.Lock()
	c.noRetry, c.held = true, nil
	c.mu.Unlock()
}
