//go:build e2e

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFrameWindowCapture runs the built app against the dev herdr, checks the
// frame's elements and saves the window as tmp/e2e/frame.png (and
// frame-no-panel.png after the panel toggle). Start the dev herdr first:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags e2e -run TestFrameWindowCapture ./cmd/contr
//
// It refuses to run against any herdr but the dev one.
func TestFrameWindowCapture(t *testing.T) {
	herdrSock := os.Getenv("HERDR_SOCKET_PATH")
	if !strings.Contains(herdrSock, "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tmp", "e2e")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(out, "contr-e2e")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}

	// A short path: macOS limits socket paths to 103 bytes.
	dir, err := os.MkdirTemp("", "contr-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	// The app must not read the real usage files.
	app.Env = append(os.Environ(), "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	c.call(t, "idle", map[string]any{"timeout": 10000})

	labels := c.labels(t)
	for _, want := range []string{"Toggle sidebar", "Toggle panel", "contr", "New tab", "Split pane"} {
		if !labels[want] {
			t.Errorf("no element %q; labels %v", want, keys(labels))
		}
	}
	c.capture(t, filepath.Join(out, "frame.png"))

	// "New tab" sends tab.create to the dev herdr; the tree then holds one more tab.
	before := c.tabCount(t)
	c.call(t, "act", map[string]any{"id": c.idOf(t, "New tab"), "action": "press"})
	deadline := time.Now().Add(10 * time.Second)
	for c.tabCount(t) <= before && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if got := c.tabCount(t); got <= before {
		t.Errorf("tabs %d after New tab, want more than %d", got, before)
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	c.capture(t, filepath.Join(out, "frame-tabs.png"))

	c.call(t, "act", map[string]any{"id": c.idOf(t, "Toggle panel"), "action": "press"})
	c.call(t, "idle", map[string]any{"timeout": 10000})
	c.capture(t, filepath.Join(out, "frame-no-panel.png"))
}

// TestBoardPanelCapture shows a scratch board in the panel and saves the
// window as tmp/e2e/board.png (header, sections and the card) and
// board-report-tab.png (the Report tab selected). Row picks and the card's
// buttons come with the pick slice.
// It marks one pane of the dev herdr as the bridge and clears the mark at the
// end. Start the dev herdr first:
//
//	eval "$(scripts/dev-herdr.sh)"
//	go test -tags e2e -run TestBoardPanelCapture ./cmd/contr
//
// It refuses to run against any herdr but the dev one.
func TestBoardPanelCapture(t *testing.T) {
	herdrSock := os.Getenv("HERDR_SOCKET_PATH")
	if !strings.Contains(herdrSock, "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tmp", "e2e")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(out, "contr-e2e")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}

	// A short path: macOS limits socket paths to 103 bytes.
	dir, err := os.MkdirTemp("", "contr-e2e")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// The scratch board: the board fixture, under its own session name.
	const session = "e2e-board"
	fixture, err := os.ReadFile(filepath.Join(root, "internal", "board", "testdata", "board_fixture_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(fixture, &doc); err != nil {
		t.Fatal(err)
	}
	doc["session"] = session
	data, _ := json.Marshal(doc)
	boardDir := filepath.Join(dir, "board")
	if err := os.MkdirAll(boardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boardDir, session+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Mark one pane of the dev herdr as the bridge.
	pane := lastPane(t)
	herdrCmd(t, "pane", "report-metadata", pane, "--source", "e2e", "--token", "contr_board=1 "+session+" 42")
	defer func() {
		_ = exec.Command("herdr", "pane", "report-metadata", pane, "--source", "e2e", "--clear-token", "contr_board").Run()
	}()

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), "MYGO_AUTOMATION="+sock, "CONTR_BOARD_DIR="+boardDir,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(15 * time.Second)
	for !c.labels(t)["board header"] || !c.hasPrefix(t, "#640") {
		if time.Now().After(deadline) {
			t.Fatalf("the board did not show; labels %v", keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "board.png"))

	// The card of an item is named after the item (ItemCardLabel), and so is
	// the item's row in the needs list (NeedLabel). Two nodes with this label
	// mean the row and the card are both shown.
	const itemLabel = "T12 Test the bag drag,"
	if n := c.countPrefix(t, itemLabel); n < 2 {
		t.Errorf("want the needs row and the answer card labelled %q, found %d node(s)", itemLabel, n)
	}

	c.call(t, "act", map[string]any{"id": c.idOf(t, "Report tab"), "action": "press"})
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "board-report-tab.png"))
}

// herdrCmd runs the herdr command line. The test runs it only against the
// dev herdr, whose socket the environment names.
func herdrCmd(t *testing.T, args ...string) []byte {
	t.Helper()
	msg, err := exec.Command("herdr", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("herdr %v: %v\n%s", args, err, msg)
	}
	return msg
}

// lastPane returns the id of the last pane of the dev herdr.
func lastPane(t *testing.T) string {
	t.Helper()
	var res struct {
		Result struct {
			Panes []struct {
				PaneID string `json:"pane_id"`
			}
		}
	}
	if err := json.Unmarshal(herdrCmd(t, "pane", "list"), &res); err != nil || len(res.Result.Panes) == 0 {
		t.Fatalf("pane list: %v", err)
	}
	return res.Result.Panes[len(res.Result.Panes)-1].PaneID
}

func (c *client) hasPrefix(t *testing.T, prefix string) bool {
	t.Helper()
	return c.countPrefix(t, prefix) > 0
}

// countPrefix counts the nodes whose label starts with prefix.
func (c *client) countPrefix(t *testing.T, prefix string) int {
	t.Helper()
	n := 0
	for _, nd := range c.nodes(t) {
		if strings.HasPrefix(nd.Label, prefix) {
			n++
		}
	}
	return n
}

type client struct {
	conn net.Conn
	r    *bufio.Reader
	next int
}

func dial(t *testing.T, sock string) *client {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			return &client{conn: conn, r: bufio.NewReaderSize(conn, 1<<26)}
		}
		if time.Now().After(deadline) {
			t.Fatalf("automation socket: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitWindow waits until the app has opened its window.
func (c *client) waitWindow(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		var res struct{ Windows []json.RawMessage }
		if err := json.Unmarshal(c.call(t, "windows", map[string]any{}), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Windows) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the app opened no window")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (c *client) call(t *testing.T, method string, params map[string]any) json.RawMessage {
	t.Helper()
	c.next++
	req, _ := json.Marshal(map[string]any{"id": c.next, "method": method, "params": params})
	if _, err := c.conn.Write(append(req, '\n')); err != nil {
		t.Fatal(err)
	}
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct{ Message string }
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("%s: %s", method, resp.Error.Message)
	}
	return resp.Result
}

type node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func (c *client) nodes(t *testing.T) []node {
	t.Helper()
	var tree struct{ Nodes []node }
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	return tree.Nodes
}

func (c *client) labels(t *testing.T) map[string]bool {
	m := map[string]bool{}
	for _, n := range c.nodes(t) {
		m[n.Label] = true
	}
	return m
}

// tabCount counts the tab buttons in the window: their labels end in a status word.
func (c *client) tabCount(t *testing.T) int {
	t.Helper()
	n := 0
	for _, nd := range c.nodes(t) {
		for _, status := range []string{", idle", ", working", ", blocked", ", done"} {
			if strings.HasSuffix(nd.Label, status) {
				n++
			}
		}
	}
	return n
}

func (c *client) idOf(t *testing.T, label string) string {
	t.Helper()
	for _, n := range c.nodes(t) {
		if n.Label == label {
			return n.ID
		}
	}
	t.Fatalf("no element %q", label)
	return ""
}

func (c *client) capture(t *testing.T, path string) {
	t.Helper()
	var res struct{ PNG string }
	if err := json.Unmarshal(c.call(t, "capture", map[string]any{}), &res); err != nil {
		t.Fatal(err)
	}
	png, err := base64.StdEncoding.DecodeString(res.PNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("captured %s", path)
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
