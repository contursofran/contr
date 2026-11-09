//go:build e2e

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestFrameWindowCapture runs the built app against a scratch hc core in Dark and in Light,
// checks the three panels and the files toggle, and saves the window as
// tmp/e2e/frame-<theme>.png (and frame-<theme>-no-files.png after the toggle):
//
//	go test -tags e2e -run TestFrameWindowCapture ./cmd/contr
//
// HOME and the config folders are scratch folders, so the owner's settings and widths stay as
// they are. It never touches the owner's hc core.
func TestFrameWindowCapture(t *testing.T) {
	h := newHarness(t)
	for _, name := range []string{"dark", "light"} {
		h.theme(t, name)
		c := h.launch(t)
		labels := c.labels(t)
		for _, want := range []string{"Hide files", "Sidebar", "Terminal", "Files", "New tab"} {
			if !labels[want] {
				t.Errorf("%s: no element %q; labels %v", name, want, keys(labels))
			}
		}
		if labels["Toggle sidebar"] || labels["Show sidebar"] {
			t.Errorf("%s: the title bar has a sidebar toggle (#437)", name)
		}
		sidebar, files, term := c.boundsOf(t, "Sidebar"), c.boundsOf(t, "Files"), c.boundsOf(t, "Terminal")
		if sidebar.Width != 312 || files.Width != 280 || term.Width < 480 || sidebar.X != 8 { // D168: 8 pt from the edge
			t.Errorf("%s: sidebar %+v, terminal %+v, files %+v", name, sidebar, term, files)
		}
		h.capture(t, c, "frame-"+name+".png")

		c.press(t, "Hide files")
		if c.labels(t)["Files"] {
			t.Errorf("%s: the files panel shows after the toggle", name)
		}
		h.capture(t, c, "frame-"+name+"-no-files.png")
		c.press(t, "Hide files")
		if got := c.boundsOf(t, "Files").Width; got != 280 {
			t.Errorf("%s: files %v wide after showing again, want 280", name, got)
		}
	}
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
	guardFocus(t)
	deadline := time.Now().Add(30 * time.Second)
	for {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			guardActive(t, sock)
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
	Role  string `json:"role"`
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

// idOfRole is idOf for an element of one role, where a text and a control
// share a name (a Settings slider and its name on screen).
func (c *client) idOfRole(t *testing.T, role, label string) string {
	t.Helper()
	for _, n := range c.nodes(t) {
		if n.Role == role && n.Label == label {
			return n.ID
		}
	}
	t.Fatalf("no %s %q", role, label)
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
