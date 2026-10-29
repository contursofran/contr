package terminal

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/herdr"
	"github.com/contursofran/contr/internal/herdr/endpoint"
)

const notRunning = "gone"

var (
	stateNotRunning = endpoint.LinkState{Kind: endpoint.LinkNotRunning, Message: notRunning}
	stateConnecting = endpoint.LinkState{Kind: endpoint.LinkConnecting}
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
		state               endpoint.LinkState
		want                line
	}{
		{"surface speaks, enabled", true, true, stateNotRunning, line{}},
		{"surface speaks, off", false, true, stateNotRunning, line{}},
		{"link off", false, false, stateNotRunning, line{OffLine, theme.TextDim, true}},
		{"connecting", true, false, stateConnecting, line{}},
		{"live", true, false, liveState, line{}},
		{"not running", true, false, stateNotRunning, line{herdr.NotRunningLine, theme.Text2, true}},
		{"incompatible", true, false, endpoint.LinkState{Kind: endpoint.LinkIncompatible, Message: "update contr"}, line{"update contr", theme.Text2, true}},
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

func TestASurfaceNotifiesOncePerRevision(t *testing.T) {
	v, _, n := newTestView(t)
	shown := surface(1, "a")
	v.acceptSurface(shown)
	if *n != 1 {
		t.Fatalf("invalidations = %d, want 1", *n)
	}
	v.acceptSurface(shown)
	if *n != 1 {
		t.Errorf("the same surface is shown again: %d", *n)
	}
	v.acceptSurface(surface(1, "a"))
	if *n != 1 {
		t.Errorf("an equal revision is shown again: %d", *n)
	}
	v.acceptSurface(surface(2, "x"))
	if *n != 2 {
		t.Errorf("a new revision: %d, want 2", *n)
	}
}

func TestAStateChangeNotifiesOnceAndALostLinkDropsTheSurface(t *testing.T) {
	v, tt, n := newTestView(t)
	v.acceptSurface(surface(1, "a"))
	base := *n

	// Connecting is the state it already has.
	v.onUpdate(endpoint.StateUpdate{State: stateConnecting})
	if *n != base {
		t.Errorf("same state invalidated: %d", *n-base)
	}

	v.onUpdate(endpoint.StateUpdate{State: stateNotRunning})
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

	v.onUpdate(endpoint.StateUpdate{State: stateNotRunning})
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
func gridAt(t *testing.T, v *View, w, h float32) endpoint.GridSize {
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
	v.onUpdate(endpoint.StateUpdate{State: liveState})
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
	v.onUpdate(endpoint.StateUpdate{State: liveState})

	want := []endpoint.ClientMsg{
		endpoint.FocusMsg{Focused: v.windowActive},
		endpoint.ResizeMsg{Size: v.sent.size, Cell: v.sent.cell},
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
	return func(endpoint.GridSize, endpoint.CellPx, func(endpoint.LinkUpdate)) (Link, func(), error) {
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
	if v.state.Kind != endpoint.LinkNotRunning {
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
	v.opts.Start = func(endpoint.GridSize, endpoint.CellPx, func(endpoint.LinkUpdate)) (Link, func(), error) {
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
	if got, want := f.queued(), []endpoint.ClientMsg{endpoint.FocusMsg{Focused: false}}; !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
	v.SetWindowActive(true)
	if got, want := f.queued(), []endpoint.ClientMsg{endpoint.FocusMsg{Focused: true}}; !reflect.DeepEqual(got, want) {
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
	v.onUpdate(endpoint.StateUpdate{State: stateNotRunning})
	if *n != 0 || v.state != stateConnecting {
		t.Errorf("a closed view applied an update: %d, %+v", *n, v.state)
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

	v.notify(endpoint.SurfaceUpdate{})
	if v.surface != nil || len(posted) != 1 {
		t.Fatal("the update was not posted")
	}
	posted[0]()
	if v.surface == nil || invalidations != 1 {
		t.Errorf("surface = %v, invalidations = %d", v.surface, invalidations)
	}

	pane := "p9"
	v.onUpdate(endpoint.SnapshotUpdate{Snapshot: &endpoint.ShellSnapshot{FocusedPaneID: &pane}})
	if got := v.focusedPane(); got != "p9" {
		t.Errorf("focusedPane = %q, want the shell's p9", got)
	}
	v.onUpdate(endpoint.StateUpdate{State: stateNotRunning})
	if got := v.focusedPane(); got != "" {
		t.Errorf("a lost link keeps the shell pane %q", got)
	}
}

func TestSendToQueuesOnTheLinkAndReportsARefusal(t *testing.T) {
	v, tt, _ := newTestView(t)
	events := []endpoint.InputEvent{endpoint.TextEvent{Text: "a"}}
	if v.sendTo("p1", events) {
		t.Error("sent with no link")
	}
	f := attach(v, tt)
	if !v.sendTo("p1", events) {
		t.Fatal("not sent")
	}
	if got, want := f.queued(), []endpoint.ClientMsg{endpoint.InputMsg{PaneID: "p1", Events: events}}; !reflect.DeepEqual(got, want) {
		t.Errorf("queued %v, want %v", got, want)
	}
	f.refuse = endpoint.ErrLinkNotConnected
	if v.sendTo("p1", events) {
		t.Error("a refused frame counts as sent")
	}
}
