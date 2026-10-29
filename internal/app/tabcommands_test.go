package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/herdr"
)

// fakeHerdr accepts one client, writes reply (one line) and keeps the request it read.
type fakeHerdr struct {
	path    string
	request chan map[string]any
}

func serveFake(t *testing.T, reply string) fakeHerdr {
	t.Helper()
	// A short path: macOS limits socket paths to 103 bytes.
	dir, err := os.MkdirTemp("/tmp", "tc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "herdr.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := fakeHerdr{path: path, request: make(chan map[string]any, 1)}
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var req map[string]any
		_ = json.Unmarshal(line, &req)
		f.request <- req
		_, _ = conn.Write([]byte(reply + "\n"))
	}()
	return f
}

func (f fakeHerdr) got(t *testing.T) map[string]any {
	t.Helper()
	select {
	case r := <-f.request:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("the fake herdr read no request")
		return nil
	}
}

// missingSocket is a socket path with no herdr behind it. It is short, because macOS limits
// socket paths to 103 bytes.
func missingSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "missing.sock")
}

func TestFocusSendsTabFocusAndAcceptsTabInfo(t *testing.T) {
	f := serveFake(t, `{"result":{"type":"tab_info"}}`)
	if err := (TabCommand{TabFocus, "w1:t2"}).run(f.path); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": "contr-tab-focus", "method": "tab.focus", "params": map[string]any{"tab_id": "w1:t2"}}
	if got := f.got(t); !reflect.DeepEqual(got, want) {
		t.Errorf("request %v, want %v", got, want)
	}
}

func TestCreateSendsTabCreateInTheWorkspaceFocused(t *testing.T) {
	f := serveFake(t, `{"result":{"type":"tab_created"}}`)
	if err := (TabCommand{TabCreate, "w1"}).run(f.path); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"id": "contr-tab-create", "method": "tab.create",
		"params": map[string]any{"workspace_id": "w1", "focus": true},
	}
	if got := f.got(t); !reflect.DeepEqual(got, want) {
		t.Errorf("request %v, want %v", got, want)
	}
}

func TestSplitSendsPaneSplitToTheRightFocused(t *testing.T) {
	f := serveFake(t, `{"result":{"type":"pane_info"}}`)
	if err := (TabCommand{TabSplitRight, "w1:p1"}).run(f.path); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"id": "contr-split", "method": "pane.split",
		"params": map[string]any{"target_pane_id": "w1:p1", "direction": "right", "focus": true},
	}
	if got := f.got(t); !reflect.DeepEqual(got, want) {
		t.Errorf("request %v, want %v", got, want)
	}
}

func TestAServerErrorIsReturnedWithItsCode(t *testing.T) {
	f := serveFake(t, `{"error":{"code":"tab_not_found","message":"no such tab"}}`)
	err := (TabCommand{TabFocus, "w9:t9"}).run(f.path)
	var wire *herdr.WireError
	if !errors.As(err, &wire) || wire.Kind != herdr.ErrServer || wire.Code != "tab_not_found" {
		t.Fatalf("got %v, want a server error with code tab_not_found", err)
	}
	f.got(t)
}

func TestNoRunningHerdrIsReportedAsNotRunning(t *testing.T) {
	err := (TabCommand{TabCreate, "w1"}).run(missingSocket(t))
	var wire *herdr.WireError
	if !errors.As(err, &wire) || !wire.IsNotRunning() {
		t.Fatalf("got %v, want a not-running error", err)
	}
}

func TestAPathIsKeptAndAMissingPathIsDropped(t *testing.T) {
	cmd := TabCommand{TabSplitRight, "w1:p1"}
	if path, ok := socketOrWarn(func() (string, bool) { return "herdr.sock", true }, cmd); !ok || path != "herdr.sock" {
		t.Errorf("got %q, %v, want the path kept", path, ok)
	}
	if _, ok := socketOrWarn(func() (string, bool) { return "", false }, cmd); ok {
		t.Error("a missing path should be dropped")
	}
}

// reports collects what a commander tells the user.
type reports struct {
	mu   sync.Mutex
	list []string
}

func (r *reports) add(s string) { r.mu.Lock(); r.list = append(r.list, s); r.mu.Unlock() }
func (r *reports) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.list...)
}

func TestAFailedCommandShowsInTheStatusMessage(t *testing.T) {
	var r reports
	cmd := TabCommand{TabFocus, "w1:t2"}
	c := &commander{
		socket: func() (string, bool) { return "x.sock", true },
		run:    func(string, TabCommand) error { return errors.New("boom") },
		report: r.add,
	}
	c.do(cmd)
	if got := r.all(); !reflect.DeepEqual(got, []string{"herdr: boom"}) {
		t.Errorf("reports %q", got)
	}
}

func TestANotRunningHerdrShowsInTheStatusMessage(t *testing.T) {
	var r reports
	c := newCommander(r.add)
	c.socket = func() (string, bool) { return missingSocket(t), true }
	c.do(TabCommand{TabCreate, "w1"})
	if got := r.all(); !reflect.DeepEqual(got, []string{"herdr is not running"}) {
		t.Errorf("reports %q", got)
	}
}

func TestNoSocketPathShowsInTheStatusMessage(t *testing.T) {
	var r reports
	c := newCommander(r.add)
	c.socket = func() (string, bool) { return "", false }
	c.do(TabCommand{TabCreate, "w1"})
	if got := r.all(); !reflect.DeepEqual(got, []string{"herdr is not running"}) {
		t.Errorf("reports %q", got)
	}
}

func TestSendReturnsBeforeTheCommandEnds(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})
	var r reports
	c := &commander{
		socket: func() (string, bool) { return "x.sock", true },
		run:    func(string, TabCommand) error { <-release; return nil },
		report: r.add,
		done:   func() { close(done) },
	}
	c.Send(TabCommand{TabFocus, "w1:t2"}) // would hang here if it ran inline
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not end")
	}
	if got := r.all(); len(got) != 0 {
		t.Errorf("a command that worked reported %q", got)
	}
}
