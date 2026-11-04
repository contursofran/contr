//go:build e2e

package main

import (
	"context"
	"github.com/contursofran/contr/internal/hc/hctest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// TestNeedsRowCapture shows the needs-you row with two questions and a to-do
// from a scratch owner.json and a scratch hc core, and saves the window as
// tmp/e2e/needs.png. It never touches the owner's hc core.:
//
//	go test -tags e2e -run TestNeedsRowCapture ./cmd/contr
func TestNeedsRowCapture(t *testing.T) {
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
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc is not installed")
	}
	core := hctest.New(t, "contr-e2e", hctest.Session("e2e"))
	dir := core.Dir
	ownerFile := filepath.Join(dir, "owner.json")
	const doc = `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"Which database?","options":["sqlite","redis"],"asked":"2026-11-03T10:00:00Z"},
{"id":"D2","project":"demo","title":"Ship the 0.2 release today?","question":"Ship?","asked":"2026-11-03T10:01:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the bag drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":3,"next_todo":2}`
	if err := os.WriteFile(ownerFile, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

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
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "sleep 600"}}
	if _, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"}); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock, "CONTR_OWNER_FILE="+ownerFile,
		"CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"),
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))...)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(15 * time.Second)
	for !c.labels(t)["needs row"] {
		if time.Now().After(deadline) {
			t.Fatalf("the needs row did not show; labels %v", keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	labels := c.labels(t)
	for _, want := range []string{"D1 Which database for the cache?", "D2 Ship the 0.2 release today?", "T1 Test the bag drag on the Mac"} {
		if !labels[want] {
			t.Errorf("no tab %q; labels %v", want, keys(labels))
		}
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	c.capture(t, filepath.Join(out, "needs.png"))
}
