//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestQuestionsNotch runs the built app against a scratch hc core with an
// orchestrator and a worker of the project "demo", and a scratch owner file
// with two questions and one to-do for "demo" and one question for "other".
// It captures the notch over the orchestrator and over the worker, rewrites the
// owner file (the notch follows the file watch), switches the theme, and clicks
// the notch to open the card. PNGs go to tmp/e2e/notch-*.png:
//
//	go test -tags e2e -run TestQuestionsNotch ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core
// or owner file.
func TestQuestionsNotch(t *testing.T) {
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
	core := hctest.New(t, "contr-nt", hctest.Session("nt"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	// What a Claude Code worker shows while it waits for CI.
	script := `printf '\n\342\227\217 Bash(gh pr checks 250)\n  \342\216\277  build   pass   1m12s\n  \342\216\277  vet     pass   8s\n  \342\216\277  e2e     pending\n\n'; ` +
		`printf '\342\227\217 Waiting for the e2e check before I report the PR.\n\n\342\225\255\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\256\n\342\224\202 > _          \342\224\202\n\342\225\260\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\257\n'; exec sleep 600`
	cfg := agents.DefaultConfig()
	cfg.Profiles["claude-like"] = agents.Profile{Argv: []string{"sh", "-c", script}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	repo := scratchRepo(t, dir, "demo", "main")
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "claude-like", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo},
		{Name: "b-250-checks", Profile: "claude-like", Role: "builder", Project: "demo", Issue: "250", Worktree: repo},
	} {
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	ownerFile := filepath.Join(dir, "owner.json")
	const questions = `{"id":"D1","project":"demo","title":"b-250 checks: merge now?","question":"CI for #250 is green except e2e. Merge now or wait for e2e?","options":["merge now","wait for e2e"],"recommended":1,"asked":"2026-11-05T10:00:00Z"},
{"id":"D2","project":"demo","title":"b-251 retry: keep the old name?","question":"Keep the old name for the retry flag?","options":["keep","rename"],"asked":"2026-11-05T10:01:00Z"},
{"id":"D3","project":"other","title":"b-9 other project","question":"Not for demo.","options":["yes","no"],"asked":"2026-11-05T10:02:00Z"}`
	const todo = `{"id":"T1","project":"demo","kind":"do","text":"Test the notch on the Mac","pinned":"2026-11-05T10:03:00Z"}`
	write := func(withTodo bool) {
		t.Helper()
		todos := ""
		if withTodo {
			todos = todo
		}
		doc := `{"questions":[` + questions + `],"todos":[` + todos + `],"next_question":4,"next_todo":2}`
		tmp := ownerFile + ".tmp"
		if err := os.WriteFile(tmp, []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, ownerFile); err != nil {
			t.Fatal(err)
		}
	}
	write(true)

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+ownerFile, "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
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
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
			if ok() {
				return
			}
		}
		t.Fatalf("timeout: %s; labels %v", what, keys(c.labels(t)))
	}
	selectAgent := func(prefix string) {
		t.Helper()
		row := ""
		waitFor("row "+prefix, func() bool { row = c.labelWithPrefix(t, prefix); return row != "" })
		click(row)
		time.Sleep(1500 * time.Millisecond) // the terminal attaches
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
	waitNotch := func(label string) {
		t.Helper()
		waitFor("notch "+label, func() bool { return has(label) })
		time.Sleep(500 * time.Millisecond)
	}

	pick("Dark")
	selectAgent("lead, ")
	waitNotch("3 items. Open") // two questions and one to-do of demo; the question of "other" does not count
	c.capture(t, filepath.Join(out, "notch-dark-orchestrator.png"))
	selectAgent("b-250-checks, ")
	waitNotch("3 items. Open")
	c.capture(t, filepath.Join(out, "notch-dark-worker.png"))

	write(false)
	waitNotch("2 questions. Open")
	c.capture(t, filepath.Join(out, "notch-dark-2questions.png"))

	pick("Light")
	waitNotch("2 questions. Open")
	c.capture(t, filepath.Join(out, "notch-light-2questions.png"))
	selectAgent("lead, ")
	write(true)
	waitNotch("3 items. Open")
	c.capture(t, filepath.Join(out, "notch-light-orchestrator.png"))

	click("3 items. Open")
	waitFor("questions card", func() bool { return has(app.LabelQuestionsField) })
	c.capture(t, filepath.Join(out, "notch-light-card.png"))
}
