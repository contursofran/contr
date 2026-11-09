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
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestFileTreePanel runs the built app against a scratch hc core and a scratch git repo under
// /tmp with a changed, a new, a deleted and an ignored file. It selects the repo's worktree, checks the rows
// and their counts, turns the filter on, and captures the window in Dark and Light. HOME is a
// scratch folder. PNGs go to tmp/e2e/filetree-*.png. It never touches the owner's core:
//
//	go test -tags e2e -run TestFileTreePanel ./cmd/contr
func TestFileTreePanel(t *testing.T) {
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

	repo, err := os.MkdirTemp("/tmp", "contr-ft-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo) })
	repo, _ = filepath.EvalSymlinks(repo)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, msg)
		}
	}
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	write(".gitignore", "*.log\nbuild/\n")
	write("main.go", "package main\n\nfunc main() {}\n")
	write("README.md", "# scratch\n")
	write("old.go", "package main\n\nvar a = 1\nvar b = 2\n")
	write("internal/util/util.go", "package util\n")
	write("internal/util/keep.go", "package util\n")
	git("add", "-A")
	git("commit", "-m", "base")
	git("checkout", "-b", "feature")
	write("main.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n") // +5 −1 … counted below
	if err := os.Remove(filepath.Join(repo, "old.go")); err != nil {
		t.Fatal(err)
	}
	write("internal/util/new.go", "package util\n\nfunc New() {}\n") // new, +3
	write("debug.log", "ignored\n")
	write("build/out.bin", "ignored")

	core := hctest.New(t, "contr-ft", hctest.Session("ft"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(dir, "xdg-config"), "XDG_STATE_HOME="+filepath.Join(dir, "xdg-state"),
		"SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	mgr := agents.New(agents.HC{Client: cl}, agents.DefaultConfig())
	if _, err := mgr.OpenTerminal(ctx, agents.TerminalSpec{Name: "ft-term", Project: "scratch", Dir: repo}); err != nil {
		t.Fatal(err)
	}

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	wake := func() {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
	}
	waitLabel := func(label string) {
		t.Helper()
		for deadline := time.Now().Add(25 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
			wake()
			if c.labels(t)[label] {
				return
			}
		}
		t.Fatalf("no %q; labels %v", label, keys(c.labels(t)))
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}

	waitLabel(app.LabelFileTree)
	// Select the worktree row of the repo in the sidebar.
	var row string
	for deadline := time.Now().Add(25 * time.Second); row == "" && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		wake()
		for l := range c.labels(t) {
			if strings.HasPrefix(l, "feature, ") || strings.HasPrefix(l, filepath.Base(repo)+", ") {
				row = l
			}
		}
	}
	if row == "" {
		t.Fatalf("no worktree row; labels %v", keys(c.labels(t)))
	}
	click(row)
	rows := []string{"main.go, +5 −1", "old.go, −4", "internal", ".gitignore", "README.md"}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		wake()
		if c.labels(t)["main.go, +5 −1"] {
			break
		}
	}
	labels := c.labels(t)
	for _, want := range rows {
		if !labels[want] {
			t.Errorf("no row %q; labels %v", want, keys(labels))
		}
	}
	for _, no := range []string{"debug.log", "build", ".git"} {
		if labels[no] {
			t.Errorf("ignored %q shows", no)
		}
	}
	click("internal")
	waitLabel("util")
	click("util")
	waitLabel("new.go, +3")
	waitLabel("keep.go")

	for _, theme := range []string{"Dark", "Light"} {
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		waitLabel(settings.LabelSettings)
		click(theme)
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		time.Sleep(700 * time.Millisecond)
		wake()
		c.capture(t, filepath.Join(out, "filetree-"+strings.ToLower(theme)+".png"))
	}
	click(app.LabelFileFilter)
	time.Sleep(600 * time.Millisecond)
	labels = c.labels(t)
	if labels["README.md"] || labels["keep.go"] || !labels["new.go, +3"] || !labels["old.go, −4"] {
		t.Errorf("filter on shows %v", keys(labels))
	}
	c.capture(t, filepath.Join(out, "filetree-light-filter.png"))
}
