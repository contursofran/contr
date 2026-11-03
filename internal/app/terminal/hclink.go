package terminal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/term"
	"github.com/contursofran/contr/internal/term/vt"
)

// HistoryLines is how many lines of scrollback an attach asks for (Q3).
const HistoryLines = 1000

// maxBatch is the most stream events the reader applies before it publishes a
// screen. Under a flood the screen is built once per batch, not per event.
const maxBatch = 64

// HCStream is one attached hc terminal. *hc.Stream is one.
type HCStream interface {
	Events() <-chan hc.StreamEvent
	Done() <-chan struct{}
	Err() error
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Focus(focused bool) error
	Close() error
}

// HCSource attaches to one hc terminal. Attach fills in the terminal address
// of the params. It returns an *hc.Error of kind
// "incompatible" or "needs_framed" when the core cannot do framed attach, and
// "finished" or "not_found" when the terminal is not running. Watch gives the
// events of that terminal; it can be nil.
type HCSource interface {
	Attach(ctx context.Context, p hc.AttachParams) (HCStream, error)
	Watch(ctx context.Context) (events <-chan hc.Event, stop func(), err error)
}

// HCLink is a [Link] to an hc terminal. A goroutine reads the stream into an
// emulator and publishes the newest screen. Updates coalesce: at most one
// [term.ScreenUpdate] waits for the UI (D43).
type HCLink struct {
	src    HCSource
	notify func(term.LinkUpdate)
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	emu    *vt.Terminal

	pending atomic.Bool // a ScreenUpdate was sent and TakeLatest has not run
	rebuild chan struct{}

	mu        sync.Mutex
	stream    HCStream // nil unless live
	attaching bool
	resumed   bool
	noRetry   bool // the last attach failed for good: only an event retries
	stopped   bool
	focused   bool
	size      term.GridSize
	cell      term.CellPx
	offset    int // lines scrolled up into the history
	latest    *term.Screen
	state     term.LinkState
}

// NewHCLink starts a link at size. It never blocks: the attach runs on a
// goroutine, and notify gets the states and screens. Stop ends the link.
func NewHCLink(src HCSource, size term.GridSize, cell term.CellPx, notify func(term.LinkUpdate)) *HCLink {
	ctx, cancel := context.WithCancel(context.Background())
	l := &HCLink{src: src, notify: notify, ctx: ctx, cancel: cancel, size: size, cell: cell, rebuild: make(chan struct{}, 1)}
	l.emu = vt.New(max(int(size.Cols), 1), max(int(size.Rows), 1), l.respond)
	l.emu.OnClipboard(func(text string) { notify(term.ClipboardUpdate{Text: text}) })
	l.setState(term.LinkState{Kind: term.LinkConnecting})
	l.retry(false)
	l.watch()
	return l
}

// HCStarter returns a [Starter] that links to the terminal of src.
func HCStarter(src HCSource) Starter {
	return func(size term.GridSize, cell term.CellPx, notify func(term.LinkUpdate)) (Link, func(), error) {
		l := NewHCLink(src, size, cell, notify)
		return l, l.Stop, nil
	}
}

func (l *HCLink) setState(s term.LinkState) {
	l.mu.Lock()
	same := l.state == s
	l.state = s
	l.mu.Unlock()
	if !same {
		l.notify(term.StateUpdate{State: s})
	}
}

// respond sends the emulator's answers (such as a cursor report) to the
// program.
func (l *HCLink) respond(b []byte) {
	l.mu.Lock()
	s := l.stream
	l.mu.Unlock()
	if s != nil {
		_, _ = s.Write(b)
	}
}

// watch follows the terminal's events: "restarted" attaches again.
func (l *HCLink) watch() {
	events, stop, err := l.src.Watch(l.ctx)
	if err != nil {
		slog.Debug("terminal events not watched", "err", err)
		return
	}
	if events == nil {
		return
	}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		defer stop()
		for {
			select {
			case e, ok := <-events:
				if !ok {
					return
				}
				if e.Method == "event" && e.Type == "restarted" {
					l.retry(true)
				}
			case <-l.ctx.Done():
				return
			}
		}
	}()
}

