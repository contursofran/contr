//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// harness is one scratch run of the app for an e2e test (#457, D178): a scratch
// hc core, a scratch home, scratch config files and the app built with the
// automation plugin. It never touches the owner's core, home or settings.
//
//	h := newHarness(t)
//	repo := h.repo(t, "demo", "main")
//	h.spawn(t, agents.Spec{Name: "lead", Profile: "claude-idle", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
//	h.theme(t, "light")
//	c := h.launch(t)
//	h.capture(t, c, "demo-light.png")
//
// Profiles: "sleepy" (a shell that prints "working" and sleeps), and
// "claude-idle" and "claude-working", a program that looks like Claude Code
// and tells hc its state.
type harness struct {
	root string // the repository
	out  string // tmp/e2e: binary and PNGs
	dir  string // the scratch folder
	home string
	env  []string // the scratch core's env; the app gets HOME and the contr files on top
	ctx  context.Context
	cl   *hc.Client
	mgr  *agents.Manager
	runs int // app launches, for a new socket each time
	proc *exec.Cmd
	c    *client
}

var (
	buildOnce sync.Once
	buildErr  error
)

// e2eBinary builds the app with the e2e tag once per test run and returns its
// path. The name contr-e2e is what guardFocus looks for.
func e2eBinary(t *testing.T, root, out string) string {
	t.Helper()
	bin := filepath.Join(out, "contr-e2e")
	buildOnce.Do(func() {
		build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=0")
		if msg, err := build.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build: %v\n%s", err, msg)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return bin
}

// newHarness builds the app, starts a scratch hc core and connects to it.
// It skips the test when hc is not installed.
func newHarness(t *testing.T) *harness {
	t.Helper()
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
	e2eBinary(t, root, out)

	// A short path: macOS limits socket paths to 103 bytes.
	core := hctest.New(t, "contr-h", hctest.Session("h"))
	h := &harness{root: root, out: out, dir: core.Dir, home: filepath.Join(core.Dir, "home"), env: core.Env}
	if err := os.MkdirAll(h.home, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	h.ctx = ctx
	cl, err := hc.Dial(ctx, hc.Command("", h.env))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	h.cl = cl

	claude := h.buildFakeClaude(t)
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "echo working; sleep 600"}}
	// sizer shows its grid size and a ruler as wide as the grid, and draws
	// them again on each resize.
	cfg.Profiles["sizer"] = agents.Profile{Argv: []string{"sh", "-c",
		`draw() { printf "\033[2J\033[H"; set -- $(stty size); echo "grid $2x$1"; i=0; while [ $i -lt $2 ]; do printf %d $((i % 10)); i=$((i+1)); done; echo; }; trap draw WINCH; draw; while :; do sleep 1; done`}}
	cfg.Profiles["claude-idle"] = agents.Profile{Argv: []string{claude, "idle"}}
	cfg.Profiles["claude-working"] = agents.Profile{Argv: []string{claude, "working"}}
	cfg.Profiles["claude-blocked"] = agents.Profile{Argv: []string{claude, "blocked:kind=permission:msg=Allow%20rm"}}
	// ended exits at once: hc keeps listing its terminal, as a worker that is done.
	cfg.Profiles["ended"] = agents.Profile{Argv: []string{"sh", "-c", "echo done"}}
	h.mgr = agents.New(agents.HC{Client: cl}, cfg)
	return h
}

// buildFakeClaude builds fakeClaude (e2e_titlebar_test.go) into the scratch folder.
func (h *harness) buildFakeClaude(t *testing.T) string {
	t.Helper()
	src := filepath.Join(h.dir, "fake")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"main.go": fakeClaude, "go.mod": "module fake\n\ngo 1.27.3\n"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Named "claude": the app reads a worker's menu only from a claude command.
	// contr puts --settings before the profile arguments, so the fake reads its
	// state from the last argument (#532).
	bin := filepath.Join(h.dir, "claude")
	mk := exec.Command("go", "build", "-o", bin, ".")
	mk.Dir = src
	mk.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("build the fake claude: %v\n%s", err, msg)
	}
	return bin
}

// repo makes a git repository with one commit in the scratch folder.
func (h *harness) repo(t *testing.T, name, branch string) string {
	t.Helper()
	return scratchRepo(t, h.dir, name, branch)
}

