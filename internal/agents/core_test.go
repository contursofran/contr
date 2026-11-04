//go:build hccore

// This test runs against a real herdr-core, isolated from the owner's core:
// its own HC_SESSION and scratch XDG folders. Run it with
//
//	go test -tags hccore ./internal/agents
package agents

import (
	"context"
	"github.com/contursofran/contr/internal/hc/hctest"
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
	return hctest.New(t, "hc", hctest.Session("a")).Env
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

// TestRealCoreStopNote: hc types an exit note into a parent agent when its
// child exits. A stop with the parent as caller must not cause that note;
// a stop by someone else must, so the test shows that the note can come.
func TestRealCoreStopNote(t *testing.T) {
	for _, byParent := range []bool{false, true} {
		env := isolatedCore(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cl, err := hc.Dial(ctx, hc.Command("", env))
		if err != nil {
			t.Fatal(err)
		}
		defer cl.Close()

		// The parent says it is an idle agent (OSC 7501), then echoes what
		// is typed into it, notes included.
		var boss struct {
			Address string `json:"address"`
		}
		parent := []string{"sh", "-c", `printf '\033]7501;state=idle\033\134'; exec cat`}
		if err := cl.Call(ctx, "terminal.run", map[string]any{"argv": parent, "name": "boss", "wait": false, "no_parent": true}, &boss); err != nil {
			t.Fatal(err)
		}
		cfg := Config{Profiles: map[string]Profile{"sleep": {Argv: []string{"sleep", "60"}}}}
		m := New(HC{cl}, cfg)
		m.getenv = func(string) string { return "" }
		if byParent {
			m.getenv = func(k string) string {
				if k == "HC_TERMINAL_ID" {
					return boss.Address
				}
				return ""
			}
		}
		if _, err := m.Spawn(ctx, Spec{Name: "w1", Profile: "sleep", Parent: boss.Address}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(300 * time.Millisecond)
		if err := m.Stop(ctx, "w1"); err != nil {
			t.Fatal(err)
		}

		var screen string
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			var res struct {
				Text string `json:"text"`
			}
			if err := cl.Call(ctx, "terminal.read", map[string]any{"terminal": "boss"}, &res); err != nil {
				t.Fatal(err)
			}
			if screen = res.Text; strings.Contains(screen, "exited") {
				break
			}
		}
		noted := strings.Contains(screen, "w1 exited")
		if byParent && noted {
			t.Errorf("stop by the parent: parent got a note: %q", screen)
		}
		if !byParent && !noted {
			t.Errorf("stop by another: no note in the parent: %q", screen)
		}
	}
}
