// Package ghostty shows hc terminals with MyGo's terminal plugin
// (libghostty-vt): the plugin emulates and draws, hc keeps the program.
package ghostty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	gterm "github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/hc"
)

// HistoryLines is how many lines of scrollback an attach asks for.
const HistoryLines = 1000

// maxHeld is the most input bytes Conn keeps while it has no live stream.
const maxHeld = 64 << 10

// The wait after a lost connection is baseRetry, doubled by each further
// failure up to maxRetry, as for the old endpoint link. firstWait is how
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
	SetDisplay(d hc.Display) error
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
	// Watch attaches as an observer first (D91): no size, focus or input go
	// to the core, so another viewer keeps its size. The first Write ends
	// the observer stream and attaches with input at the view's size; the
	// Conn then stays an input viewer.
	Watch bool
}

// Screen is the terminal the Conn feeds: the plugin's *terminal.Terminal,
// made with ResizeByConn. Conn sets its size where the core's stream says the
// size changed, in order with the output.
type Screen interface{ Resize(cols, rows int) }

// chunk is one item from the pump to Read: output, or a new size. seq is the
// place of the size in the order of all sizes Conn learned (see nextSize).
type chunk struct {
	data []byte
	size *hc.Size
	seq  uint64
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
	out chan chunk // output and sizes from the pump to Read

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
	colors    hc.Display // fg, bg and cursor of the view's theme; Cell is unused
	state     State
	watching  bool     // attach as an observer: no size, focus or input
	takeover  bool     // Write closed the observer stream to attach with input
	fitting   bool     // attached with input only to fit the view: no focus until a key (D199)
	core      *hc.Size // the core's size seen while watching
	sized     bool     // the view gave its size: before it, cols and rows are the default
	screen    Screen   // nil until SetScreen
	pending   *hc.Size // the newest size, for Read to give the screen
	sizes     uint64   // the seq of the last size Conn learned
	newest    uint64   // the seq of pending, or of the size the screen has
	wake      chan struct{}
	taken     func() // tests only: Read calls it after it takes a chunk
}

