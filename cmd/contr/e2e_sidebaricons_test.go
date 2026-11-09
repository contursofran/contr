//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestSidebarIcons runs the built app against a scratch hc core and a scratch
// git project "demo" with an orchestrator and a worker, first in a 760 wide
// window (the sidebar is an icon column, D128) and then in a 1440 wide one. It
// captures each in Dark and Light and clicks the worker in the icon column.
// PNGs go to tmp/e2e/sidebaricons-*.png:
//
//	go test -tags e2e -run TestSidebarIcons ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core.
func TestSidebarIcons(t *testing.T) {
	for _, tool := range []string{"hc", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
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
	core := hctest.New(t, "contr-si", hctest.Session("si"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "demo")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=demo", "GIT_AUTHOR_EMAIL=demo@example.com", "GIT_COMMITTER_NAME=demo", "GIT_COMMITTER_EMAIL=demo@example.com")
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, msg)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "start")
	wt241 := filepath.Join(dir, "demo-b-241")
	git("worktree", "add", "-q", "-b", "agent/241-notch", wt241)

	script := `printf '\n\342\227\217 Reading the spec before I start.\n\n\342\225\255\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\256\n\342\224\202 > _      \342\224\202\n\342\225\260\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\257\n'; exec sleep 600`
	claude := func(model string) agents.Profile {
		return agents.Profile{Argv: []string{"sh", "-c", script, "claude", "--model", model, "--effort", "medium"}}
	}
	cfg := agents.DefaultConfig()
	cfg.Profiles["orchestrator"] = claude("opus")
	cfg.Profiles["builder"] = claude("sonnet")
	cfgPath := filepath.Join(dir, "agents.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// run starts the app at the given window width and returns its client.
	run := func(width int) (*client, func()) {
		t.Helper()
		sock := filepath.Join(dir, "a"+strconv.Itoa(width)+".sock")
		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
			"CONTR_E2E_WIDTH="+strconv.Itoa(width),
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+cfgPath)...)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		c := dial(t, sock)
		c.waitWindow(t)
		return c, func() { c.conn.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	lead, err := mgr.Spawn(ctx, agents.Spec{Name: "lead", Profile: "orchestrator", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Spawn(ctx, agents.Spec{Name: "b-241", Profile: "builder", Role: "builder", Project: "demo", Issue: "241", Worktree: wt241, Parent: lead.Address}); err != nil {
		t.Fatal(err)
	}

	session := func(width int, narrow bool) {
		c, stop := run(width)
		defer stop()
		poke := func() {
			c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
		}
		click := func(label string) {
			t.Helper()
			b := c.boundsOf(t, label)
			c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		}
		has := func(label string) bool {
			for _, n := range c.nodes(t) {
				if n.Label == label {
					return true
				}
			}
			return false
		}
		waitFor := func(what string, ok func() bool) {
			t.Helper()
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
				poke()
				if ok() {
					return
				}
			}
			t.Fatalf("timeout: %s; labels %v", what, keys(c.labels(t)))
		}
		pick := func(appearance string) {
			t.Helper()
			c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
			waitFor("settings", func() bool { return has(settings.LabelSettings) })
			click(appearance)
			time.Sleep(400 * time.Millisecond)
			c.key(t, "Escape")
			waitFor("settings closed", func() bool { return !has(settings.LabelSettings) })
			time.Sleep(600 * time.Millisecond)
		}
		for _, prefix := range []string{"lead, ", "b-241, "} {
			waitFor("row "+prefix, func() bool { return c.labelWithPrefix(t, prefix) != "" })
		}
		time.Sleep(1500 * time.Millisecond)
		name := "wide"
		if narrow {
			name = "narrow"
			side := c.boundsOf(t, app.LabelPanelSidebar)
			if side.Width > 60 {
				t.Fatalf("sidebar %v wide in a %d window, want the icon column", side.Width, width)
			}
			for _, label := range []string{"lead, idle, opus title", "demo, main text", "This Mac, local text"} {
				if has(label) {
					t.Errorf("the icon column draws %q", label)
				}
			}
		}
		pick("Dark")
		c.capture(t, filepath.Join(out, "sidebaricons-"+name+"-dark.png"))
		pick("Light")
		c.capture(t, filepath.Join(out, "sidebaricons-"+name+"-light.png"))
		if narrow {
			// The click works in the icon column: the worker tab shows in the title bar.
			for try := 0; try < 4 && c.labelWithPrefix(t, "241 · demo") == ""; try++ {
				click(c.labelWithPrefix(t, "b-241, "))
				time.Sleep(1500 * time.Millisecond)
			}
			if c.labelWithPrefix(t, "241 · demo") == "" {
				t.Fatalf("a click on the worker glyph did not select it; labels %v", keys(c.labels(t)))
			}
			c.capture(t, filepath.Join(out, "sidebaricons-narrow-light-worker.png"))
		}
	}
	session(760, true)
	session(1440, false)
}
