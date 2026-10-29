package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	readerTestPong = `{"id":"contr-ping","result":{"type":"pong","version":"0.9.3","protocol":22}}`
	readerTestAck  = `{"id":"contr-sub","result":{"type":"subscription_started"}}`
	readerTestGone = `{"id":"contr-sub","error":{"code":"pane_not_found","message":"pane w1:p1 not found"}}`
	readerTestEvt  = `{"event":"pane.agent_status_changed","data":{"type":"pane.agent_status_changed","agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"}}`

	readerTestEmpty = `{"version":"0.9.3","protocol":22,"workspaces":[],"tabs":[],"panes":[]}`
)

// readerTestSnapshotOf is a session.snapshot object with one workspace, tab
// and pane, running claude.
func readerTestSnapshotOf(pane, status string) string {
	return fmt.Sprintf(`{"version":"0.9.3","protocol":22,"workspaces":[{"workspace_id":"w1","label":"contr","agent_status":"%[2]s"}],"tabs":[{"tab_id":"w1:t1","workspace_id":"w1","label":"planner","agent_status":"%[2]s"}],"panes":[{"pane_id":"%[1]s","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","agent_status":"%[2]s"}]}`, pane, status)
}

func readerTestSnapshot(status string) string { return readerTestSnapshotOf("w1:p1", status) }

// readerTestReply is the reply line to a request that carries snapshot.
func readerTestReply(snapshot string) string {
	return `{"id":"contr-snapshot","result":{"type":"session_snapshot","snapshot":` + snapshot + `}}`
}

// readerTestAnswer is what the fake herdr sends on one connection: lines,
// then the hold before close. With stream, it then writes that line once per
// interval until the client disconnects, so the stream never goes quiet.
type readerTestAnswer struct {
	lines  []string
	hold   time.Duration
	stream string
	every  time.Duration
}

func readerTestOne(line string) readerTestAnswer { return readerTestAnswer{lines: []string{line}} }

// readerTestEventStream is a subscription that is acked, then sends an event
// every `every`.
func readerTestEventStream(every time.Duration) readerTestAnswer {
	return readerTestAnswer{lines: []string{readerTestAck}, stream: readerTestEvt, every: every}
}

// readerTestHeldAck is the ack, then a connection that stays open for 5 s.
func readerTestHeldAck() readerTestAnswer {
	return readerTestAnswer{lines: []string{readerTestAck}, hold: 5 * time.Second}
}

type readerTestRequest struct {
	Method string `json:"method"`
	Params struct {
		Subscriptions []map[string]string `json:"subscriptions"`
	} `json:"params"`
}

// namesPanes is true when a subscribe request names panes, not only the
// global events.
func (r readerTestRequest) namesPanes() bool {
	return len(r.Params.Subscriptions) > len(GlobalEvents)
}

func (r readerTestRequest) subscribesTo(pane string) bool {
	for _, sub := range r.Params.Subscriptions {
		if sub["pane_id"] == pane {
			return true
		}
	}
	return false
}

func readerTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "cr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// readerTestFake starts a fake herdr in dir. Each connection gets a goroutine:
// it reads the request and returns answer(request). Everything ends when the
// test ends.
func readerTestFake(t *testing.T, dir string, answer func(readerTestRequest) readerTestAnswer) string {
	t.Helper()
	path := filepath.Join(dir, "herdr.sock")
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	// A later fake may bind the same path; closing this one must not remove it.
	ln.SetUnlinkOnClose(false)
	quit := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadString('\n')
				if err != nil {
					return
				}
				var req readerTestRequest
				if json.Unmarshal([]byte(line), &req) != nil {
					return
				}
				reply := answer(req)
				for _, l := range reply.lines {
					if _, err := conn.Write([]byte(l + "\n")); err != nil {
						return
					}
				}
				select {
				case <-time.After(reply.hold):
				case <-quit:
					return
				}
				for reply.stream != "" {
					if _, err := conn.Write([]byte(reply.stream + "\n")); err != nil {
						return
					}
					select {
					case <-time.After(reply.every):
					case <-quit:
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		close(quit)
		ln.Close()
		wg.Wait()
	})
	return path
}

