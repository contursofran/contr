//go:build e2e

package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestExplore is the engine of scripts/explore.sh (#522). It sets up the
// fixture CONTR_EXPLORE names in the harness (a scratch core, scratch home and
// folders), starts the release binary CONTR_EXPLORE_BIN in the theme of
// CONTR_EXPLORE_THEME, and writes CONTR_EXPLORE_INFO (lines app=, pid=,
// testpid=, scratch=, hc_session=). Then it keeps the app and the core open until the
// file CONTR_EXPLORE_STOP exists (a stop file that exists earlier ends the run
// before the app starts) or the test gets SIGINT or SIGTERM. The
// harness cleanup then ends the core and removes the scratch folder.
// Without CONTR_EXPLORE it skips.
func TestExplore(t *testing.T) {
	name := os.Getenv("CONTR_EXPLORE")
	if name == "" {
		t.Skip("set CONTR_EXPLORE to a fixture (scripts/explore.sh does)")
	}
	f, ok := seeFixtures[name]
	if !ok {
		t.Fatalf("explore: no fixture %q", name)
	}
	bin, info, stop := os.Getenv("CONTR_EXPLORE_BIN"), os.Getenv("CONTR_EXPLORE_INFO"), os.Getenv("CONTR_EXPLORE_STOP")
	if bin == "" || info == "" || stop == "" {
		t.Fatal("explore: set CONTR_EXPLORE_BIN, CONTR_EXPLORE_INFO and CONTR_EXPLORE_STOP")
	}
	theme := os.Getenv("CONTR_EXPLORE_THEME")
	if theme == "" {
		theme = "dark"
	}
	if theme != "dark" && theme != "light" {
		t.Fatalf("explore: theme %q: want dark or light", theme)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// A stop that came during the set-up ends the run before the app starts.
	stopped := func() bool { _, err := os.Stat(stop); return err == nil }
	h := newHarness(t)
	f.setup(t, h)
	h.theme(t, theme)
	if stopped() || ctx.Err() != nil {
		return
	}

	// The release build has no automation plugin, so the socket is empty.
	app := exec.Command(bin)
	app.Env = h.appEnv("")
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Process.Kill(); _ = app.Wait() })
	if stopped() {
		return
	}

	session := ""
	for _, v := range h.env {
		if s, ok := strings.CutPrefix(v, "HC_SESSION="); ok {
			session = s
		}
	}
	body := "app=" + filepath.Base(bin) + "\npid=" + strconv.Itoa(app.Process.Pid) + "\ntestpid=" + strconv.Itoa(os.Getpid()) +
		"\nscratch=" + h.dir + "\nhc_session=" + session + "\n"
	tmp := info + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, info); err != nil {
		t.Fatal(err)
	}

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	limit := time.After(8 * time.Hour)
	for {
		select {
		case <-ctx.Done():
			return
		case <-limit:
			return
		case <-tick.C:
			if stopped() {
				return
			}
		}
	}
}
