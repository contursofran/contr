//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestFileTreeHeadCapture runs the built app against a scratch hc core, a scratch HOME and a scratch git
// repository named contr. It checks the files panel head ("Files", D197), opens a folder and
// captures the window in Dark and Light: tmp/e2e/filetree-head-<theme>.png. It never touches the
// owner's core:
//
//	go test -tags e2e -run TestFileTreeHeadCapture ./cmd/contr
func TestFileTreeHeadCapture(t *testing.T) {
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

	core := hctest.New(t, "contr-ft", hctest.Session("ft"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "contr")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, text string) {
		p := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	titlebarGit(t, repo, "init", "-b", "main")
	for _, f := range []string{"cmd/main.go", "docs/README.md", "internal/agents/agents.go", "internal/app/board.go",
		"internal/app/frame.go", "internal/app/panel.go", "internal/hc/client.go", "kit/agent.md", "AGENTS.md", "go.mod", "NOTICE"} {
		put(f, lines("// "+f, 6))
	}
	titlebarGit(t, repo, "add", "-A")
	titlebarGit(t, repo, "commit", "-m", "start")
	put("internal/app/panel.go", lines("// internal/app/panel.go", 9))
	put("internal/app/tabs.go", lines("// tabs", 4))

	src := filepath.Join(dir, "fake")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"main.go": fakeClaude, "go.mod": "module fake\n\ngo 1.27.3\n"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "claude")
	mk := exec.Command("go", "build", "-o", script, ".")
	mk.Dir = src
	mk.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("build the fake claude: %v\n%s", err, msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["fake-idle"] = agents.Profile{Argv: []string{script, "idle"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	if _, err := mgr.Spawn(ctx, agents.Spec{Name: "lead", Profile: "fake-idle", Role: agents.RoleOrchestrator, Project: "contr", Worktree: repo}); err != nil {
		t.Fatal(err)
	}

	for _, theme := range []string{"dark", "light"} {
		settings := filepath.Join(home, "Library", "Application Support", "contr")
		if err := os.MkdirAll(settings, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"version": 1, "appearance": "`+theme+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(dir, theme[:1]+".sock")
		appCmd := exec.Command(bin)
		appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
		if err := appCmd.Start(); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()
			c := dial(t, sock)
			defer c.conn.Close()
			c.waitWindow(t)
			var labels map[string]bool
			for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
				c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
				if labels = c.labels(t); labels["panel.go"] {
					break
				}
			}
			if !labels["panel.go"] && !labels["internal"] {
				t.Fatalf("%s: the tree never showed; labels %v", theme, keys(labels))
			}
			// D197: the head is "Files"; it was the project name.
			if !labels["Files name"] || labels["contr name"] {
				t.Errorf("%s: the head does not show \"Files\"; labels %v", theme, keys(labels))
			}
			// Open internal, then app.
			for _, folder := range []string{"internal", "app"} {
				b := c.boundsOf(t, folder)
				c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + 60, "y": b.Y + b.Height/2}}})
				time.Sleep(700 * time.Millisecond)
				c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
				time.Sleep(300 * time.Millisecond)
			}
			if !c.labels(t)["frame.go"] {
				t.Errorf("%s: the folders did not open; labels %v", theme, keys(c.labels(t)))
			}
			for l := range c.labels(t) {
				if strings.Contains(l, "chevron") {
					t.Errorf("%s: a chevron shows: %q", theme, l)
				}
			}
			c.capture(t, filepath.Join(out, "filetree-head-"+theme+".png"))
		}()
	}
}