// readerTestStart starts a reader that the test stops at its end.
func readerTestStart(t *testing.T, target Target, every time.Duration) (*Reader, chan Update) {
	t.Helper()
	out := make(chan Update, 16)
	r := startReader(context.Background(), target, out, every)
	t.Cleanup(func() { readerTestJoin(t, r) })
	return r, out
}

// readerTestJoin stops the reader and waits up to 1 s for its goroutine.
func readerTestJoin(t *testing.T, r *Reader) {
	t.Helper()
	r.Stop()
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("reader goroutine still running 1 s after stop")
	}
}

// readerTestNext returns the next update. It fails after 3 s, so a stuck
// reader fails the test.
func readerTestNext(t *testing.T, out <-chan Update) Update {
	t.Helper()
	select {
	case u := <-out:
		return u
	case <-time.After(3 * time.Second):
		t.Fatal("no update")
		return Update{}
	}
}

func readerTestState(t *testing.T, out <-chan Update) ConnState {
	t.Helper()
	u := readerTestNext(t, out)
	if u.Tree != nil {
		t.Fatalf("expected a state, got a tree: %+v", u.Tree)
	}
	return u.State
}

func readerTestExpect(t *testing.T, out <-chan Update, kind ConnKind) ConnState {
	t.Helper()
	s := readerTestState(t, out)
	if s.Kind != kind {
		t.Fatalf("expected state kind %d, got %+v", kind, s)
	}
	return s
}

func readerTestTree(t *testing.T, out <-chan Update) *HerdrTree {
	t.Helper()
	u := readerTestNext(t, out)
	if u.Tree == nil {
		t.Fatalf("expected a tree, got state %+v", u.State)
	}
	return u.Tree
}

// readerTestLiveAfter applies the first count updates in order.
func readerTestLiveAfter(t *testing.T, out <-chan Update, count int) LiveHerdr {
	t.Helper()
	live := NewLiveHerdr()
	for range count {
		live.Apply(readerTestNext(t, out))
	}
	return live
}

func readerTestFirstStatus(t *testing.T, tree *HerdrTree) AgentState {
	t.Helper()
	agent := tree.Workspaces[0].Tabs[0].Panes[0].Agent
	if agent == nil {
		t.Fatal("pane has no agent")
	}
	return agent.Status
}

func readerTestTreeFrom(t *testing.T, text string) HerdrTree {
	t.Helper()
	var snap WireSnapshot
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatal(err)
	}
	return TreeFromSnapshot(&snap)
}

// readerTestConnected reads Connecting and Connected and returns the first tree.
func readerTestConnected(t *testing.T, out <-chan Update) *HerdrTree {
	t.Helper()
	readerTestExpect(t, out, Connecting)
	readerTestExpect(t, out, Connected)
	return readerTestTree(t, out)
}

