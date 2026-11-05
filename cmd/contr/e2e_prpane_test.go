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

// TestPRPaneCapture runs the built app against a scratch hc
// core with a worker that has an issue and a worktree, and a fake gh on PATH. It
// clicks the worker's number on the board and captures the PR pane
// (tmp/e2e/pr-pane.png):
//
//	go test -tags e2e -run TestPRPaneCapture ./cmd/contr
func TestPRPaneCapture(t *testing.T) {
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
	wt := filepath.Join(dir, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	// A fake gh that answers pr view with a fixed pull request.
	bindir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bindir, 0o755); err != nil {
		t.Fatal(err)
	}
	ghScript := "#!/bin/sh\ncat <<'EOF'\n" + prE2EJSON + "\nEOF\n"
	if err := os.WriteFile(filepath.Join(bindir, "gh"), []byte(ghScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "echo working; sleep 600"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "b642-fix", Profile: "sleepy", Role: "builder", Project: "demo", Issue: "642", Worktree: wt},
	} {
		s.Parent = ""
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock,
		"PATH="+bindir+string(os.PathListSeparator)+os.Getenv("PATH"),
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
	for !c.labels(t)["pr #642"] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !c.labels(t)["pr #642"] {
		t.Fatalf("no PR button; labels %v", keys(c.labels(t)))
	}
	c.call(t, "act", map[string]any{"id": c.idOf(t, "pr #642"), "action": "press"})
	deadline = time.Now().Add(15 * time.Second)
	for !c.labels(t)["GitHub ↗"] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	if !c.labels(t)["pull request"] || !c.labels(t)["GitHub ↗"] {
		t.Fatalf("the PR pane did not show; labels %v", keys(c.labels(t)))
	}
	c.capture(t, filepath.Join(out, "pr-pane.png"))
}

const prE2EJSON = `{"number":642,"title":"Fix bag drag target","url":"https://example.test/o/client/pull/642",
"state":"OPEN","body":"Drops land in the slot under the cursor instead of the first free slot.\n\nDragging onto another bag moves the item into that bag.\n\nCloses #638.","author":{"login":"b-1140"},
"headRefName":"fix/bag-drag","baseRefName":"main","headRefOid":"abc","additions":86,"deletions":12,"changedFiles":3,
"isDraft":false,"mergeable":"MERGEABLE","mergedAt":null,"closedAt":null,
"comments":[{"author":{"login":"principal"},"body":"Waits for your test at localhost:5173","createdAt":"2026-11-03T11:56:00Z"}],
"reviews":[{"author":{"login":"r-1139"},"body":"","state":"APPROVED","submittedAt":"2026-11-03T11:46:00Z"}],
"commits":[{"messageHeadline":"Drop in the slot under the cursor","committedDate":"2026-11-03T11:30:00Z","authors":[{"login":"b-1140","name":"b"}]}],
"statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"SUCCESS"}]}`
