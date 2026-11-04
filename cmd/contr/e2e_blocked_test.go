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

// TestBlockedCardCapture runs the built app against a
// scratch hc core with a worker that sits at a Claude-shaped permission menu.
// It opens the answer card on the blocked tab and saves tmp/e2e/blocked-card.png.
//
//	go test -tags e2e -run TestBlockedCardCapture ./cmd/contr
//
// It never touches the owner's hc core.
func TestBlockedCardCapture(t *testing.T) {
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
	// A fake "claude": a program with that name (hc names a script after its
	// shell). It prints a permission menu, then says it is blocked.
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
	fmt.Print("────────────────────────\n Bash command\n   rm -rf build\n\n Do you want to proceed?\n")
	fmt.Print(" > 1. Yes\n   2. Yes, and do not ask again\n   3. No, and tell Claude what to do differently\n")
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
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "sleep 600"}}
	cfg.Profiles["fakeclaude"] = agents.Profile{Argv: []string{script}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "b148-s3-card", Profile: "fakeclaude", Role: "builder", Project: "demo", Issue: "148"},
	} {
		if _, err := mgr.Spawn(ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	ownerFile := filepath.Join(dir, "owner.json")
	if err := os.WriteFile(ownerFile, []byte(`{"next_question":1,"next_todo":1}`), 0o600); err != nil {
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
	tab := ""
	deadline := time.Now().Add(20 * time.Second)
	for tab == "" && time.Now().Before(deadline) {
		for l := range c.labels(t) {
			if strings.HasPrefix(l, "b148-s3-card ") {
				tab = l
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if tab == "" {
		t.Fatalf("the blocked tab did not show; labels %v", keys(c.labels(t)))
	}
	press := func(id string) { c.call(t, "act", map[string]any{"id": id, "action": "press"}) }
	press(c.idOf(t, tab))
	deadline = time.Now().Add(10 * time.Second)
	for !c.labels(t)["Yes"] && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	c.call(t, "idle", map[string]any{"timeout": 10000})
	if !c.labels(t)["answer card"] || !c.labels(t)["Yes"] {
		t.Fatalf("the card did not show the choices; labels %v", keys(c.labels(t)))
	}
	c.capture(t, filepath.Join(out, "blocked-card.png"))
}
