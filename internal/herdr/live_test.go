//go:build live

// Live tests: the reader against a real herdr server.
//
// They run only against the isolated server from scripts/dev-herdr.sh. They
// never touch the owner's herdr. A test skips when the environment does not
// point at that server. Run them once, by hand:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags live -race ./internal/herdr/...
//	scripts/dev-herdr.sh stop
//
// The tests stop and start the dev server with the same script. The build tag
// keeps them out of scripts/check.sh.

package herdr

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const liveCallTimeout = 5 * time.Second

// liveSocket returns the API socket of the dev server, and skips the test
// when the environment does not name it. The socket must be the one that
// scripts/dev-herdr.sh makes, so that the tests cannot reach another herdr.
func liveSocket(t *testing.T) string {
	t.Helper()
	sock := os.Getenv("HERDR_SOCKET_PATH")
	if sock == "" || os.Getenv("CONTR_SURFACE") == "" {
		t.Skip("no dev herdr: run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	base := os.Getenv("TMPDIR")
	if base == "" {
		base = "/tmp"
	}
	want := filepath.Join(strings.TrimSuffix(base, "/"), "contr-go-dev-herdr", "a.sock")
	if filepath.Clean(sock) != want {
		t.Skipf("HERDR_SOCKET_PATH %q is not the dev herdr socket %q", sock, want)
	}
	return sock
}

// devHerdr runs scripts/dev-herdr.sh with the arguments.
func devHerdr(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("../../scripts/dev-herdr.sh", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("dev-herdr.sh %v: %v\n%s", args, err, out)
	}
}

// liveCall sends one request on a new connection and returns the result.
func liveCall(sock, method string, params any) (json.RawMessage, error) {
	return exchange(sock, liveCallTimeout, "contr-live", method, params)
}

// waitAnswering waits until the server answers ping.
func waitAnswering(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := liveCall(sock, "ping", map[string]any{})
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the dev herdr did not answer ping: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitGone waits until the server socket answers no more.
func waitGone(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := liveCall(sock, "ping", map[string]any{}); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the dev herdr did not exit after the stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// firstPane returns the id of the first pane in the server's snapshot. It
// creates a workspace first when the server has none.
func firstPane(t *testing.T, sock string) string {
	t.Helper()
	pane := func() string {
		raw, err := liveCall(sock, "session.snapshot", map[string]any{})
		if err != nil {
			t.Fatalf("take a snapshot: %v", err)
		}
		var r struct {
			Snapshot struct {
				Panes []struct {
					PaneID string `json:"pane_id"`
				} `json:"panes"`
			} `json:"snapshot"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			t.Fatalf("read the snapshot: %v: %s", err, raw)
		}
		if len(r.Snapshot.Panes) == 0 {
			return ""
		}
		return r.Snapshot.Panes[0].PaneID
	}
	if id := pane(); id != "" {
		return id
	}
	createWorkspace(t, sock)
	id := pane()
	if id == "" {
		t.Fatal("no pane after workspace.create")
	}
	return id
}

func createWorkspace(t *testing.T, sock string) {
	t.Helper()
	raw, err := liveCall(sock, "workspace.create", map[string]any{"cwd": "/tmp"})
	if err != nil {
		t.Fatalf("create a workspace: %v", err)
	}
	var r struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &r); err != nil || r.Type != "workspace_created" {
		t.Fatalf("workspace.create reply: %s (%v)", raw, err)
	}
}

// startLiveReader starts a reader on the socket. It stops with the test.
func startLiveReader(t *testing.T, sock string) (*Reader, <-chan Update) {
	t.Helper()
	ch := make(chan Update, 64)
	r := StartReader(context.Background(), SocketTarget(sock), ch)
	t.Cleanup(func() {
		r.Stop()
		<-r.Done()
	})
	return r, ch
}

// waitFor takes updates from ch until want accepts one. Updates it rejects
// are skipped. It fails after within.
func waitFor(t *testing.T, ch <-chan Update, within time.Duration, what string, want func(Update) bool) Update {
	t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				t.Fatalf("the reader stopped while waiting for %s", what)
			}
			if want(u) {
				return u
			}
		case <-timer.C:
			t.Fatalf("timed out after %v waiting for %s", within, what)
		}
	}
}

func isState(kind ConnKind) func(Update) bool {
	return func(u Update) bool { return u.Tree == nil && u.State.Kind == kind }
}

// agentOf returns the agent of the pane with the id, or nil.
func agentOf(tree *HerdrTree, paneID string) *AgentInfo {
	for _, w := range tree.Workspaces {
		for _, tab := range w.Tabs {
			for _, p := range tab.Panes {
				if p.ID == paneID {
					return p.Agent
				}
			}
		}
	}
	return nil
}

// reportAndWait reports a state for an agent in the pane and waits until a
// tree with that state reaches the reader.
func reportAndWait(t *testing.T, sock string, ch <-chan Update, paneID string, state AgentState, within time.Duration) {
	t.Helper()
	_, err := liveCall(sock, "pane.report_agent", map[string]any{
		"pane_id": paneID,
		"source":  "contr-test",
		"agent":   "claude",
		"state":   state.Label(),
	})
	if err != nil {
		t.Fatalf("report the agent: %v", err)
	}
	waitFor(t, ch, within, state.Label(), func(u Update) bool {
		if u.Tree == nil {
			return false
		}
		a := agentOf(u.Tree, paneID)
		return a != nil && a.Name == "claude" && a.Status == state
	})
}

// TestIsolatedServerReportsTreeAgentStatusAndReconnects: the connection, the
// first tree, the agent status of a reported agent, then the reconnect after
// the server restarts.
func TestIsolatedServerReportsTreeAgentStatusAndReconnects(t *testing.T) {
	sock := liveSocket(t)
	devHerdr(t) // start, or keep the running server
	waitAnswering(t, sock)
	paneID := firstPane(t, sock)

	_, ch := startLiveReader(t, sock)
	u := waitFor(t, ch, 10*time.Second, "Connected", isState(Connected))
	if u.State.ServerVersion == "" {
		t.Error("herdr reported no version")
	}
	waitFor(t, ch, 10*time.Second, "a tree with the pane", func(u Update) bool {
		return u.Tree != nil && contains(u.Tree.PaneIDs(), paneID)
	})

	for _, state := range []AgentState{StateWorking, StateBlocked} {
		reportAndWait(t, sock, ch, paneID, state, 3*time.Second)
	}

	// The server stops and the reader reports it. A new server on the same
	// paths brings the connection back.
	devHerdr(t, "stop")
	waitGone(t, sock)
	waitFor(t, ch, 10*time.Second, "NotRunning after the server stopped", isState(NotRunning))
	devHerdr(t)
	waitAnswering(t, sock)
	waitFor(t, ch, 15*time.Second, "Connected after the restart", isState(Connected))
	waitFor(t, ch, 10*time.Second, "a tree after the restart", func(u Update) bool { return u.Tree != nil })
}

func contains(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// stopAndNewServerReachTheReader: `herdr server stop` ends the server, then a
// new server process starts on the same paths. The reader reports that herdr
// is gone, connects to the new server and passes on a status change made
// there. With visible false the reader runs as for a hidden window.
func stopAndNewServerReachTheReader(t *testing.T, visible bool) {
	sock := liveSocket(t)
	devHerdr(t)
	waitAnswering(t, sock)
	paneID := firstPane(t, sock)

	r, ch := startLiveReader(t, sock)
	r.SetVisible(visible)
	waitFor(t, ch, 10*time.Second, "Connected", isState(Connected))
	reportAndWait(t, sock, ch, paneID, StateIdle, 5*time.Second)

	devHerdr(t, "stop")
	waitGone(t, sock)
	waitFor(t, ch, 10*time.Second, "NotRunning after herdr server stop", isState(NotRunning))
	devHerdr(t)
	waitAnswering(t, sock)
	waitFor(t, ch, 15*time.Second, "Connected to the new server", isState(Connected))
	paneID = firstPane(t, sock)
	reportAndWait(t, sock, ch, paneID, StateWorking, 5*time.Second)
}

func TestServerStopAndANewServerReachAShownReader(t *testing.T) {
	stopAndNewServerReachTheReader(t, true)
}

func TestServerStopAndANewServerReachAHiddenReader(t *testing.T) {
	stopAndNewServerReachTheReader(t, false)
}
