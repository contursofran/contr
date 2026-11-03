package terminal

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/term"
)

const notRunning = "gone"

var (
	stateNotRunning = term.LinkState{Kind: term.LinkNotRunning, Message: notRunning}
	stateConnecting = term.LinkState{Kind: term.LinkConnecting}
)

func TestTheStatusLineFollowsTheLinkAndTheSurface(t *testing.T) {
	type line struct {
		text   string
		colour uint32
		ok     bool
	}
	tests := []struct {
		name                string
		enabled, hasSurface bool
		state               term.LinkState
		want                line
	}{
		{"surface speaks, enabled", true, true, stateNotRunning, line{}},
		{"surface speaks, off", false, true, stateNotRunning, line{}},
		{"link off", false, false, stateNotRunning, line{OffLine, theme.TextDim, true}},
		{"connecting", true, false, stateConnecting, line{}},
		{"live", true, false, liveState, line{}},
		{"not running", true, false, stateNotRunning, line{herdr.NotRunningLine, theme.Text2, true}},
		{"incompatible", true, false, term.LinkState{Kind: term.LinkIncompatible, Message: "update contr"}, line{"update contr", theme.Text2, true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, colour, ok := statusLine(tc.enabled, tc.hasSurface, tc.state)
			if got := (line{text, colour, ok}); got != tc.want {
				t.Errorf("statusLine = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestASurfaceIsStaleOnlyWhileTheLinkReconnects(t *testing.T) {
	if !isStale(true, stateConnecting) {
		t.Error("a surface is stale while the link connects")
	}
	if isStale(false, stateConnecting) {
		t.Error("no surface is never stale")
	}
	if isStale(true, liveState) || isStale(true, stateNotRunning) {
		t.Error("a live or lost link does not make the surface stale")
	}
}

func TestASizeIsSentAgainOnlyASecondAfterTheLastOne(t *testing.T) {
	t0 := time.Unix(1000, 0)
	if !reassertDue(time.Time{}, t0) {
		t.Error("none sent before: due")
	}
	if reassertDue(t0, t0.Add(999*time.Millisecond)) {
		t.Error("999 ms after: not due")
	}
	if !reassertDue(t0, t0.Add(time.Second)) {
		t.Error("1 s after: due")
	}
}

func TestTheGridFillsTheViewAndTheSurfaceReplacesTheLine(t *testing.T) {
	v, tt, _ := newTestView(t)
	tt.Frame()
	if !tt.HasText(OffLine) {
		t.Fatalf("no line before a surface: %q", tt.Texts())
	}
	v.acceptSurface(surface(1, "a"))
	tt.Frame()
	grid, ok := tt.Find(LabelGrid)
	if !ok {
		t.Fatal("no grid")
	}
	if grid.W != 800 || grid.H != 600 {
		t.Errorf("grid is %vx%v, want 800x600", grid.W, grid.H)
	}
	if tt.HasText(OffLine) {
		t.Error("the line stays over the surface")
	}
}

func TestASurfaceWithBoxGlyphsRenders(t *testing.T) {
	v, tt, _ := newTestView(t)
	v.acceptSurface(surface(1, "│"))
	tt.Frame()
	if _, ok := tt.Find(LabelGrid); !ok {
		t.Fatal("no grid")
	}
	// The grid is set only when the layout had metrics, and the paint needs
	// them too.
	if !v.hasWant {
		t.Error("the layout set no grid")
	}
}

func TestASurfaceWithEveryUnderlineStyleAndItalicRenders(t *testing.T) {
	v, tt, _ := newTestView(t)
	styles := []uint16{
		0,
		Underline,
		Underline | 0x2000,
		Underline | 0x3000,
		Underline | 0x4000,
		Underline | 0x5000,
		Italic,
	}
	v.acceptSurface(styled(1, styles))
	tt.Frame()
	if _, ok := tt.Find(LabelGrid); !ok {
		t.Fatal("no grid")
	}
	if !v.hasWant {
		t.Error("the layout set no grid")
	}
}

func TestTheLineShowsWhileTheLinkIsOff(t *testing.T) {
	_, tt, _ := newTestView(t)
	tt.Frame()
	if !tt.HasText(OffLine) {
		t.Errorf("no line: %q", tt.Texts())
	}
	if _, ok := tt.Find(LabelGrid); !ok {
		t.Error("no grid")
	}
}

func TestASurfaceIsKeptPerRevision(t *testing.T) {
	v, _, _ := newTestView(t)
	shown := surface(1, "a")
	v.acceptSurface(shown)
	v.acceptSurface(surface(1, "a"))
	if v.surface != shown {
		t.Error("an equal revision replaced the surface")
	}
	next := surface(2, "x")
	v.acceptSurface(next)
	if v.surface != next {
		t.Error("a new revision did not replace the surface")
	}
}

// TestScreenUpdatesAreTakenOnceAFrame checks that updates that come between
// two frames ask for one frame each, and that the frame takes the screen from
// the link once.
func TestScreenUpdatesAreTakenOnceAFrame(t *testing.T) {
	v, tt, n := newTestView(t)
	tt.Frame()
	f := &countingLink{fakeLink: fakeLink{latest: surface(1, "a")}}
	v.link = f
	base := *n
	for range 3 {
		v.onUpdate(term.ScreenUpdate{})
	}
	if f.takes != 0 || *n-base != 3 {
		t.Fatalf("before the frame: takes = %d, invalidations = %d; want 0 and 3", f.takes, *n-base)
	}
	tt.Frame()
	if f.takes != 1 || v.surface == nil {
		t.Fatalf("after the frame: takes = %d, surface = %v; want 1 take and a surface", f.takes, v.surface)
	}
	tt.Frame()
	if f.takes != 1 {
		t.Errorf("a frame with no update took a screen: takes = %d", f.takes)
	}
}

type countingLink struct {
	fakeLink
	takes int
}

func (c *countingLink) TakeLatest() *term.Screen {
	c.takes++
	return c.fakeLink.TakeLatest()
}

func TestAStateChangeNotifiesOnceAndALostLinkDropsTheSurface(t *testing.T) {
	v, tt, n := newTestView(t)
	v.acceptSurface(surface(1, "a"))
	base := *n

	// Connecting is the state it already has.
	v.onUpdate(term.StateUpdate{State: stateConnecting})
	if *n != base {
		t.Errorf("same state invalidated: %d", *n-base)
	}

	v.onUpdate(term.StateUpdate{State: stateNotRunning})
	if *n != base+1 {
		t.Errorf("a lost link invalidated %d times, want 1", *n-base)
	}
	if v.surface != nil {
		t.Error("a lost link keeps the surface")
	}
	tt.Frame()
	if !tt.HasText(OffLine) {
		t.Errorf("no line after the surface went: %q", tt.Texts())
	}

	v.onUpdate(term.StateUpdate{State: stateNotRunning})
	if *n != base+1 {
		t.Errorf("the same lost link invalidated again: %d", *n-base)
	}
}

func TestTwoSizeChangesWithinTheWaitSendOneResize(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	before := append([]resizeSent(nil), v.sends...)

	tt.SetSize(900, 600)
	tt.Frame()
	tt.SetSize(1000, 600)
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	if len(v.sends) != len(before) {
		t.Fatalf("sent inside the wait: %d, had %d", len(v.sends), len(before))
	}

	tt.Advance(100 * time.Millisecond)
	if len(v.sends) != len(before)+1 {
		t.Fatalf("sends = %d, want %d", len(v.sends), len(before)+1)
	}
	// The one send is the last size, 1000 px wide, not the 900 px between.
	last, held := v.sends[len(v.sends)-1].size, before[len(before)-1].size
	if last.Cols <= held.Cols {
		t.Errorf("sent %v after %v", last, held)
	}
	wide := gridAt(t, v, 1000, 600)
	if last != wide {
		t.Errorf("sent %v, want the 1000 px grid %v", last, wide)
	}
}

// gridAt is the grid that a w x h view gets.
func gridAt(t *testing.T, v *View, w, h float32) term.GridSize {
	t.Helper()
	size, ok := GridFor(w, h, v.metrics)
	if !ok {
		t.Fatalf("no grid for %vx%v", w, h)
	}
	return size
}

func TestASizeThatReturnsWithinTheWaitSendsNothingMore(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	before := len(v.sends)

	tt.SetSize(900, 600)
	tt.Frame()
	tt.SetSize(800, 600)
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	if len(v.sends) != before {
		t.Errorf("sends = %d, want %d", len(v.sends), before)
	}
}

func TestALiveLinkGetsTheWantedSizeAgain(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	before := append([]resizeSent(nil), v.sends...)
	v.onUpdate(term.StateUpdate{State: liveState})
	if len(v.sends) != len(before)+1 {
		t.Fatalf("sends = %d, want %d", len(v.sends), len(before)+1)
	}
	if v.sends[len(v.sends)-1] != before[len(before)-1] {
		t.Errorf("sent %v, want %v", v.sends[len(v.sends)-1], before[len(before)-1])
	}
}

func TestALiveLinkIsToldTheFocusStateAndTheSize(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	f := &fakeLink{}
	v.link = f
	v.onUpdate(term.StateUpdate{State: liveState})

	want := []ClientMsg{
		FocusMsg{Focused: v.windowActive},
		ResizeMsg{Size: v.sent.size, Cell: v.sent.cell},
	}
	if got := f.queued(); !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
}

func TestAViewTooSmallForAGridSendsNoSize(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	before := len(v.sends)
	settle(tt, 20, 20)
	if len(v.sends) != before {
		t.Errorf("sends = %d, want %d", len(v.sends), before)
	}
}

// failingStart is a starter that fails the way a link that cannot start does.
func failingStart(starts *int) Starter {
	return func(term.GridSize, term.CellPx, func(term.LinkUpdate)) (Link, func(), error) {
		*starts++
		return nil, nil, errors.New("no thread")
	}
}

func TestAFailedStartIsTriedAgainOnAClick(t *testing.T) {
	starts := 0
	v, tt, n := newTestViewWith(t, Options{Enabled: true, Start: failingStart(&starts)})
	settle(tt, 800, 600)
	if v.link != nil {
		t.Fatal("a failed start left a link")
	}
	if v.state.Kind != term.LinkNotRunning {
		t.Fatalf("state = %+v, want not running", v.state)
	}
	base, startsBefore := *n, starts

	// The start fails again: the view keeps its state and does not notify.
	tt.ClickAt(400, 300)
	if starts != startsBefore+1 {
		t.Errorf("a click made %d starts, want 1", starts-startsBefore)
	}
	if v.link != nil {
		t.Error("a failed start left a link")
	}
	if *n != base {
		t.Errorf("a failed start again invalidated %d times", *n-base)
	}

	// When the start works, the next click starts the link.
	f := &fakeLink{}
	stopped := 0
	v.opts.Start = func(term.GridSize, term.CellPx, func(term.LinkUpdate)) (Link, func(), error) {
		return f, func() { stopped++ }, nil
	}
	tt.ClickAt(400, 300)
	if v.link != Link(f) {
		t.Fatal("the click did not start the link")
	}
	v.Close()
	v.Close()
	if stopped != 1 || v.link != nil {
		t.Errorf("Close stopped the link %d times, link = %v", stopped, v.link)
	}
}

func TestAFailedStartIsNotRetriedOnATimer(t *testing.T) {
	starts := 0
	v, tt, _ := newTestViewWith(t, Options{Enabled: true, Start: failingStart(&starts)})
	settle(tt, 800, 600)
	if starts != 1 {
		t.Fatalf("the layout started the link %d times, want 1", starts)
	}
	// Time passes with no user event: the start is not tried again.
	tt.Advance(10 * time.Second)
	if starts != 1 {
		t.Errorf("a timer started the link again: %d", starts)
	}
	if v.link != nil {
		t.Error("a failed start left a link")
	}
}

func TestTheWindowFocusStateGoesToHerdr(t *testing.T) {
	v, tt, _ := newTestView(t)
	settle(tt, 800, 600)
	// The view starts inactive; the app sets the real state.
	v.SetWindowActive(true)
	f := attach(v, tt)
	if !v.windowActive {
		t.Fatal("the view is not active")
	}

	v.SetWindowActive(true)
	if got := f.queued(); len(got) != 0 {
		t.Errorf("the same state queued %v", got)
	}
	v.SetWindowActive(false)
	if got, want := f.queued(), []ClientMsg{FocusMsg{Focused: false}}; !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
	v.SetWindowActive(true)
	if got, want := f.queued(), []ClientMsg{FocusMsg{Focused: true}}; !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
}

func TestTheTerminalHasTheKeyboardOnOpenAndAClickTakesItBack(t *testing.T) {
	v := NewView(Options{})
	t.Cleanup(v.Close)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Grow(1).Children(func() { v.Build(c) })
			ui.Button(c, "other")
		})
	}, 800, 600)
	tt.Frame()
	if !tt.Focused(LabelGrid) {
		t.Fatal("the terminal has no keyboard on open")
	}
	if err := tt.Click("other"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused(LabelGrid) {
		t.Fatal("the other control did not take the keyboard")
	}
	x, y := cellPoint(v, tt, 2, 2, 0.5)
	tt.ClickAt(x, y)
	if !tt.Focused(LabelGrid) {
		t.Error("a click did not give the keyboard back")
	}
}

func TestClosedViewDropsLateUpdates(t *testing.T) {
	v, _, n := newTestView(t)
	v.Close()
	v.onUpdate(term.StateUpdate{State: stateNotRunning})
	if *n != 0 || v.state != stateConnecting {
		t.Errorf("a closed view applied an update: %d, %+v", *n, v.state)
	}
}

func TestAClosedViewStartsNoLink(t *testing.T) {
	starts := 0
	v, tt, _ := newTestViewWith(t, Options{Enabled: true, Start: failingStart(&starts)})
	settle(tt, 800, 600)
	v.Close()
	before := starts
	tt.ClickAt(400, 300)
	v.ensureLink()
	if starts != before {
		t.Errorf("a closed view made %d starts", starts-before)
	}
}

func TestTheLinkUpdatesReachTheViewThroughPost(t *testing.T) {
	var posted []func()
	invalidations := 0
	v := NewView(Options{
		Post:       func(fn func()) { posted = append(posted, fn) },
		Invalidate: func() { invalidations++ },
	})
	f := &fakeLink{latest: surface(1, "a")}
	v.link = f

	v.notify(v.gen, term.ScreenUpdate{})
	if v.surface != nil || len(posted) != 1 {
		t.Fatal("the update was not posted")
	}
	posted[0]()
	if !v.screenWaits || invalidations != 1 {
		t.Errorf("screenWaits = %v, invalidations = %d", v.screenWaits, invalidations)
	}
	v.takeScreen() // the next Build
	if v.surface == nil || invalidations != 1 {
		t.Errorf("surface = %v, invalidations = %d", v.surface, invalidations)
	}

	pane := "p9"
	v.onUpdate(term.SnapshotUpdate{Snapshot: &term.Snapshot{FocusedPaneID: &pane}})
	if got := v.focusedPane(); got != "p9" {
		t.Errorf("focusedPane = %q, want the shell's p9", got)
	}
	v.onUpdate(term.StateUpdate{State: stateNotRunning})
	if got := v.focusedPane(); got != "" {
		t.Errorf("a lost link keeps the shell pane %q", got)
	}
}

func TestSendToQueuesOnTheLinkAndReportsARefusal(t *testing.T) {
	v, tt, _ := newTestView(t)
	events := []term.InputEvent{term.TextEvent{Text: "a"}}
	if v.sendTo("p1", events) {
		t.Error("sent with no link")
	}
	f := attach(v, tt)
	if !v.sendTo("p1", events) {
		t.Fatal("not sent")
	}
	if got, want := f.queued(), []ClientMsg{InputMsg{PaneID: "p1", Events: events}}; !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
	f.refuse = term.ErrNotConnected
	if v.sendTo("p1", events) {
		t.Error("a refused frame counts as sent")
	}
}

// Without a starter from the app, the link does not start: the state is not
// running and no test or build connects to herdr by itself.
func TestNoStarterMeansNotRunning(t *testing.T) {
	v := NewView(Options{Enabled: true})
	_, _, err := v.opts.Start(term.GridSize{Cols: 1, Rows: 1}, term.CellPx{}, nil)
	if err == nil {
		t.Fatal("the default starter started a link")
	}
}

// recordingStart is a starter that records how it was used.
type recordingStart struct {
	links   []*fakeLink
	stops   int
	notifys []func(term.LinkUpdate)
}

func (r *recordingStart) start(term.GridSize, term.CellPx, func(term.LinkUpdate)) (Link, func(), error) {
	l := &fakeLink{}
	r.links = append(r.links, l)
	return l, func() { r.stops++ }, nil
}

func TestSetTerminalSwitchesTheLinkAndDropsTheOldOnes(t *testing.T) {
	a, b := &recordingStart{}, &recordingStart{}
	var notifyA func(term.LinkUpdate)
	startA := func(s term.GridSize, c term.CellPx, n func(term.LinkUpdate)) (Link, func(), error) {
		notifyA = n
		return a.start(s, c, n)
	}
	v, tt, _ := newTestViewWith(t, Options{})
	settle(tt, 800, 600)
	if v.link != nil {
		t.Fatal("a link runs with no terminal picked")
	}
	v.SetTerminal(startA)
	if len(a.links) != 1 || v.link == nil {
		t.Fatalf("picking a terminal started %d links", len(a.links))
	}
	v.acceptSurface(surface(1, "old"))

	v.SetTerminal(b.start)
	if a.stops != 1 || len(b.links) != 1 {
		t.Errorf("old link stopped %d times, new links %d; want 1 and 1", a.stops, len(b.links))
	}
	if v.surface != nil {
		t.Error("the old terminal's screen stays")
	}
	// The old link's late update must not change the new terminal's state.
	notifyA(term.StateUpdate{State: stateNotRunning})
	if v.state.Kind == term.LinkNotRunning {
		t.Error("an update of the old link reached the view")
	}

	v.SetTerminal(nil)
	tt.Frame()
	if b.stops != 1 || v.link != nil || !tt.HasText(OffLine) {
		t.Errorf("no terminal: stops %d, link %v, line shown %v", b.stops, v.link, tt.HasText(OffLine))
	}
}
