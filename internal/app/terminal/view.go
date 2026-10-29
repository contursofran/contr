// Adapted from Herdr UI (Apache-2.0); changes: one surface and one pane selection, no scrollback or copy on select; the pointer gesture and its order are adapted; ported to Go.

package terminal

import (
	"log/slog"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

// The terminal view: the focused herdr tab, painted from the surface link,
// typed into and scrolled through it. The link starts only with
// CONTR_SURFACE=1 (Rust D38). Without it the view shows a line and connects
// to nothing.
//
// MyGo is immediate mode, so "notify" is a call to Options.Invalidate. The
// view runs on the UI thread: Build, Draw, input handlers and onUpdate. The
// link goroutine reaches onUpdate through Options.Post.
//
// This file holds the state, the link, the resize wait and the frame. The
// keys, the pointer and the wheel are in view_keys.go, view_ime.go,
// view_pointer.go and view_wheel.go.

const (
	// LabelGrid is the label of the grid box (Rust debug selector
	// "terminal-grid").
	LabelGrid = "terminal grid"
	// OffLine is shown while the link is off.
	OffLine = "terminal off · set CONTR_SURFACE=1"
	// SurfaceEnv is the variable that turns the link on (Rust D38).
	SurfaceEnv = "CONTR_SURFACE"

	// resizeWait is how long a size must hold before it is sent.
	resizeWait = 150 * time.Millisecond
	// reassertWait: after the owner types, a size herdr does not show is sent
	// again at most this often (Rust D12).
	reassertWait = time.Second
	// staleOpacity is the grid's opacity while the link reconnects and the
	// surface on screen may be out of date.
	staleOpacity = 0.5
)

// Link is the part of [endpoint.Handle] that the view uses.
type Link interface {
	SendInput(paneID string, events []endpoint.InputEvent) error
	Resize(size endpoint.GridSize, cell endpoint.CellPx) error
	SetFocus(focused bool) error
	TakeLatest() *endpoint.Surface
}

// Starter starts a link at the given size. It returns the link and a function
// that stops it. notify may run on any goroutine. An error means the link did
// not start (D19).
type Starter func(size endpoint.GridSize, cell endpoint.CellPx, notify func(endpoint.LinkUpdate)) (Link, func(), error)

// SpawnStarter is the app's [Starter]: it starts the surface link to the local
// herdr. It never fails (D19).
func SpawnStarter(size endpoint.GridSize, cell endpoint.CellPx, notify func(endpoint.LinkUpdate)) (Link, func(), error) {
	l, h := endpoint.SpawnLink(endpoint.LinkTarget{}, size, cell, notify)
	return h, l.Stop, nil
}

// Options are what the view needs from the app.
type Options struct {
	// Enabled starts the link: os.Getenv(SurfaceEnv) == "1". Tests leave it
	// false, so no test connects to herdr.
	Enabled bool
	// Start starts the link. Nil means [SpawnStarter].
	Start Starter
	// Post runs fn on the UI thread (mygo.RunOnMain). Nil runs fn at once.
	Post func(fn func())
	// Invalidate asks for a new frame. Nil does nothing.
	Invalidate func()
	// FocusPane asks herdr to focus a pane. It must not block. Nil does
	// nothing.
	FocusPane func(paneID string)
}

// wanted is a grid and the cell size that go with it.
type wanted struct {
	size endpoint.GridSize
	cell endpoint.CellPx
}

// resizeSent is one resize the view sent. Tests read them.
type resizeSent = wanted

// View is the terminal view. Create it with [NewView].
type View struct {
	opts Options

	// link is nil until the link starts. stop ends it.
	link Link
	stop func()
	// closed is set by Close. Updates that arrive afterwards are dropped.
	closed bool

	state   endpoint.LinkState
	surface *endpoint.Surface
	// shellPane is the pane the shell reported as focused. Keys go there when
	// the surface names none.
	shellPane string
	// windowActive: herdr gets this as the focus state.
	windowActive bool

	// want is the grid and cell size the last layout asked for; hasWant is
	// false before the first one.
	want    wanted
	hasWant bool
	// changedAt is when want last changed. A pending size is sent once it has
	// held for resizeWait.
	changedAt time.Time
	// sent is the size herdr was last told, in the hello or a resize.
	sent    wanted
	hasSent bool
	// reassertedAt is when the size was last sent again after typing (Rust
	// D12); zero for never.
	reassertedAt time.Time
	// sends lists every resize the view sent, in order. Tests read it.
	sends []resizeSent

	// metrics are the cell metrics of the last layout; hasMetrics is false
	// when the font gave none. metricsScale is the scale they were taken at.
	metrics      Metrics
	hasMetrics   bool
	metricsScale float32
	metricsKnown bool

	// services come from the last Build. The clipboard needs them.
	services ui.Services
	// hasServices is false before the first Build.
	hasServices bool
	// pendingClip is OSC 52 text that came before the first Build; the next
	// Build writes it.
	pendingClip string
	hasPending  bool
	// now is the time of the last Build or Draw. IME callbacks carry no
	// event time, so they use it.
	now time.Time
	// refocus asks the next Build to give the grid the keyboard.
	refocus bool

	ime       imeClient
	selection *Selection
	wheel     WheelAccum
	cache     Cache
}

// NewView returns a view with no link. The link starts on the first layout
// when o.Enabled is set.
func NewView(o Options) *View {
	if o.Start == nil {
		o.Start = SpawnStarter
	}
	v := &View{opts: o, state: endpoint.LinkState{Kind: endpoint.LinkConnecting}}
	v.ime.view = v
	return v
}

// invalidate asks for a new frame.
func (v *View) invalidate() {
	if v.opts.Invalidate != nil {
		v.opts.Invalidate()
	}
}

// Close stops the link. It is safe to call more than once.
func (v *View) Close() {
	v.closed = true
	if v.stop != nil {
		v.stop()
	}
	v.link, v.stop = nil, nil
}

// cellMetrics returns the metrics of the terminal font: the advance of "m"
// and the terminal line height. ok is false when the font has no advance. The
// result is kept per scale.
func (v *View) cellMetrics(scale float32) (Metrics, bool) {
	if !v.metricsKnown || v.metricsScale != scale {
		w := extent(ui.Shape("m", terminalFont(Style{})))
		v.metricsKnown, v.metricsScale = true, scale
		v.hasMetrics = hasSize(w)
		v.metrics = Metrics{CellW: w, CellH: theme.TerminalLineHeight, Pad: theme.TermPadding}
	}
	return v.metrics, v.hasMetrics
}

// layout is the layout step, run from Draw. r is the grid box and scale the
// display scale. It is idempotent: Draw may run more than once per frame.
// It returns how long to wait before the pending size may be sent (zero when
// none waits); the caller repaints after that time.
func (v *View) layout(r ui.Rect, scale float32, now time.Time) time.Duration {
	m, ok := v.cellMetrics(scale)
	if !ok {
		return 0
	}
	if size, fits := GridFor(r.W, r.H, m); fits {
		v.setGrid(wanted{size, CellPx(m, scale)}, now)
	}
	if !v.hasWant || (v.hasSent && v.sent == v.want) {
		return 0
	}
	if rest := resizeWait - now.Sub(v.changedAt); rest > 0 {
		return rest
	}
	v.flushResize()
	return 0
}

// setGrid records the grid the view wants. The first size is sent at once;
// a later one waits for resizeWait (see layout).
func (v *View) setGrid(want wanted, now time.Time) {
	if v.hasWant && v.want == want {
		return
	}
	v.want, v.hasWant, v.changedAt = want, true, now
	v.ensureLink()
	if !v.hasSent {
		v.sendResize(want)
	}
}

// ensureLink starts the link at the wanted grid if it is not running. It runs
// on a grid change and on each click or key, so a failed start is tried again
// with no timer: layout runs every frame.
func (v *View) ensureLink() {
	if v.link != nil || !v.opts.Enabled || !v.hasWant {
		return
	}
	v.startLink(v.want)
}

// flushResize sends the wanted size if herdr does not have it yet.
func (v *View) flushResize() {
	if v.hasWant && (!v.hasSent || v.sent != v.want) {
		v.sendResize(v.want)
	}
}

// sendResize records want as sent and queues it on the link. Without a live
// link the size is kept for the next hello, and the Live state sends it again.
func (v *View) sendResize(want wanted) {
	v.sent, v.hasSent = want, true
	v.sends = append(v.sends, want)
	if v.link == nil {
		return
	}
	if err := v.link.Resize(want.size, want.cell); err != nil {
		slog.Debug("terminal resize not queued", "err", err)
	}
}

// startLink starts the link at want. The hello carries that size, so it counts
// as sent.
func (v *View) startLink(want wanted) {
	link, stop, err := v.opts.Start(want.size, want.cell, v.notify)
	if err != nil {
		v.setState(endpoint.LinkState{Kind: endpoint.LinkNotRunning, Message: err.Error()})
		return
	}
	v.sent, v.hasSent = want, true
	v.link, v.stop = link, stop
}

// notify receives the link's updates on the link goroutine and moves them to
// the UI thread.
func (v *View) notify(u endpoint.LinkUpdate) {
	run := func() { v.onUpdate(u) }
	if v.opts.Post != nil {
		v.opts.Post(run)
		return
	}
	run()
}

// onUpdate applies one update from the link, on the UI thread.
func (v *View) onUpdate(u endpoint.LinkUpdate) {
	if v.closed {
		return
	}
	switch u := u.(type) {
	case endpoint.StateUpdate:
		v.setState(u.State)
	case endpoint.SurfaceUpdate:
		// The update says a surface is waiting. The newest one is taken from
		// the link.
		if v.link == nil {
			return
		}
		if next := v.link.TakeLatest(); next != nil {
			v.acceptSurface(next)
		}
	case endpoint.SnapshotUpdate:
		v.shellPane = ""
		if u.Snapshot != nil && u.Snapshot.FocusedPaneID != nil {
			v.shellPane = *u.Snapshot.FocusedPaneID
		}
	case endpoint.ClipboardUpdate:
		v.writeClipboard(u.Text)
	}
}

// acceptSurface shows next unless it is the surface already shown. A new
// revision invalidates once. A released selection whose rows the program wrote
// over, or whose pane moved or went, goes with it.
func (v *View) acceptSurface(next *endpoint.Surface) {
	if held := v.surface; held != nil &&
		held.BootID == next.BootID &&
		held.ProjectionRevision == next.ProjectionRevision &&
		held.Revision == next.Revision &&
		held.Size == next.Size {
		return
	}
	v.surface = next
	v.retireSelection()
	v.invalidate()
}

// setState sets the link state. A state that changes invalidates once. A lost
// link drops the surface, and a link that is not running also forgets the
// shell's pane. A live one gets the focus state and the wanted size again: the
// hello may have carried an older size, and herdr may not know the window is
// active.
func (v *View) setState(st endpoint.LinkState) {
	if v.state == st {
		return
	}
	v.state = st
	if st.Kind == endpoint.LinkNotRunning || st.Kind == endpoint.LinkIncompatible {
		v.surface = nil
		v.selection = nil
		v.cache.Reset()
		v.wheel.Clear()
	}
	if st.Kind == endpoint.LinkNotRunning {
		v.shellPane = ""
	}
	if st.Kind == endpoint.LinkLive {
		v.sendFocus()
		if v.hasWant {
			v.sendResize(v.want)
		}
	}
	v.invalidate()
}

// SetWindowActive tells the view that the window gained or lost focus. herdr
// gets the new focus state, and the cursor is painted again. Call it on the UI
// thread.
func (v *View) SetWindowActive(active bool) {
	if v.windowActive == active {
		return
	}
	v.windowActive = active
	v.sendFocus()
	v.invalidate()
}

// sendFocus queues the window's focus state on the link.
func (v *View) sendFocus() {
	if v.link == nil {
		return
	}
	if err := v.link.SetFocus(v.windowActive); err != nil {
		slog.Debug("terminal focus not queued", "err", err)
	}
}

// focusedPane is the pane keys go to: the surface's focused pane, else the
// pane the shell reported. It is "" when there is none.
func (v *View) focusedPane() string {
	if v.surface != nil {
		if p := v.surface.FocusedPane(); p != nil {
			return p.PaneID
		}
	}
	return v.shellPane
}

// sendTo queues events for pane on the link. It is false when they were not
// queued: no link, or the link refused them.
func (v *View) sendTo(pane string, events []endpoint.InputEvent) bool {
	if v.link == nil {
		return false
	}
	if err := v.link.SendInput(pane, events); err != nil {
		slog.Debug("terminal input not queued", "err", err)
		return false
	}
	return true
}

// statusLine is the line the view shows instead of the grid, with its colour.
// ok is false when the grid speaks for itself. With the link off the line says
// so; with the link on it says what the link reports.
func statusLine(enabled, hasSurface bool, st endpoint.LinkState) (line string, colour uint32, ok bool) {
	if hasSurface {
		return "", 0, false
	}
	if !enabled {
		return OffLine, theme.TextDim, true
	}
	switch st.Kind {
	case endpoint.LinkNotRunning:
		return herdr.NotRunningLine, theme.Text2, true
	case endpoint.LinkIncompatible:
		return st.Message, theme.Text2, true
	}
	return "", 0, false
}

// isStale is true while the view shows a surface that the link no longer
// confirms: the link is reconnecting.
func isStale(hasSurface bool, st endpoint.LinkState) bool {
	return hasSurface && st.Kind == endpoint.LinkConnecting
}

// reassertDue reports whether a size may be sent again now: none was sent
// before (last is the zero time), or the last one was reassertWait or more ago.
func reassertDue(last, now time.Time) bool {
	return last.IsZero() || now.Sub(last) >= reassertWait
}

// Build builds the view. It is the frame's terminal slot.
func (v *View) Build(c *ui.Context) {
	v.services, v.hasServices, v.now = c.Services(), true, c.Now()
	if v.hasPending {
		v.services.WriteClipboard(v.pendingClip)
		v.pendingClip, v.hasPending = "", false
	}
	stale := isStale(v.surface != nil, v.state)
	line, colour, showLine := statusLine(v.opts.Enabled, v.surface != nil, v.state)
	ui.Box(c).Fill().Center().Children(func() {
		grid := ui.Box(c).Label(LabelGrid).Absolute().Fill().
			Background(theme.Color(theme.BgTerminal)).
			Clip().FocusRing(false).AutoFocus().
			HandleInput(v.input).HandleTextInput(&v.ime)
		if stale {
			grid.Opacity(staleOpacity)
		}
		if v.refocus {
			v.refocus = false
			grid.Focus()
		}
		grid.Draw(func(p *ui.Painter, r ui.Rect) { v.draw(p, r, grid.Focused()) })
		if showLine {
			ui.Text(c, line).TextColor(theme.Color(colour))
		}
	})
}

// draw runs the layout and paints the surface.
func (v *View) draw(p *ui.Painter, r ui.Rect, focused bool) {
	v.now = p.Now()
	if rest := v.layout(r, p.Scale(), p.Now()); rest > 0 {
		p.After(rest)
	}
	if v.surface == nil || !v.hasMetrics {
		return
	}
	m := v.metrics
	origin := ui.Point{X: r.X + m.Pad, Y: r.Y + m.Pad}
	typing := Typing{Focused: focused, Marked: v.ime.marked}
	Paint(p, origin, m, v.surface, v.highlights(), typing, &v.cache)
}

// input is the one dispatch for the grid's input.
func (v *View) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		if ev.Mods == ui.Super {
			switch ev.Key {
			case ui.KeyC:
				return v.copySelection()
			case ui.KeyV:
				v.ensureLink()
				v.paste()
				return true
			}
		}
		v.ensureLink()
		return v.onKey(ev)
	case ui.InputCommand:
		switch ev.Text {
		case "copy":
			v.copySelection()
		case "paste":
			v.ensureLink()
			v.paste()
		case "selectAll":
			// The terminal only selects with the pointer (Rust keeps the
			// action enabled and does nothing).
		default:
			return false
		}
		return true
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		// The frame that follows the press gives the grid the keyboard.
		v.refocus = true
		v.ensureLink()
		v.onPointer(ev)
		return true
	case ui.InputPointerMove, ui.InputPointerUp:
		return v.onPointer(ev)
	case ui.InputScroll:
		return v.onScroll(ev)
	}
	return false
}
