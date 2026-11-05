//go:build e2e

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/hc/hctest"
	"github.com/contursofran/contr/internal/usage"
)

// TestUsageMeters runs the built app against a scratch hc core with fake plan
// usage: a Claude status line (62 % of the 5-hour window used, 18 % of the
// 7-day window) and a Codex rollout log (27 % used). The files panel must show
// Context with no data, Claude 62 % and Codex 27 %. It captures the window in
// Dark and in Light to tmp/e2e/usage-*.png. HOME, the state folder and
// CODEX_HOME are scratch folders; it never reads ~/.claude or ~/.codex and
// never touches the owner's core:
//
//	go test -tags e2e -run TestUsageMeters ./cmd/contr
func TestUsageMeters(t *testing.T) {
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
	core := hctest.New(t, "contr-um", hctest.Session("um"))
	dir := core.Dir
	home, state, codex := filepath.Join(dir, "home"), filepath.Join(dir, "state"), filepath.Join(dir, "codex")
	for _, d := range []string{home, state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// The fake Claude status line is parsed as the real one is.
	reset := time.Now().Add(3 * time.Hour).Unix()
	line := fmt.Sprintf(`{"version":"2.1.0","rate_limits":{"five_hour":{"used_percentage":62,"resets_at":%d},"seven_day":{"used_percentage":18,"resets_at":%d}}}`, reset, reset+86400)
	snap := usage.ParseStatusline(line, usage.UnixNow())
	if snap == nil {
		t.Fatal("the fake status line does not parse")
	}
	if _, err := usage.WriteSnapshot(state, snap); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(codex, "sessions", "2026", "11", "05", "rollout-fake.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o755); err != nil {
		t.Fatal(err)
	}
	token := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":27.0,"resets_at":%d}}}}`+"\n", reset)
	if err := os.WriteFile(rollout, []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}

	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home, "SHELL=/bin/sh",
		"CONTR_STATE_DIR="+state, "CODEX_HOME="+codex,
		"CONTR_OWNER_FILE="+filepath.Join(dir, "owner.json"), "CONTR_AGENTS_CONFIG="+filepath.Join(dir, "agents.json"))...)
	if err := appCmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = appCmd.Process.Kill(); _ = appCmd.Wait() }()
	_, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	poke := func() {
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
	}
	has := func(label string) bool { return c.labels(t)[label] }
	for deadline := time.Now().Add(20 * time.Second); !has("Codex meter") && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		poke()
	}
	for _, label := range []string{"usage", "Context meter", "Claude meter", "Codex meter"} {
		if !has(label) {
			t.Fatalf("no %q; labels %v", label, keys(c.labels(t)))
		}
	}
	// The meters sit under the tree, inside the files panel.
	files, meters := c.boundsOf(t, app.LabelPanelFiles), c.boundsOf(t, "usage")
	if meters.Y+meters.Height > files.Y+files.Height || meters.X < files.X {
		t.Errorf("meters %+v are outside the files panel %+v", meters, files)
	}
	time.Sleep(500 * time.Millisecond)

	pick := func(appearance string) {
		t.Helper()
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		for deadline := time.Now().Add(10 * time.Second); !has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		b := c.boundsOf(t, appearance)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(400 * time.Millisecond)
		c.key(t, "Escape")
		for deadline := time.Now().Add(5 * time.Second); has(app.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			poke()
		}
		time.Sleep(600 * time.Millisecond)
	}
	pick("Dark")
	c.capture(t, filepath.Join(out, "usage-dark.png"))
	pick("Light")
	c.capture(t, filepath.Join(out, "usage-light.png"))
}
