//go:build e2e

package main

import (
	"context"
	"github.com/contursofran/contr/internal/hc/hctest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
)

// launchdPath is the PATH of an app that Finder or the Dock opens.
const launchdPath = "/usr/bin:/bin:/usr/sbin:/sbin"

// realApp is the bundle, started the way the owner starts it, against a
// scratch hc core.
type realApp struct {
	root string
	out  string // tmp/e2e, for the PNGs
	dir  string // the scratch folder
	env  []string
	cl   *hc.Client
	ctx  context.Context
	exe  string // the bundle's binary
	sock string
	proc *exec.Cmd
	c    *client
}

// startReal builds the bundle with scripts/bundle.sh (the binary has the e2e
// automation plugin), starts a scratch hc core, and starts the app with the
// launchd PATH and a scratch home. hc is reachable only through
// <home>/.local/bin, as on the owner's Mac. It starts no workers; the caller
// spawns them with r.cl. The app never sees the owner's hc core.
func startReal(t *testing.T) *realApp {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the bundle is for macOS")
	}
	hcPath, err := exec.LookPath("hc")
	if err != nil {
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
	// A short path: macOS limits socket paths to 103 bytes.
	core := hctest.New(t, "contr-e2e", hctest.Session("e2e"))
	dir := core.Dir

	bin := filepath.Join(dir, "contr-e2e")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=darwin", "GOARCH=arm64")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
	bundle := exec.Command("scripts/bundle.sh", "--bin", bin, "--out", filepath.Join(dir, "bundle"))
	bundle.Dir = root
	msg, err := bundle.Output()
	if err != nil {
		t.Fatalf("bundle.sh: %v\n%s", err, msg)
	}
	app := strings.TrimSpace(string(msg))
	exe := filepath.Join(app, "Contents", "MacOS", "contr")

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hcPath, filepath.Join(home, ".local", "bin", "hc")); err != nil {
		t.Fatal(err)
	}

	// The scratch core: its own session and XDG folders.
	env := append(core.Env, "HOME="+home)
	core.Env = env

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	cl, err := hc.Dial(ctx, hc.Command(hcPath, env))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return &realApp{root: root, out: out, dir: dir, env: env, cl: cl, ctx: ctx, exe: exe}
}

// launch starts the bundle with the launchd PATH and returns its automation
// client once the window is open.
func (r *realApp) launch(t *testing.T) *client {
	t.Helper()
	r.sock = filepath.Join(r.dir, "a.sock")
	r.proc = exec.Command(r.exe)
	r.proc.Env = append([]string{"PATH=" + launchdPath}, append(r.env,
		"MYGO_AUTOMATION="+r.sock, "CONTR_STATE_DIR="+filepath.Join(r.dir, "state"),
		"CODEX_HOME="+filepath.Join(r.dir, "codex"), "CONTR_OWNER_FILE="+filepath.Join(r.dir, "owner.json"),
		"CONTR_AGENTS_CONFIG="+filepath.Join(r.dir, "agents.json"))...)
	if err := r.proc.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.proc.Process.Kill(); _ = r.proc.Wait() })
	r.c = dial(t, r.sock)
	t.Cleanup(func() { _ = r.c.conn.Close() })
	r.c.waitWindow(t)
	return r.c
}

// waitLabel waits until an element label has the prefix and returns it.
func (c *client) waitLabel(t *testing.T, prefix string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		for l := range c.labels(t) {
			if strings.HasPrefix(l, prefix) {
				return l
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no element with prefix %q; labels %v", prefix, keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestBundleLaunchdPath is #211 as the owner meets it: the bundle opened
// with the launchd PATH finds hc in ~/.local/bin and shows a spawned agent.
//
//	go test -tags e2e -run TestBundleLaunchdPath ./cmd/contr
func TestBundleLaunchdPath(t *testing.T) {
	r := startReal(t)
	cfg := agents.DefaultConfig()
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "sleep 600"}}
	mgr := agents.New(agents.HC{Client: r.cl}, cfg)
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "w211", Profile: "sleepy", Role: "builder", Project: "demo", Issue: "211"},
	} {
		if _, err := mgr.Spawn(r.ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}
	c := r.launch(t)
	c.waitLabel(t, "w211, ")
	c.call(t, "idle", map[string]any{"timeout": 10000})
	labels := c.labels(t)
	for _, want := range []string{labelOrch, labelAgents} {
		if !labels[want] {
			t.Errorf("no element %q; labels %v", want, keys(labels))
		}
	}
	c.capture(t, filepath.Join(r.out, "bundle-launchd-path.png"))
}