func TestRetryDelayDoublesFromHalfASecondAndCapsAtFive(t *testing.T) {
	cases := []struct {
		attempt uint32
		want    time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, time.Second},
		{3, 4 * time.Second},
		{4, 5 * time.Second},
		{10, 5 * time.Second},
		{^uint32(0), 5 * time.Second},
	}
	for _, c := range cases {
		if got := retryDelay(c.attempt); got != c.want {
			t.Errorf("retryDelay(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestLiveHerdrKeepsTheTreeOnlyWhileConnected(t *testing.T) {
	tree := readerTestTreeFrom(t, readerTestEmpty)
	live := NewLiveHerdr()
	live.Apply(Update{State: ConnState{Kind: Connected, ServerVersion: "0.9.3"}})
	live.Apply(Update{Tree: &tree})
	if live.Tree != &tree {
		t.Fatalf("tree = %v, want the sent tree", live.Tree)
	}

	gone := ConnState{Kind: NotRunning, Reason: "gone"}
	live.Apply(Update{State: gone})
	if live.Tree != nil {
		t.Errorf("tree = %v, want nil", live.Tree)
	}
	if live.State != gone {
		t.Errorf("state = %+v, want %+v", live.State, gone)
	}
}

func TestConnStateLineIsTheTextTheTerminalAreaShows(t *testing.T) {
	cases := []struct {
		state ConnState
		line  string
		shown bool
	}{
		{ConnState{Kind: Connecting}, "connecting to herdr…", true},
		{ConnState{Kind: Connected, ServerVersion: "0.9.3"}, "", false},
		{ConnState{Kind: NotRunning, Reason: "x"}, NotRunningLine, true},
		{ConnState{Kind: Incompatible, Message: "m"}, "m", true},
	}
	for _, c := range cases {
		line, shown := c.state.Line()
		if line != c.line || shown != c.shown {
			t.Errorf("%+v.Line() = %q, %v; want %q, %v", c.state, line, shown, c.line, c.shown)
		}
	}
}

func TestResolveUsesTheSocketTargetOrTheLocalLookup(t *testing.T) {
	local := func() (string, bool) { return "/local.sock", true }
	if got, ok := resolve(SocketTarget("/s.sock"), local); got != "/s.sock" || !ok {
		t.Errorf("socket target = %q, %v", got, ok)
	}
	if got, ok := resolve(LocalTarget(), local); got != "/local.sock" || !ok {
		t.Errorf("local target = %q, %v", got, ok)
	}
	if _, ok := resolve(LocalTarget(), func() (string, bool) { return "", false }); ok {
		t.Error("local target with no lookup result resolved")
	}
}

func TestAMissingSocketIsNotRunningAndTheGoroutineEndsOnStop(t *testing.T) {
	dir := readerTestDir(t)
	r, out := readerTestStart(t, SocketTarget(filepath.Join(dir, "missing.sock")), refreshEvery)

	readerTestExpect(t, out, Connecting)
	readerTestExpect(t, out, NotRunning)
	readerTestJoin(t, r)
}

func TestFollowsSnapshotsAndStatusEventsOverASocket(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			// The first two snapshots are idle; the one after the event is working.
			status := "working"
			if snapshots.Add(1)-1 < 2 {
				status = "idle"
			}
			return readerTestOne(readerTestReply(readerTestSnapshot(status)))
		}
		return readerTestAnswer{lines: []string{readerTestAck, readerTestEvt}, hold: 600 * time.Millisecond}
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	readerTestExpect(t, out, Connecting)
	if s := readerTestExpect(t, out, Connected); s.ServerVersion != "0.9.3" {
		t.Errorf("server version = %q", s.ServerVersion)
	}
	idle := readerTestTree(t, out)
	working := readerTestTree(t, out)
	if got := readerTestFirstStatus(t, idle); got != StateIdle {
		t.Errorf("first tree status = %v, want idle", got)
	}
	if got := readerTestFirstStatus(t, working); got != StateWorking {
		t.Errorf("second tree status = %v, want working", got)
	}
}

func TestAPongTheReaderCannotParseIsIncompatible(t *testing.T) {
	path := readerTestFake(t, readerTestDir(t), func(readerTestRequest) readerTestAnswer {
		return readerTestOne(`{"id":"contr-ping","result":{"type":"pong"}}`)
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	readerTestExpect(t, out, Connecting)
	want := ConnState{Kind: Incompatible, Message: "herdr replied in an unexpected form · update contr"}
	if got := readerTestState(t, out); got != want {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

func TestAPaneThatClosedBeforeSubscribingIsDroppedFromTheSubscription(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch {
		case req.Method == "ping":
			return readerTestOne(readerTestPong)
		case req.Method == "session.snapshot":
			if snapshots.Add(1)-1 == 0 {
				return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
			}
			return readerTestOne(readerTestReply(readerTestEmpty))
		case req.namesPanes():
			return readerTestOne(readerTestGone)
		}
		return readerTestAnswer{lines: []string{readerTestAck}, hold: 600 * time.Millisecond}
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	readerTestExpect(t, out, Connecting)
	readerTestExpect(t, out, Connected)
	if ids := readerTestTree(t, out).PaneIDs(); len(ids) != 0 {
		t.Errorf("pane ids = %v, want none", ids)
	}
}

func TestAStateOrTreeEqualToTheLastOneIsNotSentAgain(t *testing.T) {
	out := make(chan Update, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctl := &control{ctx: ctx, cancel: cancel}
	r := &reader{out: out, ctl: ctl, every: refreshEvery}
	idle := readerTestTreeFrom(t, readerTestSnapshot("idle"))

	for _, err := range []error{
		r.report(ConnState{Kind: Connecting}),
		r.report(ConnState{Kind: Connecting}),
		r.publish(idle),
		r.publish(idle),
		r.publish(readerTestTreeFrom(t, readerTestSnapshot("working"))),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) != 3 {
		t.Errorf("%d updates sent, want 3", len(out))
	}
}

func TestAProtocolOtherThan22OnlyWarnsAndStillConnects(t *testing.T) {
	path := readerTestFake(t, readerTestDir(t), func(readerTestRequest) readerTestAnswer {
		return readerTestOne(`{"id":"contr-ping","result":{"type":"pong","version":"0.9.9","protocol":23}}`)
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	readerTestExpect(t, out, Connecting)
	if s := readerTestExpect(t, out, Connected); s.ServerVersion != "0.9.9" {
		t.Errorf("server version = %q", s.ServerVersion)
	}
}

// A reader whose context ends stops without a call to Stop. This replaces the
// Rust test for a dropped receiver: in Go the receiver side ends the reader by
// cancelling the context.
func TestAReaderWhoseContextEndsStopsWithoutAStop(t *testing.T) {
	dir := readerTestDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Update, 16)
	r := startReader(ctx, SocketTarget(filepath.Join(dir, "missing.sock")), out, refreshEvery)

	readerTestExpect(t, out, Connecting)
	cancel()
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("reader still running 1 s after its context ended")
	}
}

func TestAStopEndsTheReaderWhileEventsKeepComing(t *testing.T) {
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		}
		return readerTestEventStream(10 * time.Millisecond)
	})
	r, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	// Once the first tree is out, the reader is in its event loop.
	if got := readerTestFirstStatus(t, readerTestConnected(t, out)); got != StateIdle {
		t.Errorf("status = %v, want idle", got)
	}
	// Events 10 ms apart never leave the stream quiet, so only a check on every read stops it.
	readerTestJoin(t, r)
}

// readerTestQuietHidden starts a reader connected to a herdr that acks and
// then stays quiet, with the window hidden. It returns once the reader is in
// its event loop.
func readerTestQuietHidden(t *testing.T) (*Reader, chan Update) {
	t.Helper()
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		}
		return readerTestHeldAck()
	})
	// A short timer: if hiding did not stop it, the reader would wake often.
	r, out := readerTestStart(t, SocketTarget(path), 50*time.Millisecond)
	r.SetVisible(false)
	readerTestConnected(t, out)
	return r, out
}