// spawn starts workers in the scratch core. A spec without a profile gets "sleepy".
func (h *harness) spawn(t *testing.T, specs ...agents.Spec) {
	t.Helper()
	for _, s := range specs {
		if s.Profile == "" {
			s.Profile = "sleepy"
		}
		if _, err := h.mgr.Spawn(h.ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
		if want := fakeStates[s.Profile]; want != "" {
			h.waitStatus(t, s.Name, want)
		}
	}
}

// fakeStates is the status each fake Claude profile reports.
var fakeStates = map[string]string{"claude-working": "working", "claude-blocked": "blocked"}

// waitStatus waits until hc reports the status of the worker, so a fixture
// shows a state that is already set when the app starts.
func (h *harness) waitStatus(t *testing.T, name, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		w, err := h.mgr.Get(h.ctx, name)
		if err == nil && w.Status == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("worker %s: status %q (err %v), want %q", name, w.Status, err, want)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ownerFile is the questions and to-dos file the app reads.
func (h *harness) ownerFile() string { return filepath.Join(h.dir, "owner.json") }

// owner writes the questions and to-dos file (internal/owner's JSON).
func (h *harness) owner(t *testing.T, doc string) {
	t.Helper()
	if err := os.WriteFile(h.ownerFile(), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// settings writes the app's settings.json (internal/prefs's JSON).
func (h *harness) settings(t *testing.T, doc string) {
	t.Helper()
	dir := filepath.Join(h.home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
}

// theme sets the appearance: "dark", "light" or "system".
func (h *harness) theme(t *testing.T, name string) {
	t.Helper()
	h.settings(t, `{"version": 1, "appearance": "`+name+`"}`)
}

// appEnv is the env of the app: the scratch core, the scratch home and the
// contr files in the scratch folder, so the app reads no real state.
func (h *harness) appEnv(sock string) []string {
	return append(os.Environ(), append(slices.Clone(h.env),
		"MYGO_AUTOMATION="+sock, "HOME="+h.home, "SHELL=/bin/sh",
		"XDG_CONFIG_HOME="+filepath.Join(h.home, ".config"),
		"CONTR_STATE_DIR="+filepath.Join(h.dir, "state"), "CODEX_HOME="+filepath.Join(h.dir, "codex"),
		"CONTR_OWNER_FILE="+h.ownerFile(), "CONTR_AGENTS_CONFIG="+filepath.Join(h.dir, "agents.json"))...)
}

// launch starts the app and returns its automation client once the window is
// open and idle. A second launch quits the first app.
func (h *harness) launch(t *testing.T) *client {
	t.Helper()
	h.quit()
	h.runs++
	sock := filepath.Join(h.dir, fmt.Sprintf("a%d.sock", h.runs))
	proc := exec.Command(filepath.Join(h.out, "contr-e2e"))
	proc.Env = h.appEnv(sock)
	if err := proc.Start(); err != nil {
		t.Fatal(err)
	}
	h.proc = proc
	t.Cleanup(h.quit)
	h.c = dial(t, sock)
	h.c.waitWindow(t)
	h.c.settle(t)
	return h.c
}

// quit ends the app, if it runs.
func (h *harness) quit() {
	if h.c != nil {
		_ = h.c.conn.Close()
		h.c = nil
	}
	if h.proc != nil {
		_ = h.proc.Process.Kill()
		_ = h.proc.Wait()
		h.proc = nil
	}
}

// capture saves the window as tmp/e2e/<name>.
func (h *harness) capture(t *testing.T, c *client, name string) {
	t.Helper()
	c.capture(t, filepath.Join(h.out, name))
}

// fullNode is a node of the automation tree with the fields a dump shows.
type fullNode struct {
	ID      string   `json:"id"`
	Parent  string   `json:"parent"`
	Role    string   `json:"role"`
	Key     any      `json:"key"`
	Label   string   `json:"label"`
	Value   string   `json:"value"`
	Bounds  rect     `json:"bounds"`
	States  []string `json:"states"`
	Actions []string `json:"actions"`
}

func (c *client) fullNodes(t *testing.T) []fullNode {
	t.Helper()
	var tree struct{ Nodes []fullNode }
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	return tree.Nodes
}

// dump writes the automation tree as text: one element per line, indented
// under its parent, with role, label, value, key, box and states. An agent
// reads it to check the data on screen without the PNG.
func (c *client) dump(t *testing.T, path string) {
	t.Helper()
	nodes := c.fullNodes(t)
	depth := map[string]int{}
	var b strings.Builder
	for _, n := range nodes {
		d := 0
		if n.Parent != "" {
			d = depth[n.Parent] + 1
		}
		depth[n.ID] = d
		fmt.Fprintf(&b, "%s%s", strings.Repeat("  ", d), n.Role)
		if n.Label != "" {
			fmt.Fprintf(&b, " %q", n.Label)
		}
		if n.Value != "" {
			fmt.Fprintf(&b, " value=%q", n.Value)
		}
		if n.Key != nil {
			fmt.Fprintf(&b, " key=%v", n.Key)
		}
		r := n.Bounds
		fmt.Fprintf(&b, " @%s,%s %sx%s", num(r.X), num(r.Y), num(r.Width), num(r.Height))
		if len(n.States) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(n.States, " "))
		}
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("tree %s", path)
}

// num writes a point value with at most one decimal.
func num(f float64) string {
	return strconv.FormatFloat(math.Round(f*10)/10, 'f', -1, 64)
}

// press presses the element with this label, or else the first whose label
// starts with it: the "press" action when the element has one, else a click
// at its center. Then it waits until the app is idle.
func (c *client) press(t *testing.T, label string) {
	t.Helper()
	nodes := c.fullNodes(t)
	i := slices.IndexFunc(nodes, func(n fullNode) bool { return n.Label == label })
	if i < 0 {
		i = slices.IndexFunc(nodes, func(n fullNode) bool { return strings.HasPrefix(n.Label, label) })
	}
	if i < 0 {
		t.Fatalf("no element %q; labels %v", label, keys(c.labels(t)))
	}
	n := nodes[i]
	if slices.Contains(n.Actions, "press") {
		c.call(t, "act", map[string]any{"id": n.ID, "action": "press"})
	} else {
		b := n.Bounds
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
	}
	c.settle(t)
}

// settle waits until the window is idle, for at most 3 s. A view that keeps
// moving, as the spinner of a working agent, never gets idle: then it goes on
// after the 3 s.
func (c *client) settle(t *testing.T) {
	t.Helper()
	if msg := c.try(t, "idle", map[string]any{"timeout": 3000}); msg != "" && !strings.Contains(msg, "timeout") {
		t.Fatalf("idle: %s", msg)
	}
}

// try is call that returns the error message instead of failing the test.
func (c *client) try(t *testing.T, method string, params map[string]any) string {
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
		Error *struct{ Code, Message string }
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error == nil {
		return ""
	}
	return resp.Error.Code + ": " + resp.Error.Message
}
