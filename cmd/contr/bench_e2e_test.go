//go:build e2e && bench

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/bench"
)

const (
	benchWarmup  = 6 * time.Second
	benchSamples = 15
	benchBusy    = `i=0; while :; do i=$((i+1)); echo "bench line $i abcdefghij 0123456789 -_=+()[]{}<>"; sleep 0.016; done`
	benchFlood   = `i=0; while :; do i=$((i+1)); echo "bench line $i abcdefghij 0123456789 -_=+()[]{}<>"; done`
)

// TestAppBench measures the whole app (CPU and memory) in six phases against
// the dev herdr. See docs/BENCHMARKS.md, "How to run the app test". It is a
// heavy job: run nothing else at the same time. Files go to tmp/bench/:
// start.png, busy.png and result.md.
func TestAppBench(t *testing.T) {
	herdrSock := os.Getenv("HERDR_SOCKET_PATH")
	if !strings.Contains(herdrSock, "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	if os.Getenv("CONTR_SURFACE") != "1" {
		t.Skip("CONTR_SURFACE is not 1; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "tmp", "bench")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(out, "contr-bench")
	build := exec.Command("go", "build", "-tags", "e2e", "-o", bin, "./cmd/contr")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}

	// A short path: macOS limits socket paths to 103 bytes.
	dir, err := os.MkdirTemp("", "contr-bench")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	boardDir := filepath.Join(dir, "board")
	proj := filepath.Join(dir, "proj")
	for _, d := range []string{boardDir, proj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		full := append([]string{"-C", proj, "-c", "user.name=bench", "-c", "user.email=bench@invalid"}, args...)
		if msg, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, msg)
		}
	}
	git("init", "-q")
	git("commit", "-q", "--allow-empty", "-m", "bench")

	// The board: the fixture under session "bench", rewritten once per update.
	const session = "bench"
	fixture, err := os.ReadFile(filepath.Join(root, "internal", "board", "testdata", "board_fixture_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(fixture, &doc); err != nil {
		t.Fatal(err)
	}
	doc["session"] = session
	boardFile := filepath.Join(boardDir, session+".json")
	writeBoard := func(rev int) error {
		doc["bench_rev"] = rev // one field that changes on each update
		data, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		tmp := boardFile + ".tmp"
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, boardFile)
	}
	if err := writeBoard(1); err != nil {
		t.Fatal(err)
	}
	// A fresh workspace in the scratch repo: its shell is live and focused.
	// The dev herdr may hold dead panes from earlier runs.
	var ws struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
		}
	}
	if err := json.Unmarshal(herdrCmd(t, "workspace", "create", "--cwd", proj, "--label", "bench", "--focus"), &ws); err != nil ||
		ws.Result.RootPane.PaneID == "" {
		t.Fatalf("workspace create: %v", err)
	}
	pane := ws.Result.RootPane.PaneID
	defer func() { _ = exec.Command("herdr", "workspace", "close", ws.Result.Workspace.ID).Run() }()
	token := func(rev int) {
		t.Helper()
		herdrCmd(t, "pane", "report-metadata", pane, "--source", "bench",
			"--token", "contr_board=1 "+session+" "+strconv.Itoa(rev))
	}
	token(1)
	defer func() {
		_ = exec.Command("herdr", "pane", "report-metadata", pane, "--source", "bench", "--clear-token", "contr_board").Run()
	}()

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), "MYGO_AUTOMATION="+sock, "CONTR_BOARD_DIR="+boardDir,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"))
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	deadline := time.Now().Add(30 * time.Second)
	for !(c.labels(t)["terminal grid"] && c.labels(t)["board header"]) {
		if time.Now().After(deadline) {
			t.Fatalf("grid and board did not show; labels %v", keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	grid := c.boundsOf(t, "terminal grid")
	t.Logf("terminal grid bounds: %.0f x %.0f points (the pane is smaller than the 200x60 of the grid test)", grid.Width, grid.Height)
	var wins struct{ Windows []json.RawMessage }
	if err := json.Unmarshal(c.call(t, "windows", map[string]any{}), &wins); err == nil && len(wins.Windows) > 0 {
		t.Logf("window: %s", wins.Windows[0])
	}
	c.call(t, "input", map[string]any{"events": []map[string]any{
		{"type": "click", "x": grid.X + grid.Width/2, "y": grid.Y + grid.Height/2}}})
	time.Sleep(time.Second)
	c.typeLine(t, "cd "+proj)
	c.typeLine(t, "clear")
	time.Sleep(time.Second)
	c.capture(t, filepath.Join(out, "start.png"))
	defer func() { c.key(t, "Ctrl+C") }()

	var table []string
	stats := map[string]bench.Stats{}
	table = append(table, bench.Header())
	t.Log("\n" + bench.Header())
	phase := func(name string, setup func(), teardown func()) {
		t.Helper()
		load := strings.TrimSpace(sysctlLoad(t))
		if setup != nil {
			setup()
		}
		time.Sleep(benchWarmup)
		var samples []bench.Sample
		for i := 0; i < benchSamples; i++ {
			s, err := bench.Read(app.Process.Pid)
			if err != nil {
				t.Fatalf("phase %q: %v", name, err)
			}
			samples = append(samples, s)
			time.Sleep(time.Second)
		}
		if teardown != nil {
			teardown()
		}
		st := bench.Summarize(samples)
		stats[name] = st
		t.Logf("phase %q footprint first/last: %d / %d MB", name,
			samples[0].FootprintKB/1024, samples[len(samples)-1].FootprintKB/1024)
		row := bench.Row(name, st, load)
		t.Log(row)
		table = append(table, row)
	}
	stopLoop := func() { c.key(t, "Ctrl+C"); time.Sleep(500 * time.Millisecond) }
	startLoop := func(cmd string) func() { return func() { c.typeLine(t, cmd) } }

	phase("idle shown", nil, nil)
	idleShown := stats["idle shown"].CPUMed
	phase("busy pane shown", startLoop(benchBusy), func() {
		c.capture(t, filepath.Join(out, "busy.png"))
		stopLoop()
	})
	phase("flood pane shown", startLoop(benchFlood), stopLoop)

	// Board updates: a ticker writes the file and bumps the bridge mark.
	// herdrCmd fails the test, so the goroutine runs the command itself.
	done := make(chan struct{})
	var wg sync.WaitGroup
	var updErr error
	phase("board updates shown", func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for rev := 2; ; rev++ {
				select {
				case <-done:
					return
				case <-tick.C:
				}
				if err := writeBoard(rev); err != nil {
					updErr = err
					return
				}
				msg, err := exec.Command("herdr", "pane", "report-metadata", pane, "--source", "bench",
					"--token", "contr_board=1 "+session+" "+strconv.Itoa(rev)).CombinedOutput()
				if err != nil {
					updErr = fmt.Errorf("report-metadata: %v: %s", err, msg)
					return
				}
			}
		}()
	}, func() { close(done); wg.Wait() })
	if updErr != nil {
		t.Errorf("board updater: %v", updErr)
	}

	hide := func() {
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Hide contr"}})
		time.Sleep(2 * time.Second)
	}
	phase("idle hidden", hide, nil)
	hiddenIdle := stats["idle hidden"].CPUMed
	if hiddenIdle > idleShown {
		t.Logf("WARNING: idle hidden CPU median %.1f %% is above idle shown %.1f %%", hiddenIdle, idleShown)
	}
	// The window is hidden, so the input has no visible proof. The loop also
	// writes its counter to a file. If the counter did not grow, the loop did
	// not run and the reading is not a valid result (not a target miss).
	counter := filepath.Join(dir, "n")
	busyHidden := strings.Replace(benchBusy, `; sleep`, `; echo $i > '`+counter+`'; sleep`, 1)
	// herdr starts and stops the loop: automation input does not reach a
	// hidden window.
	phase("busy pane hidden", func() { herdrCmd(t, "pane", "run", pane, busyHidden) }, func() {
		n := readCounter(counter)
		herdrCmd(t, "pane", "send-keys", pane, "C-c")
		t.Logf("busy loop counter while hidden: %d lines", n)
		if min := benchSamples * 20; n < min {
			t.Errorf("busy loop did not run while hidden: counter %d, want at least %d", n, min)
		}
	})

	// A miss is reported, never a failure (owner answer to Q6).
	res := strings.Join(table, "\n") + "\n\nTargets (a miss is reported only):\n"
	for _, tg := range benchTargets {
		st := stats[tg.phase]
		verdict := "met"
		if st.CPUMed > tg.cpuMed {
			verdict = "MISS"
		}
		line := fmt.Sprintf("- %s: CPU median %.1f %% vs target <= %.1f %%: %s", tg.phase, st.CPUMed, tg.cpuMed, verdict)
		if tg.footprintMB > 0 {
			mem := "ok"
			if st.FootprintMaxMB > tg.footprintMB {
				mem = "MISS"
			}
			line += fmt.Sprintf("; footprint max %.0f MB vs %.0f MB: %s", st.FootprintMaxMB, tg.footprintMB, mem)
		}
		t.Log(line)
		res += line + "\n"
	}
	if err := os.WriteFile(filepath.Join(out, "result.md"), []byte(res), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", filepath.Join(out, "result.md"))
}

// benchTargets are from docs/BENCHMARKS.md and the M13 plan. footprintMB 0
// means no memory target. Idle shown has a flag at 150 MB, not a target.
var benchTargets = []struct {
	phase       string
	cpuMed      float64
	footprintMB float64
}{
	{"idle shown", 0.5, 150},
	{"busy pane shown", 24, 230},
	{"flood pane shown", 51, 230},
	{"board updates shown", 2, 0},
	{"idle hidden", 0, 0},
	{"busy pane hidden", 2, 0},
}

func sysctlLoad(t *testing.T) string {
	t.Helper()
	b, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		t.Logf("sysctl vm.loadavg: %v", err)
		return "?"
	}
	return strings.Trim(strings.TrimSpace(string(b)), "{} ")
}

// readCounter reads the busy loop counter file; a missing file counts as 0.
func readCounter(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}
