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

	"github.com/contursofran/contr/internal/hc/hctest"
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
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc is not installed")
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
	core := hctest.New(t, "contr-fr", hctest.Session("fr"))
	for _, name := range []string{"dark", "light"} {
		dir := filepath.Join(core.Dir, name)
		home := filepath.Join(dir, "home")
		cfg := filepath.Join(home, "Library", "Application Support", "contr")
		if err := os.MkdirAll(cfg, 0o755); err != nil {
			t.Fatal(err)
		}
		settings := `{"version": 1, "appearance": "` + name + `"}`
		if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(settings), 0o644); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(core.Dir, name[:1]+".sock")
		app := exec.Command(bin)
		// The app must not read the real usage files or settings.
		app.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home,
			"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
		if err := app.Start(); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() { _ = app.Process.Kill(); _ = app.Wait() }()
			c := dial(t, sock)
			defer c.conn.Close()
			c.waitWindow(t)
			c.call(t, "idle", map[string]any{"timeout": 10000})

			labels := c.labels(t)
			for _, want := range []string{"Toggle sidebar", "Hide files", "Sidebar", "Terminal", "Files", "New terminal"} {
				if !labels[want] {
					t.Errorf("%s: no element %q; labels %v", name, want, keys(labels))
				}
			}
			sidebar, files, term := c.boundsOf(t, "Sidebar"), c.boundsOf(t, "Files"), c.boundsOf(t, "Terminal")
			if sidebar.Width != 312 || files.Width != 280 || term.Width < 480 || sidebar.X != 10 {
				t.Errorf("%s: sidebar %+v, terminal %+v, files %+v", name, sidebar, term, files)
			}
			c.capture(t, filepath.Join(out, "frame-"+name+".png"))

			c.call(t, "act", map[string]any{"id": c.idOf(t, "Hide files"), "action": "press"})
			c.call(t, "idle", map[string]any{"timeout": 10000})
			if c.labels(t)["Files"] {
				t.Errorf("%s: the files panel shows after the toggle", name)
			}
			c.capture(t, filepath.Join(out, "frame-"+name+"-no-files.png"))
			c.call(t, "act", map[string]any{"id": c.idOf(t, "Hide files"), "action": "press"})
			c.call(t, "idle", map[string]any{"timeout": 10000})
			if got := c.boundsOf(t, "Files").Width; got != 280 {
				t.Errorf("%s: files %v wide after showing again, want 280", name, got)
			}
		}()
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