func TestAQuietStreamDoesNotWakeTheReaderWhileHidden(t *testing.T) {
	r, _ := readerTestQuietHidden(t)
	before := r.ctl.reads.Load()
	// Time must pass to show that nothing happens in it.
	time.Sleep(time.Second)
	// At most the read that was waiting when the test started.
	if reads := r.ctl.reads.Load() - before; reads > 1 {
		t.Errorf("%d event reads in 1 s with no event", reads)
	}
}

func TestAStopEndsAReadThatWaitsWithNoTimerAtOnce(t *testing.T) {
	r, _ := readerTestQuietHidden(t)
	select {
	case <-r.Done():
		t.Fatal("reader ended before the stop")
	default:
	}
	started := time.Now()
	readerTestJoin(t, r)
	if d := time.Since(started); d > 200*time.Millisecond {
		t.Errorf("the goroutine ended %v after the stop", d)
	}
}

func TestABusyStreamSnapshotsAtMostOncePerSnapshotGap(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			snapshots.Add(1)
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		}
		return readerTestEventStream(10 * time.Millisecond)
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)
	readerTestConnected(t, out)

	before := snapshots.Load()
	window := time.Now()
	time.Sleep(time.Second)
	taken := snapshots.Load() - before
	// One snapshot per snapshotGap at most, plus two for the edges of the window.
	allowed := int64(time.Since(window)/snapshotGap) + 2
	if taken > allowed {
		t.Errorf("%d snapshots in %v", taken, time.Since(window))
	}
}

func TestTheTreeIsRefreshedOnTheTimerWhenNoEventComes(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			// Idle for the first two snapshots, working after that. No event is sent.
			status := "working"
			if snapshots.Add(1)-1 < 2 {
				status = "idle"
			}
			return readerTestOne(readerTestReply(readerTestSnapshot(status)))
		}
		return readerTestHeldAck()
	})
	_, out := readerTestStart(t, SocketTarget(path), 100*time.Millisecond)

	if got := readerTestFirstStatus(t, readerTestConnected(t, out)); got != StateIdle {
		t.Errorf("first status = %v, want idle", got)
	}
	if got := readerTestFirstStatus(t, readerTestTree(t, out)); got != StateWorking {
		t.Errorf("second status = %v, want working", got)
	}
}

