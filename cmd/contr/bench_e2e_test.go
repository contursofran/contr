//go:build e2e && bench

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/bench"
	"github.com/contursofran/contr/internal/hc"
)

const (
	benchWarmup  = 6 * time.Second
	benchSamples = 15
	benchBusy    = `i=0; while :; do i=$((i+1)); echo "bench line $i abcdefghij 0123456789 -_=+()[]{}<>"; sleep 0.016; done`
	benchFlood   = `i=0; while :; do i=$((i+1)); echo "bench line $i abcdefghij 0123456789 -_=+()[]{}<>"; done`
)

// TestAppBench measures the whole app (frames, CPU and memory) in seven phases
// against the dev herdr and a scratch hc core with three agents: an
// orchestrator, a "flag" worker whose status the test flips between idle and
// working, and a "term" worker with a shell, which the app shows in its
// terminal view. See docs/BENCHMARKS.md, "How to run the app test". It is a
// heavy job: run nothing else at the same time. Files go to tmp/bench/:
// start.png, busy.png, result.md and result.json.
func TestAppBench(t *testing.T) {
	if !strings.Contains(os.Getenv("HERDR_SOCKET_PATH"), "contr-go-dev-herdr") {
		t.Skip("HERDR_SOCKET_PATH is not the dev herdr; run eval \"$(scripts/dev-herdr.sh)\" first")
	}
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc is not installed")
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
	dir, err := os.MkdirTemp("/tmp", "contr-bench")
	if err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	// The scratch core: its own session and XDG folders. The owner's core is
	// never used.
	env := []string{fmt.Sprintf("HC_SESSION=b%d", os.Getpid())}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME"} {
		p := filepath.Join(dir, strings.ToLower(v[4:6]))
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, v+"="+p)
	}
	t.Cleanup(func() {
		cmd := exec.Command("hc", "server", "stop")
		cmd.Env = append(cmd.Environ(), env...)
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop the scratch core: %v: %s", err, msg)
		}
		_ = os.RemoveAll(dir)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	// "flag" reports its status as its program does (OSC 7501): each line it
	// reads is the new status word.
	cfg.Profiles["flag"] = agents.Profile{Argv: []string{"sh", "-c", `while read s; do printf '\033]7501;state=%s\007' "$s"; done`}}
	cfg.Profiles["sleepy"] = agents.Profile{Argv: []string{"sleep", "3600"}}
	cfg.Profiles["sh"] = agents.Profile{Argv: []string{"sh"}}
	mgr := agents.New(agents.HC{Client: cl}, cfg)
	spawned := map[string]agents.Worker{}
	for _, s := range []agents.Spec{
		{Name: "lead", Profile: "sleepy", Role: agents.RoleOrchestrator, Project: "bench", Worktree: proj},
		{Name: "flag", Profile: "flag", Role: "builder", Project: "bench", Issue: "1", Worktree: proj},
		{Name: "term", Profile: "sh", Role: "builder", Project: "bench", Issue: "2", Worktree: proj},
	} {
		w, err := mgr.Spawn(ctx, s)
		if err != nil {
			t.Fatalf("spawn %s: %v", s.Name, err)
		}
		spawned[s.Name] = w
	}
	// setWorking flips the status of the "flag" worker and gives the app time
	// to take the event. The worker starts with no status, so the first call
	// always sends.
	var working *bool
	setWorking := func(w bool) {
		t.Helper()
		if working != nil && *working == w {
			return
		}
		word := "idle"
		if w {
			word = "working"
		}
		if err := mgr.Send(ctx, "flag", word); err != nil {
			t.Fatal(err)
		}
		working = &w
		time.Sleep(time.Second)
	}
	setWorking(false)

	sock := filepath.Join(dir, "a.sock")
	app := exec.Command(bin)
	app.Env = append(os.Environ(), append(env, "MYGO_AUTOMATION="+sock,
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"),
		"MYGO_FRAME_STATS=all")...) // the frame log gives the frames of each phase
	// The app's log, with MyGo's frame log when MYGO_FRAME_STATS is set.
	appLog, err := os.Create(filepath.Join(out, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer appLog.Close()
	app.Stdout, app.Stderr = appLog, appLog
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	// Select the "term" worker: the terminal view follows the selected agent.
	row := ""
	for deadline := time.Now().Add(30 * time.Second); row == "" && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}}) // draws a frame
		row = c.labelWithPrefix(t, "term, ")
	}
	if row == "" {
		t.Fatalf("no row for the term worker; labels %v", keys(c.labels(t)))
	}
	rb := c.boundsOf(t, row)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": rb.X + rb.Width/2, "y": rb.Y + rb.Height/2}}})
	for deadline := time.Now().Add(30 * time.Second); !c.labels(t)["terminal grid"]; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("grid did not show; labels %v", keys(c.labels(t)))
		}
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
	type timing struct {
		seconds, cpuSeconds float64
		working             bool
	}
	timings := map[string]timing{}
	loads := map[string]string{}
	var order []string
	table = append(table, bench.Header())
	t.Log("\n" + bench.Header())
	// phase runs one phase. isWorking tells whether a working item is on
	// screen (the phase sets the "flag" worker to that before it starts).
	hidden := false // set once the window is hidden on purpose
	// BENCH_PHASES, when set, names the phases to run. BENCH_PROFILE_PHASE
	// names a phase to profile, with CONTR_PROFILE set (docs/BENCHMARKS.md).
	phase := func(name string, isWorking bool, setup func(), teardown func()) {
		t.Helper()
		if only := os.Getenv("BENCH_PHASES"); only != "" && !strings.Contains(only, name) {
			return
		}
		prof := os.Getenv("BENCH_PROFILE_PHASE") == name
		load := strings.TrimSpace(sysctlLoad(t))
		setWorking(isWorking)
		if setup != nil {
			setup()
		}
		time.Sleep(benchWarmup)
		cpu0, err := bench.ReadCPUTime(app.Process.Pid)
		if err != nil {
			t.Fatalf("phase %q: %v", name, err)
		}
		t0 := time.Now()
		fmt.Fprintf(appLog, "%s\n", bench.StartMark(name))
		if prof { // start a CPU profile (cmd/contr/profile_e2e.go)
			if err := app.Process.Signal(syscall.SIGUSR1); err != nil {
				t.Logf("start the profile: %v", err)
			}
		}
		var samples []bench.Sample
		for i := 0; i < benchSamples; i++ {
			s, err := bench.Read(app.Process.Pid)
			if err != nil {
				t.Fatalf("phase %q: %v", name, err)
			}
			samples = append(samples, s)
			time.Sleep(time.Second)
		}
		fmt.Fprintf(appLog, "%s\n", bench.EndMark(name))
		secs := time.Since(t0).Seconds()
		if !hidden {
			var wins struct{ Windows []struct{ Visible bool } }
			if err := json.Unmarshal(c.call(t, "windows", map[string]any{}), &wins); err == nil && len(wins.Windows) > 0 && !wins.Windows[0].Visible {
				t.Errorf("phase %q: the window is not visible, so its numbers are not valid (another window may cover it)", name)
			}
		}
		cpu1, err := bench.ReadCPUTime(app.Process.Pid)
		if err != nil {
			t.Fatalf("phase %q: %v", name, err)
		}
		if prof { // stop it and write a heap profile, after the CPU time was read
			if err := app.Process.Signal(syscall.SIGUSR2); err != nil {
				t.Logf("stop the profile: %v", err)
			}
			time.Sleep(2 * time.Second)
		}
		timings[name] = timing{seconds: secs, cpuSeconds: cpu1 - cpu0, working: isWorking}
		order = append(order, name)
		if teardown != nil {
			teardown()
		}
		st := bench.Summarize(samples)
		stats[name] = st
		t.Logf("phase %q footprint first/last: %d / %d MB", name,
			samples[0].FootprintKB/1024, samples[len(samples)-1].FootprintKB/1024)
		// The frames come from the app log after the run (see below), so
		// the row is built then; here the load is kept.
		loads[name] = load
	}
	stopLoop := func() { c.key(t, "Ctrl+C"); time.Sleep(500 * time.Millisecond) }
	startLoop := func(cmd string) func() { return func() { c.typeLine(t, cmd) } }

	phase("idle shown", false, nil, nil)
	idleShown := stats["idle shown"].CPUMed
	phase("idle shown, one item working", true, nil, nil)
	phase("busy pane shown", false, startLoop(benchBusy), func() {
		c.capture(t, filepath.Join(out, "busy.png"))
		stopLoop()
	})
	phase("flood pane shown", false, startLoop(benchFlood), stopLoop)

	// Agent updates: a ticker changes a label of the "flag" worker through
	// hc, once a second. Each change is an hc event, so the model builds a new
	// snapshot and notifies the window. The ticker runs `hc label` itself
	// because t.Fatal must not run on another goroutine.
	done := make(chan struct{})
	var wg sync.WaitGroup
	var updErr error
	phase("agent updates shown", false, func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for n := 3; ; n++ {
				select {
				case <-done:
					return
				case <-tick.C:
				}
				cmd := exec.Command("hc", "label", spawned["flag"].Address, "issue="+strconv.Itoa(n))
				cmd.Env = append(cmd.Environ(), env...)
				if msg, err := cmd.CombinedOutput(); err != nil {
					updErr = fmt.Errorf("hc label: %v: %s", err, msg)
					return
				}
			}
		}()
	}, func() { close(done); wg.Wait() })
	if updErr != nil {
		t.Errorf("agent updater: %v", updErr)
	}

	hide := func() {
		hidden = true
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Hide contr"}})
		time.Sleep(2 * time.Second)
	}
	phase("idle hidden", false, hide, nil)
	hiddenIdle := stats["idle hidden"].CPUMed
	if hiddenIdle > idleShown {
		t.Logf("WARNING: idle hidden CPU median %.1f %% is above idle shown %.1f %%", hiddenIdle, idleShown)
	}
	// The window is hidden, so the input has no visible proof. The loop also
	// writes its counter to a file. If the counter did not grow, the loop did
	// not run and the reading is not a valid result (not a target miss).
	counter := filepath.Join(dir, "n")
	busyHidden := strings.Replace(benchBusy, `; sleep`, `; echo $i > '`+counter+`'; sleep`, 1)
	// hc starts and stops the loop: automation input does not reach a
	// hidden window.
	phase("busy pane hidden", false, func() {
		if err := mgr.Send(ctx, "term", busyHidden); err != nil {
			t.Fatal(err)
		}
	}, func() {
		n := readCounter(counter)
		if _, err := mgr.Interrupt(ctx, "term"); err != nil {
			t.Errorf("stop the loop: %v", err)
		}
		t.Logf("busy loop counter while hidden: %d lines", n)
		if min := benchSamples * 20; n < min {
			t.Errorf("busy loop did not run while hidden: counter %d, want at least %d", n, min)
		}
	})

	// Frames per phase, from the app's frame log between the phase marks.
	logData, err := os.ReadFile(filepath.Join(out, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	frames := bench.CountFrames(string(logData))
	result := map[string]bench.Phase{}
	for _, name := range order {
		tm := timings[name]
		p := bench.NewPhase(frames[name], tm.seconds, tm.cpuSeconds, stats[name], tm.working)
		if p.Frames == 0 && name != "idle hidden" && name != "busy pane hidden" {
			t.Logf("WARNING: phase %q: no frames in the app log (is MYGO_FRAME_STATS set?)", name)
		}
		result[name] = p
		row := bench.Row(name, p, stats[name], loads[name])
		t.Log(row)
		table = append(table, row)
	}
	js, err := bench.MarshalResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "result.json"), js, 0o644); err != nil {
		t.Fatal(err)
	}

	// A miss is reported, never a failure (owner answer to Q6).
	res := strings.Join(table, "\n") + "\n\nTargets (a miss is reported only):\n"
	for _, tg := range benchTargets {
		st, ran := stats[tg.phase]
		if !ran { // BENCH_PHASES left it out
			continue
		}
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
	{"agent updates shown", 2, 0},
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
