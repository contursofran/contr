//go:build e2e

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/hc/hctest"
	"github.com/contursofran/contr/internal/usage"
)

// TestPlanUsageInTheSidebarFoot starts the built app on a scratch core with
// scratch plan data (Claude 38%, Codex 12% used) and checks that the plan
// meters show in the sidebar foot, left of Settings, in Dark and Light
// (D196). PNGs go to tmp/e2e/planusage-*.png. It never touches the owner's core:
//
//	go test -tags e2e -run TestPlanUsageInTheSidebarFoot ./cmd/contr
func TestPlanUsageInTheSidebarFoot(t *testing.T) {
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
	core := hctest.New(t, "contr-pu", hctest.Session("pu"))
	dir := core.Dir
	home, state, codex := filepath.Join(dir, "home"), filepath.Join(dir, "state"), filepath.Join(dir, "codex")
	reset := time.Now().Add(3 * time.Hour).Unix()
	snap := usage.ParseStatusline(fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":38,"resets_at":%d}}}`, reset), uint64(time.Now().Unix()))
	if snap == nil {
		t.Fatal("no Claude snapshot")
	}
	if _, err := usage.WriteSnapshot(state, snap); err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(codex, "sessions", time.Now().Format("2006/01/02"))
	line := fmt.Sprintf(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":12,"window_minutes":300,"resets_at":%d}}}}`+"\n", reset)
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(day, "rollout-e2e.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
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

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	labels := func() map[string]bool {
		m := map[string]bool{}
		for _, n := range c.nodes(t) {
			m[n.Label] = true
		}
		return m
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
			if ok() {
				return
			}
		}
		t.Fatalf("timed out: %s; labels %v", what, keys(c.labels(t)))
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}
	waitFor("plan values", func() bool { tx := labels(); return tx["38%"] && tx["12%"] })
	for _, theme := range []string{"Dark", "Light"} {
		c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
		waitFor("settings", func() bool { return labels()[settings.LabelSettings] })
		click(theme)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "key", "key": "Escape"}}})
		waitFor("settings closed", func() bool { return !labels()[settings.LabelSettings] })
		time.Sleep(500 * time.Millisecond)
		claude, codexBar, gear := c.boundsOf(t, "Claude meter"), c.boundsOf(t, "Codex meter"), c.boundsOf(t, sidebar.LabelSettingsOpen)
		if !(claude.X < codexBar.X && codexBar.X+codexBar.Width < gear.X) {
			t.Errorf("%s: order Claude %v, Codex %v, gear %v", theme, claude, codexBar, gear)
		}
		if l := labels(); l["Claude line"] || l["Codex line"] {
			t.Errorf("%s: the files panel still has plan lines", theme)
		}
		c.capture(t, filepath.Join(out, "planusage-"+theme+".png"))
	}
}
