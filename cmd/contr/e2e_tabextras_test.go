//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// barTab is one tab of the title bar as the automation tree shows it.
type barTab struct {
	Label    string
	Bounds   rect
	Selected bool
}

// barTabs returns the tabs in the 46px title bar, left to right.
func (c *client) barTabs(t *testing.T) []barTab {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Label  string
			Role   string
			States []string
			Bounds rect
		}
	}
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	var tabs []barTab
	for _, n := range tree.Nodes {
		if n.Role == "tab" && n.Bounds.Y+n.Bounds.Height <= 46 {
			tabs = append(tabs, barTab{n.Label, n.Bounds, slices.Contains(n.States, "checked")})
		}
	}
	sort.Slice(tabs, func(i, j int) bool { return tabs[i].Bounds.X < tabs[j].Bounds.X })
	return tabs
}

// TestTabExtrasCapture runs the built app (Light) against a scratch hc core and a scratch HOME.
// The project has an orchestrator and three plain terminals. A middle click closes a plain tab
// and does nothing on the agent's tab; a drag moves a plain tab and cannot move the agent's tab.
// The context menu is a system menu: the automation plugin cannot open or capture one, so the
// unit tests cover it (ui.Tester). PNG: tmp/e2e/tabextras-light.png. Run:
//
//	go test -tags e2e -run TestTabExtrasCapture ./cmd/contr
func TestTabExtrasCapture(t *testing.T) {
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

	core := hctest.New(t, "contr-te", hctest.Session("te"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "demo")
	for _, d := range []string{home, repo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	titlebarGit(t, repo, "init", "-b", "main")
	titlebarGit(t, repo, "commit", "--allow-empty", "-m", "start")
	settings := filepath.Join(home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(settings, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"version": 1, "appearance": "light"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["quiet"] = agents.Profile{Argv: []string{"sh", "-c", "exec sleep 600"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	if _, err := mgr.Spawn(ctx, agents.Spec{Name: "lead", Profile: "quiet", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo}); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "te.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()
	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)

	wake := func() {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
	}
	waitTabs := func(what string, ok func([]barTab) bool) []barTab {
		t.Helper()
		var tabs []barTab
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			wake()
			if tabs = c.barTabs(t); ok(tabs) {
				return tabs
			}
		}
		t.Fatalf("%s; tabs %+v", what, tabs)
		return nil
	}
	input := func(events ...map[string]any) {
		t.Helper()
		c.call(t, "input", map[string]any{"events": events})
		time.Sleep(500 * time.Millisecond)
	}
	center := func(b rect) (float64, float64) { return b.X + b.Width/2, b.Y + b.Height/2 }
	clickTab := func(b rect, button string) {
		t.Helper()
		x, y := center(b)
		input(map[string]any{"type": "click", "x": x, "y": y, "button": button})
	}
	plains := func() int {
		ws, err := mgr.List(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, w := range ws {
			if w.Plain() {
				n++
			}
		}
		return n
	}
	waitPlains := func(want int) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			if plains() == want {
				return
			}
		}
		t.Fatalf("hc has %d plain terminals, want %d", plains(), want)
	}

	waitTabs("the orchestrator's tab", func(tabs []barTab) bool { return len(tabs) == 1 && tabs[0].Label == "lead" })
	for n := 1; n <= 3; n++ {
		b := c.boundsOf(t, titlebar.LabelNewTab)
		x, y := center(b)
		input(map[string]any{"type": "click", "x": x, "y": y})
		waitPlains(n)
	}
	tabs := waitTabs("lead and three plain tabs", func(tabs []barTab) bool { return len(tabs) == 4 })
	if tabs[0].Label != "lead" {
		t.Fatalf("the agent's tab is not first: %+v", tabs)
	}

	// Middle click on the agent's tab: nothing closes (D85).
	clickTab(tabs[0].Bounds, "middle")
	time.Sleep(time.Second)
	if got := len(c.barTabs(t)); got != 4 || plains() != 3 {
		t.Errorf("a middle click on the agent tab left %d tabs and %d plain terminals", got, plains())
	}
	// Middle click on the last plain tab: it closes and hc stops the terminal.
	clickTab(tabs[3].Bounds, "middle")
	waitPlains(2)
	tabs = waitTabs("three tabs after the middle click", func(tabs []barTab) bool { return len(tabs) == 3 })
	if tabs[0].Label != "lead" {
		t.Errorf("the agent's tab is not first after a close: %+v", tabs)
	}

	// Select the first plain tab, then drag it onto the second one: the selected tab is now last.
	clickTab(tabs[1].Bounds, "left")
	tabs = waitTabs("the first plain tab selected", func(tabs []barTab) bool { return len(tabs) == 3 && tabs[1].Selected })
	x1, y1 := center(tabs[1].Bounds)
	x2, y2 := center(tabs[2].Bounds)
	input(map[string]any{"type": "pointerDown", "x": x1, "y": y1}, map[string]any{"type": "pointerMove", "x": x1 + 12, "y": y1})
	input(map[string]any{"type": "pointerMove", "x": x2, "y": y2}, map[string]any{"type": "pointerUp", "x": x2, "y": y2})
	tabs = waitTabs("the selected tab moved to the end", func(tabs []barTab) bool { return len(tabs) == 3 && tabs[2].Selected })
	if tabs[0].Label != "lead" || tabs[1].Selected {
		t.Errorf("after the drag the tabs are %+v", tabs)
	}
	// Dragging the agent's tab onto a plain tab moves nothing.
	xa, ya := center(tabs[0].Bounds)
	x1, y1 = center(tabs[1].Bounds)
	input(map[string]any{"type": "pointerDown", "x": xa, "y": ya}, map[string]any{"type": "pointerMove", "x": xa + 12, "y": ya})
	input(map[string]any{"type": "pointerMove", "x": x1, "y": y1}, map[string]any{"type": "pointerUp", "x": x1, "y": y1})
	time.Sleep(time.Second)
	if after := c.barTabs(t); len(after) != 3 || after[0].Label != "lead" || !after[2].Selected {
		t.Errorf("dragging the agent tab changed the bar: %+v", after)
	}
	if plains() != 2 {
		t.Errorf("hc has %d plain terminals, want 2", plains())
	}
	time.Sleep(time.Second)
	c.capture(t, filepath.Join(out, "tabextras-light.png"))
}
