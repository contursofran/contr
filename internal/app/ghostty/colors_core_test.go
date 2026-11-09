//go:build hccore

package ghostty

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/hc"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// A terminal spawned with display colors answers a program's OSC 11 query with
// the background it was given (#521).
func TestSpawnColorsAnswerOSC11(t *testing.T) {
	core := hctest.New(t, "ctc", hctest.Session("c"))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cl, err := hc.Dial(ctx, hc.Command("", core.Env))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	script := `stty raw -echo; printf '\033]11;?\033\\'; dd bs=1 count=24 2>/dev/null | od -An -c | tr -d ' \n'; echo; echo END; sleep 30`
	cfg := agents.DefaultConfig()
	cfg.Profiles["q"] = agents.Profile{Argv: []string{"sh", "-c", script}}
	w, err := agents.New(agents.HC{Client: cl}, cfg).Spawn(ctx, agents.Spec{
		Name: "q1", Profile: "q", Role: "builder", Project: "contr",
		Colors: &agents.Colors{FG: "#1a1a1a", BG: "#f5f5f3", Cursor: "#1e1e1e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// A viewer with input must attach for hc to answer the query.
	conn := NewConn(NewCoreSource(hc.Command("", core.Env), w.Address), Options{})
	defer conn.Close()
	_ = conn.ResizePixels(80, 24, 8, 16)
	for ctx.Err() == nil {
		var out struct{ Text string }
		raw := json.RawMessage{}
		if err := cl.Call(ctx, "terminal.read", map[string]any{"terminal": w.Address}, &raw); err == nil {
			_ = json.Unmarshal(raw, &out)
			if strings.Contains(out.Text, "END") {
				if !strings.Contains(out.Text, "f5f5/f5f5/f3f3") {
					t.Fatalf("reply %q has no light background", out.Text)
				}
				t.Logf("reply: %s", out.Text)
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no reply")
}
