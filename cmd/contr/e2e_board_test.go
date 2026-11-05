//go:build e2e

package main

import (
	"context"
	"github.com/contursofran/contr/internal/hc/hctest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// TestBoardCapture runs the built app against a scratch hc
// core with an orchestrator, a planner and a builder, one question and one
// to-do. With the orchestrator selected the panel shows the board
// (tmp/e2e/board.png):
//
//	go test -tags e2e -run TestBoardCapture ./cmd/contr
//
// It never touches the owner's hc core.
func TestBoardCapture(t *testing.T) {
	skipWhileViewersHidden(t)
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
	core := hctest.New(t, "contr-e2e", hctest.Session("e2e"))
	dir := core.Dir

	// The scratch core: its own session and XDG folders.
	env := core.Env

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "echo working; sleep 600"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "p162-plan", Profile: "sleepy", Role: "planner", Project: "demo", Issue: "170"},
		{Name: "b162-s6-board", Profile: "sleepy", Role: "builder", Project: "demo", Issue: "162"},
	} {
		s.Parent = ""
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	ownerFile := filepath.Join(dir, "owner.json")
	const doc = `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"The cache needs a store. Redis is fast but one more service; sqlite is already in the app. Which one should it use?","options":["sqlite","redis","files"],"recommended":0,"asked":"2026-11-03T10:00:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the bag drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":2,"next_todo":2}`
	if err := os.WriteFile(ownerFile, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+ownerFile, "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(15 * time.Second)
	for !(c.hasPrefix(t, "b162-s6-board,") && c.labels(t)["needs row"]) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	for _, n := range c.nodes(t) {
		if strings.HasPrefix(n.Label, "lead,") {
			c.call(t, "act", map[string]any{"id": n.ID, "action": "press"})
			break
		}
	}
	deadline = time.Now().Add(15 * time.Second)
	for !c.labels(t)["work rows"] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	if !c.labels(t)["work rows"] || !c.labels(t)["needs rows"] {
		t.Fatalf("the board did not show; labels %v", keys(c.labels(t)))
	}
	c.capture(t, filepath.Join(out, "board.png"))
}
