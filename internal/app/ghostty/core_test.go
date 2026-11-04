//go:build hccore

package ghostty

import (
	"context"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// An hc terminal shows in the plugin's view: typing reaches the program, its
// output and the size come back, and closing the view leaves it running.
func TestViewOnHC(t *testing.T) {
	core := hctest.New(t, "ctg", hctest.Session("g"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sh"] = agents.Profile{Argv: []string{"sh"}}
	w, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "w1", Profile: "sh", Role: "builder", Project: "contr"})
	if err != nil {
		t.Fatal(err)
	}
	conn := NewConn(NewCoreSource(hc.Command("", core.Env), w.Address), Options{})
	term, err := terminal.New(terminal.Options{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 640, 400)
	tt.Frame()
	tt.Type("echo hi-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	wait := func(what string, ok func() bool) {
		t.Helper()
		for !ok() {
			if ctx.Err() != nil {
				t.Fatalf("timed out waiting for %s; screen:\n%s", what, term.Text())
			}
			time.Sleep(20 * time.Millisecond)
			tt.Frame()
		}
	}
	wait("output", func() bool { return strings.Contains(term.Text(), "hi-42") })
	cols, rows := term.Size()
	wait("hc size", func() bool {
		var tm struct{ Cols, Rows int }
		if err := cl.Call(ctx, "terminal.get", map[string]any{"terminal": w.Address}, &tm); err != nil {
			t.Fatal(err)
		}
		return tm.Cols == cols && tm.Rows == rows && cols != 80
	})
	if out := os.Getenv("GHOSTTY_CAPTURE"); out != "" {
		tt.SetDark(true)
		tt.Type(`printf '\033[1;32mgreen bold\033[0m \033[38;2;255;120;0mtruecolor\033[0m ─┼─ 漢字 ✓\n'; vim -u NONE -c 'syntax on' -c 'set nu' /etc/zprofile`)
		tt.Key(0, ui.KeyEnter)
		wait("vim", func() bool { return strings.Contains(term.Text(), "zprofile") })
		time.Sleep(1500 * time.Millisecond)
		tt.Frame()
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		tt.Type(":q!")
		tt.Key(0, ui.KeyEnter)
	}
	term.Close()
	var tm struct{ State string }
	if err := cl.Call(ctx, "terminal.get", map[string]any{"terminal": w.Address}, &tm); err != nil {
		t.Fatal(err)
	}
	t.Logf("view %dx%d; after close hc says state=%q", cols, rows, tm.State)
}

// While contr watches, a second viewer keeps its size and contr's screen
// shows it; the first key makes contr's view size the terminal (D91).
func TestWatchKeepsASecondViewersSize(t *testing.T) {
	core := hctest.New(t, "ctw", hctest.Session("w"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	cfg := agents.DefaultConfig()
	cfg.Profiles["sh"] = agents.Profile{Argv: []string{"sh"}}
	w, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{Name: "w1", Profile: "sh", Role: "builder"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := hc.DialFramed(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err := other.Attach(ctx, hc.AttachParams{Terminal: w.Address, Cols: 101, Rows: 31}); err != nil {
		t.Fatal(err)
	}
	hcSize := func() (int, int) {
		var tm struct{ Cols, Rows int }
		if err := cl.Call(ctx, "terminal.get", map[string]any{"terminal": w.Address}, &tm); err != nil {
			t.Fatal(err)
		}
		return tm.Cols, tm.Rows
	}

	conn := NewConn(NewCoreSource(hc.Command("", core.Env), w.Address), Options{Watch: true})
	term, err := terminal.New(terminal.Options{Conn: conn, ResizeByConn: true})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	conn.SetScreen(term)
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill().AutoFocus() }, 640, 400)
	wait := func(what string, ok func() bool) {
		t.Helper()
		for !ok() {
			if ctx.Err() != nil {
				t.Fatalf("timed out waiting for %s; screen:\n%s", what, term.Text())
			}
			time.Sleep(20 * time.Millisecond)
			tt.Frame()
		}
	}
	wait("the core's size on the screen", func() bool { c, r := term.Size(); return c == 101 && r == 31 })
	time.Sleep(300 * time.Millisecond)
	tt.Frame()
	if c, r := hcSize(); c != 101 || r != 31 {
		t.Fatalf("hc size %dx%d while contr watches, want 101x31", c, r)
	}

	tt.Type("echo hi-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	wait("output", func() bool { return strings.Contains(term.Text(), "hi-42") })
	wait("contr's size", func() bool {
		c, r := hcSize()
		tc, tr := term.Size()
		return c != 101 && c == tc && r == tr
	})
}
