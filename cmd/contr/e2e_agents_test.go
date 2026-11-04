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

// TestAgentsSidebarCapture runs the built app against a
// scratch hc core with one orchestrator and two workers, and saves the window
// as tmp/e2e/agents-sidebar.png:
//
//	go test -tags e2e -run TestAgentsSidebarCapture ./cmd/contr
//
// It never touches the owner's hc core.
func TestAgentsSidebarCapture(t *testing.T) {
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
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "contr"},
		{Name: "b162-s3-sidebar-long-name-clips", Profile: "sleepy", Role: "builder", Project: "contr", Issue: "162"},
		{Name: "r162", Profile: "sleepy", Role: "reviewer", Project: "contr", Issue: "162"},
	} {
		s.Parent = ""
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(15 * time.Second)
	for !c.labels(t)[labelOrch] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	labels := c.labels(t)
	for _, want := range []string{labelOrch, labelAgents} {
		if !labels[want] {
			t.Errorf("no element %q; labels %v", want, keys(labels))
		}
	}
	c.capture(t, filepath.Join(out, "agents-sidebar.png"))
}

const (
	labelOrch   = "orchestrators header"
	labelAgents = "agents header"
)
