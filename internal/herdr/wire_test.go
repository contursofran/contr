package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	tLong = 2 * time.Second
	short = 200 * time.Millisecond
	poll  = 50 * time.Millisecond
)

const pong = `{"id":"contr-ping","result":{"type":"pong","version":"0.9.3","protocol":22,"capabilities":{"endpoint_protocol_generation":1}}}`
const ack = `{"id":"contr-sub","result":{"type":"subscription_started"}}`
const event = `{"event":"pane.agent_status_changed","data":{"type":"pane.agent_status_changed","agent":"claude","agent_status":"working","pane_id":"w1:p1","workspace_id":"w1"}}`

// snapshotFixture is a hand-written `snapshot` object, shaped after the keys
// herdr documents.
const snapshotFixture = `{
	"version": "0.9.3",
	"protocol": 22,
	"focused_workspace_id": "w1",
	"focused_tab_id": "w1:t1",
	"focused_pane_id": "w1:p1",
	"workspaces": [
		{"workspace_id": "w1", "number": 1, "label": "contr", "focused": true, "pane_count": 2, "tab_count": 2, "active_tab_id": "w1:t1", "agent_status": "working"},
		{"workspace_id": "w2", "number": 2, "label": "notes", "focused": false, "pane_count": 2, "tab_count": 1, "active_tab_id": "w2:t2", "agent_status": "idle"}
	],
	"tabs": [
		{"tab_id": "w1:t1", "workspace_id": "w1", "number": 1, "label": "planner", "focused": true, "pane_count": 2, "agent_status": "working"},
		{"tab_id": "w1:t2", "workspace_id": "w1", "number": 2, "label": "build", "focused": false, "pane_count": 1, "agent_status": "done"},
		{"tab_id": "w2:t2", "workspace_id": "w2", "number": 1, "label": "main", "focused": false, "pane_count": 2, "agent_status": "idle"}
	],
	"panes": [
		{"pane_id": "w1:p1", "tab_id": "w1:t1", "workspace_id": "w1", "terminal_id": "t1", "focused": true, "revision": 7, "agent_status": "working", "agent": "claude", "display_agent": "claude-code", "label": "api", "cwd": "/srv/example", "scroll": {"offset": 0}},
		{"pane_id": "w1:p2", "tab_id": "w1:t1", "workspace_id": "w1", "terminal_id": "t2", "focused": false, "revision": 3, "agent_status": "unknown"},
		{"pane_id": "w1:p3", "tab_id": "w1:t2", "workspace_id": "w1", "terminal_id": "t3", "focused": false, "revision": 9, "agent_status": "done", "agent": "codex"},
		{"pane_id": "w2:p1", "tab_id": "w2:t2", "workspace_id": "w2", "terminal_id": "t4", "focused": false, "revision": 1, "agent_status": "idle"}
	],
	"agents": [],
	"layouts": []
}`

const emptySnapshot = `{"version":"0.9.3","protocol":22,"workspaces":[],"tabs":[],"panes":[],"agents":[],"layouts":[]}`

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func requestJSON(t *testing.T, request string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(request), &out); err != nil {
		t.Fatalf("request is not JSON: %v", err)
	}
	return out
}

func wireErr(t *testing.T, err error) *WireError {
	t.Helper()
	var we *WireError
	if !errors.As(err, &we) {
		t.Fatalf("error %v (%T) is not a WireError", err, err)
	}
	return we
}

func wantKind(t *testing.T, err error, kind ErrKind) *WireError {
	t.Helper()
	we := wireErr(t, err)
	if we.Kind != kind {
		t.Fatalf("error kind = %d, want %d (%v)", we.Kind, kind, err)
	}
	return we
}

func equalStr(t *testing.T, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("got %v, want %q", got, want)
	}
}

