//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// fakeClaude is a program that looks like Claude Code in a terminal: the welcome box, the
// conversation, a status line and the prompt box. It tells hc its state with the OSC the hook
// sends (last argument: working, idle or blocked:kind=...:msg=..., which also prints a permission menu).
const fakeClaude = `package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	cwd, _ := os.Getwd()
	state := os.Args[len(os.Args)-1] // contr puts --settings before the profile arguments
	fmt.Print("\n╭───────────────────────────────────────────────────╮\n")
	fmt.Print("│ ✻ Welcome to Claude Code!                         │\n")
	fmt.Print("│                                                   │\n")
	fmt.Print("│   /help for help, /status for your current setup  │\n")
	fmt.Print("│                                                   │\n")
	fmt.Printf("│   cwd: %-43s│\n", cwd[max(0, len(cwd)-43):])
	fmt.Print("╰───────────────────────────────────────────────────╯\n\n")
	fmt.Print("> Build the title bar from docs/spec/TitleBar.md\n\n")
	fmt.Print("● I will read the spec first, then the tab model.\n\n")
	fmt.Print("● Read(docs/spec/TitleBar.md)\n  ⎿  Read 74 lines\n\n")
	if state == "working" {
		fmt.Print("✻ Reading the spec… (12s · ↓ 1.4k tokens · esc to interrupt)\n\n")
	}
	if strings.HasPrefix(state, "blocked") {
		fmt.Print("● Bash(rm -rf build/ dist/)\n\n")
		fmt.Print("────────────────────────────────────────\n Bash command\n\n   rm -rf build/ dist/\n   Remove the old build output\n\n Do you want to proceed?\n")
		fmt.Print(" ❯ 1. Yes\n   2. Yes, and don't ask again for rm commands in this folder\n   3. No, and tell Claude what to do differently (esc)\n")
		time.Sleep(time.Second) // hc needs the terminal up before it takes the state
		fmt.Printf("\x1b]7501;state=%s\x07", state)
		time.Sleep(time.Hour)
	}
	fmt.Print("╭───────────────────────────────────────────────────╮\n")
	fmt.Print("│ >                                                 │\n")
	fmt.Print("╰───────────────────────────────────────────────────╯\n")
	fmt.Print("  ? for shortcuts\n")
	fmt.Printf("\x1b]7501;state=%s\x07", state)
	time.Sleep(time.Hour)
}
`

// gitIn runs git in dir with a fixed identity.
func titlebarGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=contr", "-c", "user.email=contr@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, msg)
	}
}

func lines(prefix string, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s %d\n", prefix, i)
	}
	return b.String()
}

// inBar returns the bounds of the element with a label inside the 46px title bar, so a sidebar
// row with the same label does not count.
func (c *client) inBar(t *testing.T, label string) (rect, bool) {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Label  string
			Bounds rect
		}
	}
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	for _, n := range tree.Nodes {
		if n.Label == label && n.Bounds.Y+n.Bounds.Height <= 46 {
			return n.Bounds, true
		}
	}
	return rect{}, false
}

// plainTabWidths returns the width of the selected "$ contr" tab and of the other one.
func (c *client) plainTabWidths(t *testing.T) (selected, other float64, ok bool) {
	t.Helper()
	var tree struct {
		Nodes []struct {
			Label  string
			Role   string
			States []string
			Bounds rect
		}
	}
	if err := json.Unmarshal(c.call(t, "tree", map[string]any{}), &tree); err != nil {
		t.Fatal(err)
	}
	var found int
	for _, n := range tree.Nodes {
		if n.Role != "tab" || n.Label != "$ contr" {
			continue
		}
		found++
		if slices.Contains(n.States, "checked") {
			selected = n.Bounds.Width
		} else {
			other = n.Bounds.Width
		}
	}
	return selected, other, found == 2
}

