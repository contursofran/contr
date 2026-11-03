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