// retry starts an attach if none runs and none is live. force also tries
// after a failure that is final until the terminal restarts. There is no
// timer: an event, an input or a grid change calls it.
func (l *HCLink) retry(force bool) {
	l.mu.Lock()
	if l.stopped || l.attaching || l.stream != nil || (l.noRetry && !force) {
		l.mu.Unlock()
		return
	}
	l.attaching = true
	resumed, size, cell := l.resumed, l.size, l.cell
	l.mu.Unlock()
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.run(resumed, size, cell)
	}()
}

// run attaches, then reads the stream until it ends.
func (l *HCLink) run(resumed bool, size term.GridSize, cell term.CellPx) {
	p := hc.AttachParams{
		Cols: size.Cols, Rows: size.Rows,
		History: HistoryLines, ResizeNotifications: true, Resumed: resumed,
	}
	if cell.Width > 0 && cell.Height > 0 {
		p.Display = &hc.Display{Cell: &[2]int{int(cell.Width), int(cell.Height)}}
	}
	s, err := l.src.Attach(l.ctx, p)
	if err != nil {
		l.mu.Lock()
		l.attaching = false
		l.mu.Unlock()
		l.attachFailed(err)
		return
	}
	l.mu.Lock()
	if l.stopped {
		l.attaching = false
		l.mu.Unlock()
		_ = s.Close()
		return
	}
	l.stream, l.attaching, l.resumed, l.noRetry = s, false, true, false
	focused, now := l.focused, l.size
	l.mu.Unlock()
	l.setState(term.LinkState{Kind: term.LinkLive})
	if now != size {
		_ = s.Resize(now.Cols, now.Rows) // the grid changed during the attach
	}
	if focused {
		_ = s.Focus(true)
	}
	l.read(s)

	l.mu.Lock()
	if l.stream == s {
		l.stream = nil
	}
	stopped := l.stopped
	l.mu.Unlock()
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
	l.mu.Lock()
	l.noRetry = true // the program ended; "restarted" attaches again
	l.mu.Unlock()
	l.setState(term.LinkState{Kind: term.LinkNotRunning, Message: msg})
}

func (l *HCLink) attachFailed(err error) {
	if l.ctx.Err() != nil {
		return
	}
	switch {
	case hc.IsKind(err, hc.KindIncompatible), hc.IsKind(err, "needs_framed"):
		l.mu.Lock()
		l.noRetry = true
		l.mu.Unlock()
		l.setState(term.LinkState{Kind: term.LinkIncompatible, Message: err.Error()})
	case hc.IsKind(err, "finished"), hc.IsKind(err, "not_found"), hc.IsKind(err, "machine_unknown"):
		l.mu.Lock()
		l.noRetry = true
		l.mu.Unlock()
		l.setState(term.LinkState{Kind: term.LinkNotRunning, Message: err.Error()})
	default: // "unavailable", a lost connection: an input or a grid change tries again
		l.setState(term.LinkState{Kind: term.LinkNotRunning, Message: err.Error()})
	}
}

// read applies the stream to the emulator and publishes screens. It is the
// only goroutine that writes to the emulator.
func (l *HCLink) read(s HCStream) {
	events := s.Events()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				l.publish()
				return
			}
			l.apply(ev)
			for n := 0; n < maxBatch; n++ {
				select {
				case ev, ok = <-events:
					if !ok {
						l.publish()
						return
					}
					l.apply(ev)
					continue
				default:
				}
				break
			}
			l.publish()
		case <-l.rebuild:
			l.publish()
		case <-l.ctx.Done():
			return
		}
	}
}

func (l *HCLink) apply(ev hc.StreamEvent) {
	if ev.Resize != nil {
		if ev.Resize.Cols > 0 && ev.Resize.Rows > 0 {
			l.emu.Resize(int(ev.Resize.Cols), int(ev.Resize.Rows))
		}
		return
	}
	if _, err := l.emu.Write(ev.Data); err != nil {
		slog.Debug("terminal emulator write", "err", err)
	}
}