// readerTestCountingFake is a quiet herdr that counts its snapshots.
func readerTestCountingFake(t *testing.T) (string, *atomic.Int64) {
	t.Helper()
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			snapshots.Add(1)
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		}
		return readerTestHeldAck()
	})
	return path, &snapshots
}

func TestTheTimerRefreshPausesWhileHidden(t *testing.T) {
	path, snapshots := readerTestCountingFake(t)
	// One second between timer snapshots: the hidden phase below spans more than one.
	r, out := readerTestStart(t, SocketTarget(path), time.Second)
	readerTestConnected(t, out)
	// The first session takes two snapshots (see connect). Count from here.
	connected := snapshots.Load()
	if !r.Visible() {
		t.Error("a reader starts with the window shown")
	}

	r.SetVisible(false)
	if r.Visible() {
		t.Error("reader still visible after SetVisible(false)")
	}
	time.Sleep(1500 * time.Millisecond)
	if got := snapshots.Load(); got != connected {
		t.Errorf("%d snapshots while hidden, want none", got-connected)
	}
}

func TestTheWindowShownAgainTakesOneSnapshot(t *testing.T) {
	path, snapshots := readerTestCountingFake(t)
	// The timer is due only every 30 s, so only the show can take a snapshot here.
	r, out := readerTestStart(t, SocketTarget(path), 30*time.Second)
	readerTestConnected(t, out)
	connected := snapshots.Load()

	r.SetVisible(false)
	// Longer than one read, so the reader sees the window hidden before it is shown.
	time.Sleep(400 * time.Millisecond)
	r.SetVisible(true)
	// The snapshot comes at once. The timer is not due for 30 s.
	deadline := time.Now().Add(900 * time.Millisecond)
	for snapshots.Load() == connected {
		if time.Now().After(deadline) {
			t.Fatal("no snapshot after the window was shown")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if got := snapshots.Load(); got != connected+1 {
		t.Errorf("%d snapshots after the show, want one", got-connected)
	}
}

func TestARefreshThatFailsWhenTheWindowIsShownEndsTheSession(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			// The two snapshots of the connect succeed. The third, taken on show, fails.
			if snapshots.Add(1)-1 < 2 {
				return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
			}
			return readerTestOne(`{"id":"contr-snapshot","error":{"code":"internal","message":"boom"}}`)
		}
		return readerTestHeldAck()
	})
	r, out := readerTestStart(t, SocketTarget(path), time.Second)
	readerTestConnected(t, out)

	r.SetVisible(false)
	// Longer than one read, so the reader sees the window hidden before it is shown.
	time.Sleep(400 * time.Millisecond)
	r.SetVisible(true)
	want := ConnState{Kind: Incompatible, Message: "herdr 0.9.3 replied in an unexpected form · update contr"}
	if got := readerTestState(t, out); got != want {
		t.Errorf("state after the failed refresh = %+v, want %+v", got, want)
	}
}

func TestTwoPaneClosesInOneSessionKeepTheConnection(t *testing.T) {
	var snapshots, refusals atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch {
		case req.Method == "ping":
			return readerTestOne(readerTestPong)
		case req.Method == "session.snapshot":
			// w1:p1 closes, then w1:p2 closes too; after that w1:p2 stays.
			pane := "w1:p2"
			if snapshots.Add(1)-1 == 0 {
				pane = "w1:p1"
			}
			return readerTestOne(readerTestReply(readerTestSnapshotOf(pane, "idle")))
		case req.namesPanes() && refusals.Add(1)-1 < 2:
			// The first two subscriptions that name a pane find it closed.
			return readerTestOne(readerTestGone)
		}
		return readerTestHeldAck()
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	live := readerTestLiveAfter(t, out, 3) // Connecting, Connected, then the tree
	if want := (ConnState{Kind: Connected, ServerVersion: "0.9.3"}); live.State != want {
		t.Errorf("state = %+v, want %+v", live.State, want)
	}
	if live.Tree == nil {
		t.Fatal("no tree")
	}
	if ids := live.Tree.PaneIDs(); !reflect.DeepEqual(ids, []string{"w1:p2"}) {
		t.Errorf("pane ids = %v, want [w1:p2]", ids)
	}
}

