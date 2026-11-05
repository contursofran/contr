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
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestQuestionsCard runs the built app against a scratch hc core with an
// orchestrator, a worker that waits for CI and a worker at a Claude permission
// menu, and a scratch owner file with two questions (the first with impacts,
// refs and a source) and one to-do for "demo". It opens the card from the
// notch, shows each kind of item, opens Details, switches the theme and
// answers a question. PNGs go to tmp/e2e/questions-*.png:
//
//	go test -tags e2e -run TestQuestionsCard ./cmd/contr
//
// HOME and every state folder are scratch. It never touches the owner's hc core
// or owner file.
func TestQuestionsCard(t *testing.T) {
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
	core := hctest.New(t, "contr-qc", hctest.Session("qc"))
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

	// A program named "claude" that prints a permission menu as Claude Code
	// does, then says it is blocked.
	src := filepath.Join(dir, "fake")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	const prog = `package main

import (
	"fmt"
	"time"
)

func main() {
	time.Sleep(time.Second)
	fmt.Print("\n● Bash(rm -rf build/ dist/)\n\n")
	fmt.Print("────────────────────────────────────────\n Bash command\n\n   rm -rf build/ dist/\n   Remove the old build output\n\n Do you want to proceed?\n")
	fmt.Print(" ❯ 1. Yes\n   2. Yes, and don't ask again for rm commands in this folder\n   3. No, and tell Claude what to do differently (esc)\n")
	fmt.Print("\x1b]7501;state=blocked:kind=permission:msg=Allow%20rm\x07")
	time.Sleep(time.Hour)
}
`
	script := filepath.Join(dir, "claude")
	for name, content := range map[string]string{"main.go": prog, "go.mod": "module fake\n\ngo 1.27.3\n"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk := exec.Command("go", "build", "-o", script, ".")
	mk.Dir = src
	mk.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("build the fake claude: %v\n%s", err, msg)
	}
	// What a Claude Code worker shows while it waits for CI.
	waiting := `printf '\n\342\227\217 Bash(gh pr checks 250)\n  \342\216\277  build   pass   1m12s\n  \342\216\277  vet     pass   8s\n  \342\216\277  e2e     pending\n\n'; ` +
		`printf '\342\227\217 Waiting for the e2e check before I report the PR.\n\n\342\225\255\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\256\n\342\224\202 > _          \342\224\202\n\342\225\260\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\225\257\n'; exec sleep 600`
	cfg := agents.DefaultConfig()
	cfg.Profiles["claude-like"] = agents.Profile{Argv: []string{"sh", "-c", waiting}}
	cfg.Profiles["fakeclaude"] = agents.Profile{Argv: []string{script}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "claude-like", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "b-250-checks", Profile: "claude-like", Role: "builder", Project: "demo", Issue: "250"},
		{Name: "b-252-cleanup", Profile: "fakeclaude", Role: "builder", Project: "demo", Issue: "252"},
	} {
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	ownerFile := filepath.Join(dir, "owner.json")
	const doc = `{"questions":[
{"id":"D1","project":"demo","from":"b-250-checks","title":"b-250 checks: merge now?","question":"CI for #250 is green except e2e. Merge now or wait for e2e?","options":["merge now","wait for e2e"],"recommended":1,"impacts":["Ships today; if e2e fails main is red until a revert.","Costs about 12 minutes; main stays green."],"refs":["PR 250","#247"],"source":"\"Merge only when every check is green.\" - AGENTS.md, Workflow","asked":"2026-11-05T10:00:00Z"},
{"id":"D2","project":"demo","from":"b-251-retry","title":"b-251 retry: keep the old name?","question":"Keep the old name for the retry flag?","options":["keep","rename"],"asked":"2026-11-05T10:01:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","from":"lead","text":"Test the notch on the Mac","pinned":"2026-11-05T10:03:00Z"}],
"next_question":3,"next_todo":2}`
	if err := os.WriteFile(ownerFile, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

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
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 700}}})
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
				time.Sleep(400 * time.Millisecond)
				return
			}
		}
		t.Fatalf("timeout: %s; labels %v", what, keys(c.labels(t)))
	}
	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		waitFor("settings", func() bool { return has(app.LabelSettings) })
		click(appearance)
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		waitFor("settings closed", func() bool { return !has(app.LabelSettings) })
		time.Sleep(600 * time.Millisecond)
	}
	row := ""
	waitFor("the orchestrator row", func() bool { row = c.labelWithPrefix(t, "lead, "); return row != "" })
	click(row)
	time.Sleep(1500 * time.Millisecond) // the terminal attaches
	capture := func(name string) {
		t.Helper()
		c.call(t, "idle", map[string]any{"timeout": 10000})
		c.capture(t, filepath.Join(out, "questions-"+name+".png"))
	}
	openCard := func() {
		t.Helper()
		waitFor("the notch", func() bool { return has("4 items. Open") })
		click("4 items. Open")
		waitFor("the card", func() bool { return has(app.LabelQuestionsChips) })
	}

	// The card opens on the first item: the blocked menu.
	pick("Dark")
	openCard()
	waitFor("the menu", func() bool { return has("Yes") })
	capture("dark-blocked")
	click("b-250 checks: merge now?")
	waitFor("the question", func() bool { return has(app.LabelQuestionsField) })
	capture("dark-question")
	click(app.LabelQuestionsDetails)
	time.Sleep(600 * time.Millisecond)
	capture("dark-details")
	// Tab moves to the next chip: the first question, the second, the to-do.
	c.key(t, "Tab")
	time.Sleep(400 * time.Millisecond)
	capture("dark-second-question")
	c.key(t, "Tab")
	time.Sleep(400 * time.Millisecond)
	capture("dark-todo")
	c.key(t, "Escape")
	time.Sleep(400 * time.Millisecond)

	pick("Light")
	openCard()
	capture("light-blocked")
	click("b-250 checks: merge now?")
	waitFor("the question", func() bool { return has(app.LabelQuestionsField) })
	capture("light-question")
	click(app.LabelQuestionsDetails)
	time.Sleep(600 * time.Millisecond)
	capture("light-details")

	// Answer the second question: the answer goes to the owner file, and the
	// card moves on.
	click("b-251 retry: keep the old name?")
	waitFor("the second question", func() bool { return has("keep") })
	click("keep")
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(ownerFile)
		if strings.Contains(string(b), `"answer": "keep"`) || strings.Contains(string(b), `"answer":"keep"`) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b, _ := os.ReadFile(ownerFile); !strings.Contains(string(b), "keep") || !strings.Contains(string(b), "answered_by") {
		t.Fatalf("the answer did not reach the owner file:\n%s", b)
	}
	waitFor("one question less", func() bool { return !has("b-251 retry: keep the old name?") })
	capture("light-after-answer")
}
