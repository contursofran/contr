//go:build hccore

// This test runs against a real herdr-core, isolated from the owner's core:
// its own HC_SESSION and scratch XDG folders. Run it with
//
//	go test -tags hccore ./internal/agents
package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/hc"
)

func isolatedCore(t *testing.T) []string {
	t.Helper()
	if _, err := exec.LookPath("hc"); err != nil {
		t.Skip("hc is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "hc")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"HC_SESSION=" + fmt.Sprintf("a%d", os.Getpid())}
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME"} {
		p := dir + "/" + strings.ToLower(v[4:6])
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, v+"="+p)
	}
	t.Cleanup(func() {
		cmd := exec.Command("hc", "server", "stop")
		cmd.Env = append(cmd.Environ(), env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("stop the scratch core: %v: %s", err, out)
		}
		_ = os.RemoveAll(dir)
	})
	return env
}

func TestRealCoreSpawnWatchSend(t *testing.T) {
	env := isolatedCore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	cfg := Config{Profiles: map[string]Profile{"echo": {Argv: []string{"sh", "-c", "read x; echo got $x"}}}}
	m := New(HC{cl}, cfg)
	dir := t.TempDir()
	if _, err := m.Spawn(ctx, Spec{Name: "w1", Profile: "echo", Issue: "158", Worktree: dir}); err != nil {
		t.Fatal(err)
	}

	sawLive, sawEnd := false, false
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		// Send once the watch is running; the program waits for a line.
		time.Sleep(200 * time.Millisecond)
		if err := m.Send(ctx, "w1", "hello"); err != nil {
			t.Errorf("send: %v", err)
		}
	}()
	err = m.Watch(wctx, func(ws []Worker) {
		for _, w := range ws {
			if w.Name != "w1" {
				continue
			}
			if w.Issue != "158" || w.Role != "echo" || w.Worktree != dir {
				t.Errorf("worker = %+v", w)
			}
			sawLive = sawLive || w.Live
			if !w.Live {
				sawEnd = true
				stop()
			}
		}
	})
	if err != nil && ctx.Err() != nil {
		t.Fatalf("watch: %v", err)
	}
	if !sawLive || !sawEnd {
		t.Fatalf("live %v, end %v", sawLive, sawEnd)
	}
	txt, err := m.Read(ctx, "w1", 0)
	if err != nil || !strings.Contains(txt, "got hello") {
		t.Errorf("read = %q, %v", txt, err)
	}
	ws, err := m.List(ctx, true)
	if err != nil || len(ws) != 1 {
		t.Errorf("list = %+v, %v", ws, err)
	}
}

// TestRealCoreSendKeysNoEnter checks that SendKeys presses the keys and no
// Enter: the program reads one raw byte and prints it.
func TestRealCoreSendKeysNoEnter(t *testing.T) {
	env := isolatedCore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()

	cfg := Config{Profiles: map[string]Profile{"one": {Argv: []string{"sh", "-c", "stty raw -echo; dd bs=1 count=2 2>/dev/null | od -An -c"}}}}
	m := New(HC{cl}, cfg)
	if _, err := m.Spawn(ctx, Spec{Name: "k1", Profile: "one", Issue: "148", Worktree: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := m.SendKeys(ctx, "k1", []byte("1")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	out, err := m.Read(ctx, "k1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `\r`) || strings.Contains(out, `\n  `) {
		t.Errorf("screen has an Enter: %q", out)
	}
	// One byte is in; the program still waits for the second.
	if strings.Contains(out, "1") && strings.Contains(out, "0000") {
		t.Errorf("program finished after one key: %q", out)
	}
	if err := m.SendKeys(ctx, "k1", []byte("\x1b[B")); err != nil {
		t.Fatal(err)
	}
	var seen string
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		seen, _ = m.Read(ctx, "k1", 0)
		if strings.Contains(seen, `1`) && strings.Contains(seen, `033`) {
			break
		}
	}
	// "1" then ESC: two bytes, no CR. Arrow sequences go as ESC [ B, so the
	// reader got "1" and the first byte of the sequence.
	if !strings.Contains(seen, "1") || !strings.Contains(seen, "033") || strings.Contains(seen, `\r`) {
		t.Errorf("screen = %q, want bytes 1 and ESC and no CR", seen)
	}
}