func TestAPaneRefusedAgainForTheSameSetEndsTheSession(t *testing.T) {
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch {
		case req.Method == "ping":
			return readerTestOne(readerTestPong)
		case req.Method == "session.snapshot":
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		case req.namesPanes():
			return readerTestOne(readerTestGone)
		}
		return readerTestHeldAck()
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	// The snapshot still lists w1:p1 after the fallback, so the refusal repeats.
	live := readerTestLiveAfter(t, out, 3) // Connecting, Connected, then Incompatible
	want := ConnState{Kind: Incompatible, Message: "herdr 0.9.3 replied in an unexpected form · update contr"}
	if live.State != want {
		t.Errorf("state = %+v, want %+v", live.State, want)
	}
	if live.Tree != nil {
		t.Errorf("tree = %v, want nil", live.Tree)
	}
}

func TestARefusalAfterAnAcceptedSubscriptionIsANewRace(t *testing.T) {
	var snapshots atomic.Int64
	path := readerTestFake(t, readerTestDir(t), func(req readerTestRequest) readerTestAnswer {
		switch {
		case req.Method == "ping":
			return readerTestOne(readerTestPong)
		case req.Method == "session.snapshot":
			// Snapshots name w1:p1, w1:p2, w1:p1, then w1:p2 for good.
			n := snapshots.Add(1) - 1
			pane := "w1:p2"
			if n == 0 || n == 2 {
				pane = "w1:p1"
			}
			return readerTestOne(readerTestReply(readerTestSnapshotOf(pane, "idle")))
		case req.subscribesTo("w1:p1"):
			return readerTestOne(readerTestGone)
		}
		return readerTestHeldAck()
	})
	_, out := readerTestStart(t, SocketTarget(path), refreshEvery)

	// The subscription to w1:p2 is accepted between the two refusals of w1:p1.
	live := readerTestLiveAfter(t, out, 3) // Connecting, Connected, then the tree
	if want := (ConnState{Kind: Connected, ServerVersion: "0.9.3"}); live.State != want {
		t.Errorf("state = %+v, want %+v", live.State, want)
	}
	if live.Tree == nil {
		t.Fatal("no tree")
	}
	if ids := live.Tree.PaneIDs(); !reflect.DeepEqual(ids, []string{"w1:p2"}) {
		t.Errorf("pane ids = %v, want [w1:p2]", ids)
	}
}

// The server ends the subscription and exits; a new server binds the same
// path (a new socket file). A hidden reader, which has no refresh timer,
// reports that herdr is gone, connects to the new server and sends its tree.
func TestAHiddenReaderFollowsANewServerOnTheSamePath(t *testing.T) {
	dir := readerTestDir(t)
	path := readerTestFake(t, dir, func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			return readerTestOne(readerTestReply(readerTestSnapshot("idle")))
		}
		return readerTestAnswer{lines: []string{readerTestAck}, hold: 500 * time.Millisecond}
	})
	r, out := readerTestStart(t, SocketTarget(path), refreshEvery)
	r.SetVisible(false)
	live := readerTestLiveAfter(t, out, 3)
	if live.Tree == nil {
		t.Fatal("no tree")
	}
	if got := readerTestFirstStatus(t, live.Tree); got != StateIdle {
		t.Errorf("first status = %v, want idle", got)
	}

	// The old server still holds the subscription; its path now leads to a new one.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	newPath := readerTestFake(t, dir, func(req readerTestRequest) readerTestAnswer {
		switch req.Method {
		case "ping":
			return readerTestOne(readerTestPong)
		case "session.snapshot":
			return readerTestOne(readerTestReply(readerTestSnapshot("working")))
		}
		return readerTestHeldAck()
	})
	if newPath != path {
		t.Fatalf("new path %q, want %q", newPath, path)
	}

	readerTestExpect(t, out, NotRunning)
	readerTestExpect(t, out, Connected)
	if got := readerTestFirstStatus(t, readerTestTree(t, out)); got != StateWorking {
		t.Errorf("status = %v, want working", got)
	}
}