// publish builds the screen and tells the UI, unless an update waits.
func (l *HCLink) publish() {
	l.mu.Lock()
	off := l.offset
	l.mu.Unlock()
	scr := l.emu.Screen(off)
	if p := scr.FocusedPane(); p != nil && p.Scroll != nil {
		l.mu.Lock()
		l.offset = int(p.Scroll.OffsetFromBottom) // clamped to the history
		l.mu.Unlock()
	}
	l.mu.Lock()
	l.latest = &scr
	l.mu.Unlock()
	if l.pending.CompareAndSwap(false, true) {
		l.notify(term.ScreenUpdate{Screen: &scr})
	}
}

func (l *HCLink) requestRebuild() {
	select {
	case l.rebuild <- struct{}{}:
	default:
	}
}

// TakeLatest returns the newest screen. The next screen sends a new update.
func (l *HCLink) TakeLatest() *term.Screen {
	l.pending.Store(false)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.latest
}

// SendInput encodes the events with the emulator modes and sends them. A
// wheel step over the history moves the view, not the program. Without a live
// stream it starts an attach and returns [term.ErrNotConnected].
func (l *HCLink) SendInput(_ string, events []term.InputEvent) error {
	l.mu.Lock()
	s := l.stream
	l.mu.Unlock()
	if s == nil {
		l.retry(false)
		return term.ErrNotConnected
	}
	modes := l.emu.Modes()
	var out []byte
	scroll, typed := 0, false
	for _, ev := range events {
		if w, ok := ev.(term.WheelEvent); ok {
			b, n := vt.Wheel(w, modes)
			out = append(out, b...)
			scroll += n
			continue
		}
		typed = true
		out = append(out, vt.Encode(ev, modes)...)
	}
	if scroll != 0 || typed {
		l.mu.Lock()
		if typed {
			l.offset = 0 // typing shows the bottom
		} else {
			l.offset = vt.ClampOffset(l.offset, scroll, vt.MaxScrollback)
		}
		l.mu.Unlock()
		l.requestRebuild()
	}
	if len(out) == 0 {
		return nil
	}
	if _, err := s.Write(out); err != nil {
		return fmt.Errorf("terminal input: %w", err)
	}
	return nil
}

// Resize sets the grid. The core owns the size (D43): the new size goes to
// it, and the emulator follows at once.
func (l *HCLink) Resize(size term.GridSize, cell term.CellPx) error {
	if size.Cols == 0 || size.Rows == 0 {
		return nil
	}
	l.mu.Lock()
	changed := l.size != size
	l.size, l.cell = size, cell
	s := l.stream
	l.mu.Unlock()
	if !changed {
		return nil
	}
	l.emu.Resize(int(size.Cols), int(size.Rows))
	l.requestRebuild()
	if s == nil {
		l.retry(false)
		return term.ErrNotConnected
	}
	return s.Resize(size.Cols, size.Rows)
}

// SetFocus tells the core, and the program when it asked for focus reports.
func (l *HCLink) SetFocus(focused bool) error {
	l.mu.Lock()
	l.focused = focused
	s := l.stream
	l.mu.Unlock()
	if s == nil {
		return term.ErrNotConnected
	}
	if err := s.Focus(focused); err != nil {
		return err
	}
	if l.emu.Modes().Focus {
		report := "\x1b[O"
		if focused {
			report = "\x1b[I"
		}
		_, err := s.Write([]byte(report))
		return err
	}
	return nil
}

// Stop ends the link: the reader, the watcher and the stream. It waits for
// them. The terminal keeps running.
func (l *HCLink) Stop() {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.stopped = true
	s := l.stream
	l.mu.Unlock()
	l.cancel()
	if s != nil {
		_ = s.Close()
	}
	l.wg.Wait()
	_ = l.emu.Close()
}