// fakeClaudeSource acts like Claude Code in a terminal: it sets the terminal
// title to its version, as Claude Code does, says it is working, and records
// its argv next to itself.
const fakeClaudeSource = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	exe, _ := os.Executable()
	_ = os.WriteFile(filepath.Join(filepath.Dir(exe), "argv.txt"), []byte(strings.Join(os.Args[1:], "\n")), 0o644)
	fmt.Print("\x1b]0;2.1.317\x07")
	fmt.Print("\x1b]7501;state=working\x07")
	time.Sleep(time.Hour)
}
`

// TestFakeClaudeWorker is #212: a worker that starts like a kit worker
// (`--agent contr:builder`) and titles its terminal like Claude Code. The
// sidebar, the board and the tabs must show the name, role and state from
// the hc labels, not Claude's version.
//
//	go test -tags e2e -run TestFakeClaudeWorker ./cmd/contr
func TestFakeClaudeWorker(t *testing.T) {
	r := startReal(t)
	src := filepath.Join(r.dir, "fake")
	bindir := filepath.Join(r.dir, "fakebin")
	for _, d := range []string{src, bindir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{"main.go": fakeClaudeSource, "go.mod": "module fake\n\ngo 1.27.3\n"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The program is named claude, so the manager adds the kit's --agent.
	prog := filepath.Join(bindir, "claude")
	mk := exec.Command("go", "build", "-o", prog, ".")
	mk.Dir = src
	mk.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := mk.CombinedOutput(); err != nil {
		t.Fatalf("build the fake claude: %v\n%s", err, msg)
	}
	cfg := agents.DefaultConfig()
	cfg.Kit = filepath.Join(r.root, "kit")
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sh", "-c", "sleep 600"}}
	cfg.Profiles["builder"] = agents.Profile{Argv: []string{prog}}
	mgr := agents.New(agents.HC{Client: r.cl}, cfg)
	wt := filepath.Join(r.dir, "wt-b212")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "demo"},
		{Name: "b212", Profile: "builder", Project: "demo", Issue: "212", Worktree: wt},
	} {
		if _, err := mgr.Spawn(r.ctx, s); err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
	}

	// The worker starts like a real kit worker.
	var argv []byte
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if argv, _ = os.ReadFile(filepath.Join(bindir, "argv.txt")); len(argv) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if want := "--plugin-dir\n" + cfg.Kit + "\n--agent\ncontr:builder"; string(argv) != want {
		t.Fatalf("worker argv %q, want %q", argv, want)
	}

	c := r.launch(t)
	row := c.waitLabel(t, "b212, ")
	// The working icon animates, so the window never goes idle.
	time.Sleep(time.Second)
	c.capture(t, filepath.Join(r.out, "fake-claude-worker.png"))

	t.Run("sidebar", func(t *testing.T) {
		if strings.Contains(row, "2.1.317") {
			t.Errorf("sidebar row %q shows Claude's version", row)
		}
		for _, want := range []string{"b212", "working", "builder"} {
			if !strings.Contains(row, want) {
				t.Errorf("sidebar row %q lacks %q", row, want)
			}
		}
	})
	t.Run("board", func(t *testing.T) {
		work := c.waitLabel(t, "#212 ")
		if strings.Contains(work, "2.1.317") || strings.Contains(work, "contr:builder") {
			t.Errorf("board row %q shows the version or the agent id", work)
		}
		for _, want := range []string{"b212", "builder working"} {
			if !strings.Contains(work, want) {
				t.Errorf("board row %q lacks %q", work, want)
			}
		}
	})
	// The tab already shows the labels (issue and worktree); keep it so.
	t.Run("tabs", func(t *testing.T) {
		tab := c.waitLabel(t, "212 · wt-b212")
		if strings.Contains(tab, "2.1.317") || !strings.HasSuffix(tab, ", working") {
			t.Errorf("tab %q: want the issue and worktree and the working state", tab)
		}
	})
}