// NewConn returns a Conn for src. It never blocks: the attach and the watch
// run on goroutines. The first attach waits for the view's size, or
// firstWait. Close ends it.
func NewConn(src Source, o Options) *Conn {
	ctx, end := context.WithCancel(context.Background())
	c := &Conn{src: src, on: o.OnState, ctx: ctx, end: end, out: make(chan chunk),
		visible: true, cols: o.Cols, rows: o.Rows, state: State{Kind: Connecting}, watching: o.Watch,
		wake: make(chan struct{}, 1)}
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

// SetScreen gives Conn the terminal whose size it sets. A size that came
// before it waits for Read.
func (c *Conn) SetScreen(s Screen) {
	c.mu.Lock()
	c.screen = s
	c.mu.Unlock()
	c.poke()
}

// Read gives the program's output. It waits through a lost connection and
// returns io.EOF only after Close. A new size from the stream resizes the
// screen here, after the output before it was read, unless Conn learned a
// newer size meanwhile.
func (c *Conn) Read(p []byte) (int, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	for len(c.buf) == 0 {
		select {
		case <-c.wake:
			c.resizeScreen(nil, 0)
		case ch := <-c.out:
			if c.taken != nil {
				c.taken()
			}
			// A size this viewer set comes before later output.
			c.resizeScreen(ch.size, ch.seq)
			if ch.size != nil {
				continue
			}
			c.buf = ch.data
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
	if isSchemeReport(p) { // hc pushes the report itself (D213)
		return len(p), nil
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if c.watching { // the owner types: attach with input instead
		if c.noRetry || len(c.held)+len(p) > maxHeld {
			c.mu.Unlock()
			return 0, ErrNotRunning
		}
		c.watching, c.live = false, false
		c.held = append(c.held, p...)
		s := c.stream
		c.takeover = s != nil
		c.mu.Unlock()
		if s != nil {
			_ = s.Close() // run attaches again once the pump ends
		} else {
			c.retry(false)
		}
		return len(p), nil
	}
	if !c.live {
		if c.noRetry || len(c.held)+len(p) > maxHeld {
			c.mu.Unlock()
			return 0, ErrNotRunning
		}
		c.held = append(c.held, p...)
		c.fitting = false // a key: run sends the focus after the held input
		c.mu.Unlock()
		c.retry(false)
		return len(p), nil
	}
	s := c.stream
	focus := c.fitting && c.focused
	c.fitting = false
	c.mu.Unlock()
	if focus { // the owner types: the focus held back by the fit goes now
		_ = s.Focus(true)
	}
	return s.Write(p)
}

// isSchemeReport tells if p is exactly the color scheme report that the
// plugin writes when the theme turns dark or light (CSI ? 997 ; 1 n or 2 n).
// It is no input from the owner.
func isSchemeReport(p []byte) bool {
	s := string(p)
	return s == "\x1b[?997;1n" || s == "\x1b[?997;2n"
}

// sameColors tells if the colors (not the cell size) of a and of the attach
// display b are equal. A nil b has no colors.
func sameColors(a hc.Display, b *hc.Display) bool {
	var x hc.Display
	if b != nil {
		x = *b
	}
	eq := func(p, q *string) bool { return (p == nil) == (q == nil) && (p == nil || *p == *q) }
	return eq(a.FG, x.FG) && eq(a.BG, x.BG) && eq(a.Cursor, x.Cursor)
}

// hex returns col as "#rrggbb". A == 0 means "not set": a fully transparent
// color sends nothing, and the result is nil.
func hex(col ui.Color) *string {
	if col.A == 0 {
		return nil
	}
	s := fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B)
	return &s
}

// UseTheme keeps the colors of the view's theme (the terminal plugin calls it
// at the first build and on each appearance change). The next attach sends
// them with the display. While the stream has input, hc gets them at once and
// answers the program's color queries with them. An observer stream cannot
// set colors: hc ignores them (D91), so they wait for the next attach with
// input (D213).
func (c *Conn) UseTheme(t *gterm.Theme) error {
	if t == nil {
		return nil
	}
	d := hc.Display{FG: hex(t.Foreground), BG: hex(t.Background), Cursor: hex(t.Cursor)}
	c.mu.Lock()
	c.colors = d
	s, send := c.stream, c.stream != nil && !c.watching
	if c.cell[0] > 0 && c.cell[1] > 0 {
		cell := c.cell
		d.Cell = &cell
	}
	c.mu.Unlock()
	if !send {
		return nil
	}
	return s.SetDisplay(d)
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
	c.sized = true
	s, tried, watching := c.stream, c.tried, c.watching
	c.mu.Unlock()
	if s == nil {
		if !tried { // the first size starts the first attach; later ones wait for the backoff
			c.retry(false)
		}
		return nil
	}
	if watching {
		c.fitIfNeeded(s)
		return nil
	}
	if !changed {
		return nil
	}
	if err := s.Resize(uint16(cols), uint16(rows)); err != nil {
		return err
	}
	// The core sends no resize frame to the viewer that resized (D43).
	c.setSize(uint16(cols), uint16(rows))
	return nil
}

// fitIfNeeded ends the observer stream s when the core's size differs from
// the view's, so that run attaches with input at the view's size (D199). It
// sends no input; the focus waits for the first key. It waits for the
// view's first size.
func (c *Conn) fitIfNeeded(s Stream) {
	c.mu.Lock()
	if !c.watching || c.stream != s || c.core == nil || !c.sized ||
		(c.core.Cols == c.cols && c.core.Rows == c.rows) {
		c.mu.Unlock()
		return
	}
	c.watching, c.live, c.takeover, c.fitting = false, false, true, true
	c.mu.Unlock()
	_ = s.Close() // run attaches again once the pump ends
}

// setSize marks a size this viewer gave the core, for Read to give the
// screen before the output that follows. It never waits for Read.
func (c *Conn) setSize(cols, rows uint16) {
	c.mu.Lock()
	c.pending = &hc.Size{Cols: cols, Rows: rows}
	c.newest = c.nextSize()
	c.mu.Unlock()
	c.poke()
}

// nextSize gives the seq of a size Conn learns now: a resize frame when the
// pump reads it, or a size this viewer sets. A size frame can reach Read after
// a size that Conn learned later; the seq lets Read drop it (D199, #486).
// c.mu must be held.
func (c *Conn) nextSize() uint64 {
	c.sizes++
	return c.sizes
}

// poke wakes Read to give the screen the pending size.
func (c *Conn) poke() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// resizeScreen gives the screen the newest size: size, with its seq, when it
// is newer than the pending size and the screen's size, else the pending size.
// An older size is dropped, so a frame of a closed observer stream never comes
// after the fit size. A nil size gives only the pending size. Without a screen
// yet, the newest size waits as the pending one. Read calls it.
func (c *Conn) resizeScreen(size *hc.Size, seq uint64) {
	c.mu.Lock()
	if size != nil && seq > c.newest {
		c.pending, c.newest = size, seq
	}
	scr := c.screen
	size = c.pending
	if scr == nil || size == nil {
		c.mu.Unlock()
		return
	}
	c.pending = nil
	c.mu.Unlock()
	scr.Resize(int(size.Cols), int(size.Rows))
}

// deliver gives ch to Read, unless Conn closes first.
func (c *Conn) deliver(ch chunk) bool {
	select {
	case c.out <- ch:
		return true
	case <-c.ctx.Done():
		return false
	}
}

// SetFocus tells the core whether the window has the focus. It is sent again
// after an attach.
func (c *Conn) SetFocus(focused bool) error {
	c.mu.Lock()
	c.focused = focused
	s, watching := c.stream, c.watching || c.fitting
	c.mu.Unlock()
	if s == nil || watching {
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
	p := hc.AttachParams{Cols: c.cols, Rows: c.rows, History: HistoryLines, Resumed: c.resumed,
		Observe: c.watching, ResizeNotifications: true}
	d := c.colors
	if c.cell[0] > 0 && c.cell[1] > 0 {
		cell := c.cell
		d.Cell = &cell
	}
	if d != (hc.Display{}) {
		p.Display = &d
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
	if p.Observe && !c.watching { // a key came during the attach: attach with input
		c.attaching = false
		c.mu.Unlock()
		_ = s.Close()
		c.retry(false)
		return
	}
	c.stream, c.attaching, c.resumed, c.noRetry = s, false, true, false
	fitting := c.fitting
	cols, rows, focused := c.cols, c.rows, c.focused && !fitting
	// UseTheme may have run during the attach, when it had no stream to tell.
	var recolor *hc.Display
	if !p.Observe && !sameColors(c.colors, p.Display) {
		d := c.colors
		if c.cell[0] > 0 && c.cell[1] > 0 {
			cell := c.cell
			d.Cell = &cell
		}
		recolor = &d
	}
	c.mu.Unlock()
	if recolor != nil {
		_ = s.SetDisplay(*recolor)
	}
	if !p.Observe {
		if cols != p.Cols || rows != p.Rows { // the view's size changed during the attach
			_ = s.Resize(cols, rows)
		}
		if focused {
			_ = s.Focus(true)
		}
		// This viewer set the size, and gets no resize frame for it (D43).
		c.setSize(cols, rows)
	}
	c.flushHeld(s)
	c.mu.Lock()
	late := fitting && !c.fitting && c.focused && c.stream == s // a key came during the fit
	c.mu.Unlock()
	if late {
		_ = s.Focus(true)
	}
	c.setState(State{Kind: Live})
	c.pump(s)

	c.mu.Lock()
	if c.stream == s {
		c.stream = nil
	}
	c.live = false
	stopped, takeover := c.stopped, c.takeover
	c.takeover = false
	c.mu.Unlock()
	if stopped {
		_ = s.Close()
		return
	}
	if takeover { // Write closed the observer stream: attach with input now
		c.retry(false)
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
			var seq uint64
			if ev.Resize != nil {
				seq = c.nextSize()
			}
			c.mu.Unlock()
			if ev.Resize != nil {
				if !c.deliver(chunk{size: ev.Resize, seq: seq}) {
					return
				}
				c.mu.Lock()
				if c.watching {
					sz := *ev.Resize
					c.core = &sz
				}
				c.mu.Unlock()
				c.fitIfNeeded(s)
			}
			if len(ev.Data) > 0 && !c.deliver(chunk{data: ev.Data}) {
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
