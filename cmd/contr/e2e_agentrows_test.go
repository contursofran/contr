//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestAgentRows runs the built app against a scratch hc core and a scratch git
// project "demo" with an orchestrator and two workers in their own worktrees.
// The agents are shell scripts that print what Claude Code prints; their
// profiles carry `--model` so line 2 shows a model. It captures the sidebar in
// Dark and Light, clicks a worker, and waits for the one-minute timer to move
// the ages. PNGs go to tmp/e2e/agentrows-*.png:
//
//	go test -tags e2e -run TestAgentRows ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core.
func TestAgentRows(t *testing.T) {
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
	core := hctest.New(t, "contr-ar", hctest.Session("ar"))
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
	wt241, wt242 := filepath.Join(dir, "demo-b-241"), filepath.Join(dir, "demo-b-242")
	git("worktree", "add", "-q", "-b", "agent/241-notch", wt241)
	git("worktree", "add", "-q", "-b", "agent/242-outline", wt242)

	// What a Claude Code agent shows, with the --model that the row reads.
	script := `printf '\n\342\227\217 Reading the spec before I start.\n\n\342\225\255\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\256\n\342\224\202 > _      \342\224\202\n\342\225\260\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\257\n'; exec sleep 600`
	// A title set by an OSC sequence is the worker's task on line 1 of its row.
	claude := func(model, title string) agents.Profile {
		s := script
		if title != "" {
			s = `printf '\033]0;` + title + `\007'; ` + script
		}
		return agents.Profile{Argv: []string{"sh", "-c", s, "claude", "--model", model, "--effort", "medium"}}
	}
	cfg := agents.DefaultConfig()
	cfg.Profiles["orchestrator"] = claude("opus", "")
	cfg.Profiles["builder"] = claude("sonnet", "Agent rows on ui.List")
	cfg.Profiles["builder-bare"] = claude("sonnet", "") // no title: line 1 is the name
	cfgPath := filepath.Join(dir, "agents.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+cfgPath)...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
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
	waitFor := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
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
		waitFor("settings", 20*time.Second, func() bool { return has(app.LabelSettings) })
		click(appearance)
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		waitFor("settings closed", 20*time.Second, func() bool { return !has(app.LabelSettings) })
		time.Sleep(600 * time.Millisecond)
	}

	// The agents start after the app, so hc events stamp them (D35) and the ages show.
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
	for _, s := range []agents.Spec{
		{Name: "b-241", Profile: "builder", Role: "builder", Project: "demo", Issue: "241", Worktree: wt241, Parent: lead.Address},
		{Name: "b-242", Profile: "builder-bare", Role: "builder", Project: "demo", Issue: "242", Worktree: wt242, Parent: lead.Address},
	} {
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	rows := []string{"lead, ", "b-241, ", "b-242, "}
	waitRows := func() {
		t.Helper()
		for _, prefix := range rows {
			waitFor("row "+prefix, 30*time.Second, func() bool { return c.labelWithPrefix(t, prefix) != "" })
		}
		time.Sleep(1500 * time.Millisecond) // the terminal attaches
	}

	pick("Dark")
	waitRows()
	t.Logf("labels: %v", keys(c.labels(t)))
	c.capture(t, filepath.Join(out, "agentrows-dark.png"))
	// The click selects the worker: its tab shows in the title bar. Click again
	// if the first one came before the rows settled.
	for try := 0; try < 4 && c.labelWithPrefix(t, "241 · demo") == ""; try++ {
		click(c.labelWithPrefix(t, "b-241, "))
		time.Sleep(1500 * time.Millisecond)
	}
	if c.labelWithPrefix(t, "241 · demo") == "" {
		t.Fatalf("a click on the worker row did not select it; labels %v", keys(c.labels(t)))
	}
	c.capture(t, filepath.Join(out, "agentrows-dark-worker.png"))

	// ↑ from the worker row moves the selection to the orchestrator (D145).
	c.call(t, "act", map[string]any{"id": c.idOf(t, app.LabelProjectList), "action": "focus"})
	c.key(t, "Up")
	waitFor("Up selects the orchestrator", 20*time.Second, func() bool { return c.labelWithPrefix(t, "241 · demo") == "" })
	time.Sleep(800 * time.Millisecond)
	c.capture(t, filepath.Join(out, "agentrows-dark-keys.png"))

	pick("Light")
	click(c.labelWithPrefix(t, "lead, "))
	waitFor("the orchestrator tab", 20*time.Second, func() bool { return c.labelWithPrefix(t, "241 · demo") == "" })
	time.Sleep(1500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "agentrows-light.png"))
	// ↓ from the orchestrator row moves the selection to the first worker.
	c.call(t, "act", map[string]any{"id": c.idOf(t, app.LabelProjectList), "action": "focus"})
	c.key(t, "Down")
	waitFor("Down selects the worker", 20*time.Second, func() bool { return c.labelWithPrefix(t, "241 · demo") != "" })
	time.Sleep(800 * time.Millisecond)
	c.capture(t, filepath.Join(out, "agentrows-light-keys.png"))

	// The age timer: a minute after the agents started, with no other event, the
	// ages read "1m". Text is not a label, so the capture is the check.
	time.Sleep(75 * time.Second)
	c.capture(t, filepath.Join(out, "agentrows-light-1m.png"))
}