// TestTitleBarCapture runs the built app against a scratch hc core, a scratch HOME and a scratch
// git repository. The project has a fake Claude Code orchestrator, a fake Claude Code builder in
// a worktree, and two plain terminals. It captures the window in Dark and Light with each agent
// selected and checks the tabs and the diff summary against the file tree. PNGs go to tmp/e2e/titlebar-*.png. It needs `hc` and never
// touches the owner's core:
//
//	go test -tags e2e -run TestTitleBarCapture ./cmd/contr
func TestTitleBarCapture(t *testing.T) {
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

	core := hctest.New(t, "contr-tb", hctest.Session("tb"))
	dir := core.Dir
	home := filepath.Join(dir, "home")

	// The project: a repository on main, and the worktree of the builder on a branch that is
	// ahead of main by one commit (+29 −4 over two files), with an uncommitted edit (+3) and an
	// untracked file (+5): the title bar and the file tree both count +37 −4 over three files.
	repo := filepath.Join(dir, "contr")
	wt := filepath.Join(dir, "contr-wt", "agent-342-titlebar")
	for _, d := range []string{home, repo} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	titlebarGit(t, repo, "init", "-b", "main")
	write := func(base, name, text string) {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(repo, "internal/app/titlebar.go", lines("// v0.2 line", 12))
	titlebarGit(t, repo, "add", "-A")
	titlebarGit(t, repo, "commit", "-m", "Title bar: v0.2")
	titlebarGit(t, repo, "worktree", "add", "-b", "agent/342-titlebar", wt)
	write(wt, "internal/app/titlebar.go", lines("// v0.3 line", 9)+lines("// v0.2 line", 12)[len(lines("// v0.2 line", 4)):])
	write(wt, "internal/app/tabs.go", lines("// tab model line", 20))
	titlebarGit(t, wt, "add", "-A")
	titlebarGit(t, wt, "commit", "-m", "Title bar: tabs of the selected agent")
	write(wt, "internal/app/tabs.go", lines("// tab model line", 23))
	write(wt, "notes.txt", lines("note", 5))

	// The fake Claude Code.
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

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["fake-idle"] = agents.Profile{Argv: []string{script, "idle"}}
	cfg.Profiles["fake-working"] = agents.Profile{Argv: []string{script, "working"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "fake-idle", Role: agents.RoleOrchestrator, Project: "contr", Worktree: repo},
		{Name: "b342-s1-titlebar", Profile: "fake-working", Role: "builder", Project: "contr", Issue: "#342", Worktree: wt},
	} {
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	var sock string
	run := func(theme string) *client {
		t.Helper()
		settings := filepath.Join(home, "Library", "Application Support", "contr")
		if err := os.MkdirAll(settings, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(settings, "settings.json"), []byte(`{"version": 1, "appearance": "`+theme+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		sock = filepath.Join(dir, theme+".sock")
		appCmd := exec.Command(bin)
		appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
			"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
			"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
		if err := appCmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() })
		c := dial(t, sock)
		t.Cleanup(func() { c.conn.Close() })
		c.waitWindow(t)
		return c
	}
	wake := func(c *client) {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
	}
	waitFor := func(c *client, what string, ok func(map[string]bool) bool) {
		t.Helper()
		var labels map[string]bool
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
			wake(c)
			if labels = c.labels(t); ok(labels) {
				return
			}
		}
		t.Fatalf("%s never showed; labels %v", what, keys(labels))
	}
	// clickAt clicks the element with a label anywhere in the window.
	clickAt := func(c *client, label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(700 * time.Millisecond)
	}
	click := func(c *client, label string) {
		t.Helper()
		b, ok := c.inBar(t, label)
		if !ok {
			t.Fatalf("no %q in the title bar", label)
		}
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(700 * time.Millisecond)
	}
	inBar := func(c *client, label string) bool { _, ok := c.inBar(t, label); return ok }
	plains := func() int {
		ws, err := mgr.List(ctx, true)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, w := range ws {
			if w.Plain() {
				n++
			}
		}
		return n
	}

	for i, theme := range []string{"dark", "light"} {
		c := run(theme)
		// The first start shows the orchestrator's tab; "+" opens the two plain terminals.
		waitFor(c, "the orchestrator tab", func(map[string]bool) bool { return inBar(c, "lead") })
		if i == 0 {
			for n := 1; n <= 2; n++ {
				// A click can land while the window builds a frame: click again until the terminal exists.
				for try := 0; try < 5 && plains() < n; try++ {
					waitFor(c, "the new tab button", func(map[string]bool) bool { return inBar(c, titlebar.LabelNewTab) })
					click(c, titlebar.LabelNewTab)
					for deadline := time.Now().Add(4 * time.Second); plains() < n && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
					}
				}
			}
		}
		waitFor(c, "the two plain terminal tabs", func(map[string]bool) bool {
			_, _, two := c.plainTabWidths(t)
			return two && inBar(c, titlebar.LabelNewTab)
		})
		click(c, "lead")
		time.Sleep(time.Second)
		for _, want := range []string{"lead", "$ contr", titlebar.LabelNewTab, titlebar.LabelFilesToggle} {
			if !inBar(c, want) {
				t.Errorf("%s, orchestrator: no %q in the bar; labels %v", theme, want, keys(c.labels(t)))
			}
		}
		for _, absent := range []string{"Split", "#342 · agent-342-titlebar", titlebar.LabelCloseTab} {
			if inBar(c, absent) {
				t.Errorf("%s, orchestrator selected: the bar has %q", theme, absent)
			}
		}
		c.capture(t, filepath.Join(out, "titlebar-"+theme+"-orchestrator.png"))

		// A plain terminal tab selected: its close button shows, the agent's tab does not have one.
		click(c, "$ contr")
		time.Sleep(time.Second)
		// MyGo exposes a tab as one element, so the close button inside it has no node of its own.
		// The selected plain tab is the 16px button and its 8px gap wider than the other one.
		var sel, other float64
		var two bool
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
			wake(c)
			if sel, other, two = c.plainTabWidths(t); two && sel >= other+20 {
				break
			}
		}
		if !two || sel < other+20 {
			t.Errorf("%s: the selected plain tab has no close button (widths %.0f and %.0f)", theme, sel, other)
		}
		c.capture(t, filepath.Join(out, "titlebar-"+theme+"-terminal.png"))

		// Select the builder in the sidebar: its tab joins the shared set, after the orchestrator's (D190).
		var row string
		for l := range c.labels(t) {
			if strings.HasPrefix(l, "b342-s1-titlebar, working, #342") {
				row = l
			}
		}
		if row == "" {
			t.Fatalf("%s: no sidebar row of the builder; labels %v", theme, keys(c.labels(t)))
		}
		// A press can land while the window still builds a frame: press again until the tab shows.
		for try := 0; try < 5 && !inBar(c, "#342 · agent-342-titlebar"); try++ {
			clickAt(c, row)
			for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline) && !inBar(c, "#342 · agent-342-titlebar"); time.Sleep(200 * time.Millisecond) {
				wake(c)
			}
		}
		waitFor(c, "the builder's tab", func(map[string]bool) bool { return inBar(c, "#342 · agent-342-titlebar") })
		if !inBar(c, "lead") {
			t.Errorf("%s: the orchestrator's tab left the bar with the builder selected (the tab set is shared, D190)", theme)
		}
		if !inBar(c, "$ contr") {
			t.Errorf("%s: the plain terminals left the bar with the builder selected", theme)
		}
		time.Sleep(time.Second)

		// The diff summary counts what the file tree lists: with the changed-only filter on, the
		// rows of the tree add up to the number in the title bar.
		const summary = "37 lines added, 4 removed, 3 files. Open review"
		waitFor(c, "the diff summary", func(map[string]bool) bool { return inBar(c, summary) })
		clickAt(c, files.LabelFileFilter)
		waitFor(c, "the changed files in the tree", func(l map[string]bool) bool { return l["notes.txt, +5"] })
		for _, row := range []string{"tabs.go, +23", "notes.txt, +5", "titlebar.go, +9 −4"} {
			if !c.labels(t)[row] {
				t.Errorf("%s: the tree has no row %q; the title bar shows %q", theme, row, summary)
			}
		}
		if !inBar(c, summary) {
			t.Errorf("%s: no diff summary with the builder selected", theme)
		}
		c.capture(t, filepath.Join(out, "titlebar-"+theme+"-builder.png"))
		clickAt(c, files.LabelFileFilter)

		// A click on the diff summary opens Review; a click on a tab brings the terminal back.
		click(c, summary)
		waitFor(c, "the Review view", func(l map[string]bool) bool { return l[app.LabelReview] })
		time.Sleep(time.Second)
		c.capture(t, filepath.Join(out, "titlebar-"+theme+"-review.png"))
		click(c, "$ contr")
		waitFor(c, "the terminal again", func(l map[string]bool) bool { return !l[app.LabelReview] })
	}
}
