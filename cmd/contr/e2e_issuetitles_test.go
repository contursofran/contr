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

// TestIssueTitlesCapture is TestSidebarChromeCapture with a fake gh on PATH that answers
// `issue view N --json title`, so line 1 of a worker row shows its issue title. The empty
// project is first in the stored order and still shows last. It captures the window in Dark and Light (#443). PNGs go to
// tmp/e2e/issuetitles-*.png:
//
//	go test -tags e2e -run TestIssueTitlesCapture ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core.
func TestIssueTitlesCapture(t *testing.T) {
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
	cfg.Profiles["builder"] = claude("sonnet", "")
	cfg.Profiles["builder-bare"] = claude("sonnet", "") // no title: line 1 is the name
	cfgPath := filepath.Join(dir, "agents.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// A second project with an orchestrator, and an empty stored project.
	other, idle := filepath.Join(dir, "other"), filepath.Join(dir, "idle")
	for _, r := range []string{other, idle} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "init", "-q", "-b", "main")
		cmd.Dir = r
		cmd.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, msg)
		}
	}
	store := filepath.Join(home, "Library", "Application Support", "contr")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(idle); err == nil {
		idle = real
	}
	pj, _ := json.Marshal(map[string]any{"version": 1, "order": []string{filepath.Join(idle, ".git")}, "hidden": []string{}, "recent": []any{},
		"folders": map[string]string{filepath.Join(idle, ".git"): idle}})
	if err := os.WriteFile(filepath.Join(store, "projects.json"), pj, 0o600); err != nil {
		t.Fatal(err)
	}

	// A fake gh: the issue titles come from here, never from the network.
	bindir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\ncase \"$*\" in\n*\"issue view 241 --json title\"*) echo '{\"title\":\"Notch above the terminal\"}' ;;\n*\"issue view 242 --json title\"*) echo '{\"title\":\"Outline on the right edge\"}' ;;\n*) echo 'unknown' >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bindir, "gh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"PATH="+bindir+":"+os.Getenv("PATH"), "CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+cfgPath)...)
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

	if _, err := mgr.Spawn(ctx, agents.Spec{Name: "lead2", Profile: "orchestrator", Role: agents.RoleOrchestrator, Project: "other", Worktree: other}); err != nil {
		t.Fatal(err)
	}
	rows := []string{"lead, ", "b-241, ", "b-242, ", "lead2, "}
	waitRows := func() {
		t.Helper()
		for _, prefix := range rows {
			waitFor("row "+prefix, 30*time.Second, func() bool { return c.labelWithPrefix(t, prefix) != "" })
		}
		time.Sleep(1500 * time.Millisecond) // the terminal attaches
	}

	pick("Dark")
	waitRows()
	waitFor("the empty project", 30*time.Second, func() bool { return c.labelWithPrefix(t, "idle, ") != "" })
	c.capture(t, filepath.Join(out, "issuetitles-dark.png"))
	pick("Light")
	time.Sleep(1500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "issuetitles-light.png"))
}