func TestSocketPathPrefersHerdrSocketPath(t *testing.T) {
	got, ok := socketPathFrom(envOf(map[string]string{
		"HERDR_SOCKET_PATH": "/s/herdr.sock",
		"XDG_CONFIG_HOME":   "/x",
		"HOME":              "/h",
	}))
	if !ok || got != "/s/herdr.sock" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSocketPathFallsBackToXdgConfigHome(t *testing.T) {
	got, ok := socketPathFrom(envOf(map[string]string{"XDG_CONFIG_HOME": "/x", "HOME": "/h"}))
	if !ok || got != "/x/herdr/herdr.sock" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSocketPathFallsBackToHome(t *testing.T) {
	got, ok := socketPathFrom(envOf(map[string]string{"HOME": "/h"}))
	if !ok || got != "/h/.config/herdr/herdr.sock" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSocketPathIgnoresEmptyValues(t *testing.T) {
	got, ok := socketPathFrom(envOf(map[string]string{
		"HERDR_SOCKET_PATH": "",
		"XDG_CONFIG_HOME":   "",
		"HOME":              "/h",
	}))
	if !ok || got != "/h/.config/herdr/herdr.sock" {
		t.Fatalf("got %q, %v", got, ok)
	}
}

func TestSocketPathIsNoneWithoutHomeOrXdg(t *testing.T) {
	if got, ok := socketPathFrom(envOf(nil)); ok {
		t.Fatalf("got %q", got)
	}
	if got, ok := socketPathFrom(envOf(map[string]string{"HERDR_SOCKET_PATH": ""})); ok {
		t.Fatalf("got %q", got)
	}
}

func TestRequestLineIsOneJSONLine(t *testing.T) {
	line, err := RequestLine("contr-ping", "ping", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
		t.Fatalf("not one line: %q", line)
	}
	want := map[string]any{"id": "contr-ping", "method": "ping", "params": map[string]any{}}
	if got := requestJSON(t, strings.TrimSuffix(line, "\n")); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRequestLineKeepsKeyOrderAndDoesNotEscapeHTML(t *testing.T) {
	line, err := RequestLine("i", "m", map[string]any{"b": "<&>", "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"i","method":"m","params":{"a":1,"b":"<&>"}}` + "\n"
	if line != want {
		t.Fatalf("got %q, want %q", line, want)
	}
}

func TestParseReplyReturnsResult(t *testing.T) {
	result, err := parseReply(`{"id":"r1","result":{"type":"pong"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"type":"pong"}` {
		t.Fatalf("got %s", result)
	}
}

func TestParseReplyMapsServerError(t *testing.T) {
	_, err := parseReply(`{"id":"r1","error":{"code":"pane_not_found","message":"pane w9:p99 not found"}}`)
	we := wantKind(t, err, ErrServer)
	if we.Code != "pane_not_found" || we.Message != "pane w9:p99 not found" {
		t.Fatalf("got %q %q", we.Code, we.Message)
	}
}

func TestParseReplyRejectsGarbageAndRepliesWithoutResult(t *testing.T) {
	for _, line := range []string{
		"not json",
		`{"id":"r1"}`,
		`{"id":"r1","result":null}`,
		`{"id":"r1","error":{"code":"x"}}`,
	} {
		_, err := parseReply(line)
		wantKind(t, err, ErrUnexpected)
	}
}

func TestWireErrorMessagesArePlain(t *testing.T) {
	if got := closedError().Error(); got != "herdr closed the connection" {
		t.Fatal(got)
	}
	server := &WireError{Kind: ErrServer, Code: "pane_not_found", Message: "pane w9:p99 not found"}
	if got := server.Error(); got != "herdr error pane_not_found: pane w9:p99 not found" {
		t.Fatal(got)
	}
	if got := unexpected("bad").Error(); got != "bad" {
		t.Fatal(got)
	}
}

func TestIsNotRunningOnlyForMissingOrRefusedSockets(t *testing.T) {
	for _, cause := range []error{fs.ErrNotExist, syscall.ECONNREFUSED, syscall.EADDRNOTAVAIL} {
		if !wireErr(t, ioError(cause)).IsNotRunning() {
			t.Errorf("%v", cause)
		}
	}
	for _, cause := range []error{os.ErrDeadlineExceeded, fs.ErrPermission} {
		if wireErr(t, ioError(cause)).IsNotRunning() {
			t.Errorf("%v", cause)
		}
	}
	if wireErr(t, closedError()).IsNotRunning() {
		t.Error("closed")
	}
	if wireErr(t, unexpected("")).IsNotRunning() {
		t.Error("unexpected")
	}
}

func parseSnapshot(t *testing.T, text string) WireSnapshot {
	t.Helper()
	var snap WireSnapshot
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestSnapshotFixtureHasTwoWorkspacesThreeTabsFourPanes(t *testing.T) {
	snap := parseSnapshot(t, snapshotFixture)
	if snap.Version != "0.9.3" || snap.Protocol != 22 {
		t.Fatalf("%q %d", snap.Version, snap.Protocol)
	}
	equalStr(t, snap.FocusedWorkspaceID, "w1")
	equalStr(t, snap.FocusedPaneID, "w1:p1")
	if len(snap.Workspaces) != 2 {
		t.Fatal(len(snap.Workspaces))
	}
	if snap.Workspaces[0].Label != "contr" || !snap.Workspaces[0].Focused {
		t.Fatalf("%+v", snap.Workspaces[0])
	}
	if snap.Workspaces[1].AgentStatus != StatusIdle {
		t.Fatal(snap.Workspaces[1].AgentStatus)
	}
	if len(snap.Tabs) != 3 || snap.Tabs[1].AgentStatus != StatusDone {
		t.Fatalf("%+v", snap.Tabs)
	}
	if len(snap.Panes) != 4 {
		t.Fatal(len(snap.Panes))
	}
}

func TestSnapshotFixtureKeepsAgentFieldsAndDoneStatus(t *testing.T) {
	snap := parseSnapshot(t, snapshotFixture)
	first := snap.Panes[0]
	equalStr(t, first.Agent, "claude")
	equalStr(t, first.DisplayAgent, "claude-code")
	if !first.Focused {
		t.Fatal("first pane is not focused")
	}
	if snap.Panes[1].Agent != nil {
		t.Fatal("pane 1 has an agent")
	}
	if snap.Panes[1].AgentStatus != StatusUnknown {
		t.Fatal(snap.Panes[1].AgentStatus)
	}
	if snap.Panes[2].AgentStatus != StatusDone {
		t.Fatal(snap.Panes[2].AgentStatus)
	}
}

func TestEmptyServerSnapshotWithoutFocusParses(t *testing.T) {
	snap := parseSnapshot(t, emptySnapshot)
	if snap.FocusedWorkspaceID != nil || snap.FocusedTabID != nil || snap.FocusedPaneID != nil {
		t.Fatalf("%+v", snap)
	}
	if len(snap.Workspaces) != 0 || len(snap.Tabs) != 0 || len(snap.Panes) != 0 {
		t.Fatalf("%+v", snap)
	}
}

func TestSnapshotPaneTokensDecodeAndAPaneWithoutThemHasNone(t *testing.T) {
	snap := parseSnapshot(t, `{"version":"0.9.3","protocol":22,"panes":[
		{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent_status":"idle","tokens":{"contr_board":"1 s1 3","x":"y"}},
		{"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","agent_status":"idle"}
	]}`)
	with := snap.Panes[0].Tokens
	if len(with) != 2 || with["contr_board"] != "1 s1 3" || with["x"] != "y" {
		t.Fatalf("%v", with)
	}
	if len(snap.Panes[1].Tokens) != 0 {
		t.Fatalf("%v", snap.Panes[1].Tokens)
	}
}

func TestAPaneTokenValueThatIsNotAStringFailsTheSnapshot(t *testing.T) {
	var snap WireSnapshot
	err := json.Unmarshal([]byte(`{"version":"0.9.3","protocol":22,"panes":[
		{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent_status":"idle","tokens":{"contr_board":3}}
	]}`), &snap)
	if err == nil {
		t.Fatal("snapshot parsed")
	}
}

func TestSnapshotRequiresItsFields(t *testing.T) {
	for _, text := range []string{
		`{"protocol":22}`,
		`{"version":"0.9.3"}`,
		`{"version":"0.9.3","protocol":22,"workspaces":[{"workspace_id":"w1","label":"a"}]}`,
		`{"version":"0.9.3","protocol":22,"tabs":[{"tab_id":"t","workspace_id":"w1","agent_status":"idle"}]}`,
		`{"version":"0.9.3","protocol":22,"panes":[{"pane_id":"p","tab_id":"t","workspace_id":"w1","agent_status":null}]}`,
		`{"version":"0.9.3","protocol":22,"panes":[{"pane_id":"p","tab_id":"t","workspace_id":"w1","agent_status":3}]}`,
	} {
		var snap WireSnapshot
		if err := json.Unmarshal([]byte(text), &snap); err == nil {
			t.Errorf("parsed %s", text)
		}
	}
}

func TestWireStatusMapsFiveStatesAndUnknownStrings(t *testing.T) {
	cases := []struct {
		text string
		want WireStatus
	}{
		{"idle", StatusIdle},
		{"working", StatusWorking},
		{"blocked", StatusBlocked},
		{"done", StatusDone},
		{"unknown", StatusUnknown},
		{"sleeping", StatusUnknown},
	}
	for _, c := range cases {
		var got WireStatus
		if err := json.Unmarshal([]byte(`"`+c.text+`"`), &got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: got %v, want %v", c.text, got, c.want)
		}
	}
}

// step is one scripted read: data, or a failure.
type step struct {
	data []byte
	fail error
}

// script yields the scripted reads in order. An exhausted script is the end
// of the stream.
type script []step

func (s *script) Read(out []byte) (int, error) {
	if len(*s) == 0 {
		return 0, io.EOF
	}
	next := (*s)[0]
	*s = (*s)[1:]
	if next.fail != nil {
		return 0, next.fail
	}
	return copy(out, next.data), nil
}

// repeat reads the same byte for ever.
type repeat byte

func (r repeat) Read(out []byte) (int, error) {
	for i := range out {
		out[i] = byte(r)
	}
	return len(out), nil
}

func nextLine(t *testing.T, r *LineReader) (string, bool) {
	t.Helper()
	line, ok, err := r.NextLine()
	if err != nil {
		t.Fatal(err)
	}
	return line, ok
}

func TestLineReaderKeepsPartialLineAcrossTimeout(t *testing.T) {
	reader := NewLineReader(&script{
		{data: []byte("{\"a\":1}\n{\"b\"")},
		{fail: os.ErrDeadlineExceeded},
		{data: []byte(":2}\n")},
	})
	if line, ok := nextLine(t, reader); !ok || line != `{"a":1}` {
		t.Fatalf("line 1: %q %v", line, ok)
	}
	if line, ok := nextLine(t, reader); ok {
		t.Fatalf("timeout gave %q", line)
	}
	if line, ok := nextLine(t, reader); !ok || line != `{"b":2}` {
		t.Fatalf("line 2: %q %v", line, ok)
	}
	_, _, err := reader.NextLine()
	wantKind(t, err, ErrClosed)
}

func TestLineReaderRetriesInterruptedAndMapsOtherReadErrors(t *testing.T) {
	interrupted := NewLineReader(&script{{fail: syscall.EINTR}, {data: []byte("ok\n")}})
	if line, ok := nextLine(t, interrupted); !ok || line != "ok" {
		t.Fatalf("%q %v", line, ok)
	}

	timedOut := NewLineReader(&script{{fail: syscall.ETIMEDOUT}})
	if line, ok := nextLine(t, timedOut); ok {
		t.Fatalf("timeout gave %q", line)
	}

	wouldBlock := NewLineReader(&script{{fail: syscall.EAGAIN}})
	if line, ok := nextLine(t, wouldBlock); ok {
		t.Fatalf("would-block gave %q", line)
	}

	reset := NewLineReader(&script{{fail: syscall.ECONNRESET}})
	_, _, err := reset.NextLine()
	we := wantKind(t, err, ErrIO)
	if !errors.Is(we, syscall.ECONNRESET) {
		t.Fatalf("cause lost: %v", err)
	}
}

func TestLineReaderReportsEndOfStreamAsClosed(t *testing.T) {
	_, _, err := NewLineReader(strings.NewReader("")).NextLine()
	wantKind(t, err, ErrClosed)
}

func TestLineReaderRejectsOverlongLine(t *testing.T) {
	_, _, err := NewLineReader(repeat('a')).NextLine()
	wantKind(t, err, ErrUnexpected)
}

func TestLineReaderRejectsNonUTF8Line(t *testing.T) {
	reader := NewLineReader(strings.NewReader("\xff\nnext\n"))
	_, _, err := reader.NextLine()
	wantKind(t, err, ErrUnexpected)
	// The bad line is gone; the reader goes on with the next one.
	if line, ok := nextLine(t, reader); !ok || line != "next" {
		t.Fatalf("%q %v", line, ok)
	}
}

// fakeServer is a fake herdr on a socket in a temporary folder.
type fakeServer struct {
	request chan string
}

// Request waits for the request line that the client sent.
func (s fakeServer) Request(t *testing.T) string {
	t.Helper()
	select {
	case request := <-s.request:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("the fake server did not finish")
		return ""
	}
}

// tempDir makes a short folder: socket paths have a small length limit.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// serve binds herdr.sock in dir, accepts one client, reads its request line,
// writes replies (one line each), waits hold, then closes. The socket file
// stays after the server ends, as a crashed herdr leaves it.
func serve(t *testing.T, dir string, replies []string, hold time.Duration) (string, fakeServer) {
	t.Helper()
	path := filepath.Join(dir, "herdr.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	server := fakeServer{request: make(chan string, 1)}
	go func() {
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close()
		request, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		for _, reply := range replies {
			if _, err := io.WriteString(conn, reply+"\n"); err != nil {
				t.Errorf("write reply: %v", err)
				return
			}
		}
		select {
		case <-time.After(hold):
		case <-stop:
		}
		server.request <- strings.TrimRight(request, "\n")
	}()
	return path, server
}

func TestPingParsesPongAndSendsPing(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{pong}, 0)
	got, err := Ping(path, tLong)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.9.3" || got.Protocol != 22 {
		t.Fatalf("%+v", got)
	}
	request := requestJSON(t, server.Request(t))
	if request["id"] != "contr-ping" || request["method"] != "ping" {
		t.Fatalf("%v", request)
	}
}

func TestPingRejectsAResultOfAnotherType(t *testing.T) {
	reply := `{"id":"contr-ping","result":{"type":"session_snapshot"}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := Ping(path, tLong)
	wantKind(t, err, ErrUnexpected)
	server.Request(t)
}

func TestPingMissingSocketIsNotRunning(t *testing.T) {
	_, err := Ping(filepath.Join(tempDir(t), "missing.sock"), tLong)
	if we := wireErr(t, err); !we.IsNotRunning() {
		t.Fatalf("%v", err)
	}
}

func TestPingStaleSocketFileIsNotRunning(t *testing.T) {
	path := filepath.Join(tempDir(t), "herdr.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	listener.Close() // the socket file stays, nobody listens
	_, err = Ping(path, tLong)
	if we := wireErr(t, err); !we.IsNotRunning() {
		t.Fatalf("%v", err)
	}
}

func TestPingTimesOutWhenServerIsSilent(t *testing.T) {
	path, server := serve(t, tempDir(t), nil, 600*time.Millisecond)
	_, err := Ping(path, short)
	we := wantKind(t, err, ErrIO)
	if !errors.Is(we, os.ErrDeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if we.IsNotRunning() {
		t.Fatal("a timeout is not 'not running'")
	}
	server.Request(t)
}

func TestSnapshotParsesFixtureOverASocket(t *testing.T) {
	// A reply is one line: flatten the pretty-printed fixture first.
	flat := strings.ReplaceAll(snapshotFixture, "\n", " ")
	reply := `{"id":"contr-snapshot","result":{"type":"session_snapshot","snapshot":` + flat + `}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	snap, err := Snapshot(path, tLong)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Workspaces) != 2 || len(snap.Panes) != 4 {
		t.Fatalf("%+v", snap)
	}
	if request := requestJSON(t, server.Request(t)); request["method"] != "session.snapshot" {
		t.Fatalf("%v", request)
	}
}

func TestSnapshotRejectsABodyWithoutRequiredFields(t *testing.T) {
	reply := `{"id":"contr-snapshot","result":{"type":"session_snapshot","snapshot":{"version":"0.9.3"}}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := Snapshot(path, tLong)
	wantKind(t, err, ErrUnexpected)
	server.Request(t)
}

func TestSnapshotRejectsAResultWithoutSnapshot(t *testing.T) {
	reply := `{"id":"contr-snapshot","result":{"type":"session_snapshot"}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := Snapshot(path, tLong)
	wantKind(t, err, ErrUnexpected)
	server.Request(t)
}

func TestSnapshotRejectsAResultOfAnotherType(t *testing.T) {
	reply := `{"id":"contr-snapshot","result":{"type":"pong","version":"0.9.3","protocol":22}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := Snapshot(path, tLong)
	wantKind(t, err, ErrUnexpected)
	server.Request(t)
}

func TestSubscriptionAsksForGlobalAndPaneEvents(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{ack, event}, 0)
	stream, err := OpenSubscription(path, []string{"w1:p1", "w1:p2"}, tLong)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if ok, err := stream.NextEvent(tLong); err != nil || !ok {
		t.Fatalf("event: %v %v", ok, err)
	}
	_, err = stream.NextEvent(WaitForever)
	wantKind(t, err, ErrClosed)

	raw := server.Request(t)
	request := requestJSON(t, raw)
	if request["id"] != "contr-sub" || request["method"] != "events.subscribe" {
		t.Fatalf("%v", request)
	}
	params, _ := request["params"].(map[string]any)
	subs, _ := params["subscriptions"].([]any)
	if len(GlobalEvents) != 19 || len(subs) != 21 {
		t.Fatalf("%d events, %d subscriptions", len(GlobalEvents), len(subs))
	}
	for i, kind := range GlobalEvents {
		want := map[string]any{"type": kind}
		if !reflect.DeepEqual(subs[i], want) {
			t.Fatalf("sub %d = %v, want %v", i, subs[i], want)
		}
	}
	for i, id := range []string{"w1:p1", "w1:p2"} {
		want := map[string]any{"type": "pane.agent_status_changed", "pane_id": id}
		if !reflect.DeepEqual(subs[19+i], want) {
			t.Fatalf("sub %d = %v, want %v", 19+i, subs[19+i], want)
		}
	}
	// Byte for byte: the keys of one pane subscription are sorted.
	if !strings.Contains(raw, `{"pane_id":"w1:p1","type":"pane.agent_status_changed"}`) {
		t.Fatalf("pane subscription text: %s", raw)
	}
}

func TestSubscriptionQuietPollIsNotAnError(t *testing.T) {
	path, server := serve(t, tempDir(t), []string{ack}, 300*time.Millisecond)
	stream, err := OpenSubscription(path, nil, tLong)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if ok, err := stream.NextEvent(poll); err != nil || ok {
		t.Fatalf("quiet poll: %v %v", ok, err)
	}
	for {
		ok, err := stream.NextEvent(poll)
		if err == nil && !ok {
			continue
		}
		wantKind(t, err, ErrClosed)
		break
	}
	server.Request(t)
}

func TestAShutdownOnTheWakeHandleEndsAReadWithNoTimer(t *testing.T) {
	// herdr keeps the stream open and quiet for 5 s.
	path, _ := serve(t, tempDir(t), []string{ack}, 5*time.Second)
	stream, err := OpenSubscription(path, nil, tLong)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	started := time.Now()
	woke := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		woke <- stream.Wake()
	}()
	_, err = stream.NextEvent(WaitForever)
	wantKind(t, err, ErrClosed)
	if elapsed := time.Since(started); elapsed >= tLong {
		t.Fatalf("the read ended %v after the start, not on the shutdown", elapsed)
	}
	if err := <-woke; err != nil {
		t.Fatalf("wake: %v", err)
	}
}

func TestSubscriptionOpenFailsOnPaneNotFound(t *testing.T) {
	reply := `{"id":"contr-sub","error":{"code":"pane_not_found","message":"pane w9:p99 not found"}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := OpenSubscription(path, []string{"w9:p99"}, tLong)
	we := wantKind(t, err, ErrServer)
	if we.Code != "pane_not_found" {
		t.Fatalf("%q", we.Code)
	}
	server.Request(t)
}

func TestSubscriptionOpenFailsWhenClosedBeforeAck(t *testing.T) {
	path, server := serve(t, tempDir(t), nil, 0)
	_, err := OpenSubscription(path, nil, tLong)
	wantKind(t, err, ErrClosed)
	server.Request(t)
}

func TestSubscriptionOpenTimesOutWithoutAck(t *testing.T) {
	path, server := serve(t, tempDir(t), nil, 600*time.Millisecond)
	_, err := OpenSubscription(path, nil, short)
	we := wantKind(t, err, ErrIO)
	if !errors.Is(we, os.ErrDeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	server.Request(t)
}

func TestSubscriptionOpenFailsWhenAckHasAnotherType(t *testing.T) {
	reply := `{"id":"contr-sub","result":{"type":"pong"}}`
	path, server := serve(t, tempDir(t), []string{reply}, 0)
	_, err := OpenSubscription(path, nil, tLong)
	wantKind(t, err, ErrUnexpected)
	server.Request(t)
}
