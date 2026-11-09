//go:build e2e

package main

import (
	"context"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/titlebar"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestAppearanceRow opens Settings in the built app, picks Dark, then Light, and checks that the
// choice reaches settings.json. HOME is a scratch folder, so the owner's settings stay as they
// are. PNGs go to tmp/e2e/appearance-*.png. It never touches the owner's core:
//
//	go test -tags e2e -run TestAppearanceRow ./cmd/contr
func TestAppearanceRow(t *testing.T) {
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
	core := hctest.New(t, "contr-ap", hctest.Session("ap"))
	dir := core.Dir
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "a.sock")
	appCmd := exec.Command(bin)
	appCmd.Env = append(os.Environ(), append(core.Env, "MYGO_AUTOMATION="+sock, "HOME="+home,
		"SHELL=/bin/sh",
		"CONTR_STATE_DIR="+filepath.Join(dir, "state"), "CODEX_HOME="+filepath.Join(dir, "codex"),
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
	has := func(label string) bool {
		for _, n := range c.nodes(t) {
			if n.Label == label {
				return true
			}
		}
		return false
	}
	wait := func(label string) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); !has(label) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "pointerMove", "x": 700, "y": 500}}})
		}
		if !has(label) {
			t.Fatalf("no %q; labels %v", label, keys(c.labels(t)))
		}
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}
	stored := func() string {
		data, _ := os.ReadFile(filepath.Join(home, "Library", "Application Support", "contr", "settings.json"))
		return string(data)
	}

	wait(titlebar.LabelNewTab)
	c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
	wait(settings.LabelSettings)
	time.Sleep(500 * time.Millisecond)
	c.capture(t, filepath.Join(out, "appearance-0-system.png"))

	click("Dark")
	c.capture(t, filepath.Join(out, "appearance-1-dark.png"))
	// Dark: the card is the Dark ground.
	checkGround(t, c, filepath.Join(out, "appearance-1-dark.png"), theme.Dark)
	click("Light")
	light := filepath.Join(out, "appearance-2-light.png")
	c.capture(t, light)
	// Light (#400): the open dialog and the window ground take the Light palette at once. The card
	// is the ground token; the window edge is the glass-less frame under the veil, near it.
	checkGround(t, c, light, theme.Light)
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(stored(), `"light"`) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	if got := stored(); !strings.Contains(got, `"appearance": "light"`) {
		t.Errorf("settings.json = %q, want light", got)
	}
}

// checkGround reads the capture at path: a pixel of the Settings card must be the ground token of
// p, and a pixel of the window edge must be near it (the veil over the frame is the same ground).
func checkGround(t *testing.T, c *client, path string, p *theme.Palette) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := c.boundsOf(t, settings.LabelSettings)
	// The card is centered, so twice its center is the window width in DIPs.
	scale := float64(img.Bounds().Dx()) / (2 * (b.X + b.Width/2))
	at := func(x, y float64) [3]int {
		return rgb(img, int(x*scale), int(y*scale))
	}
	want := [3]int{int(p.Ground.R), int(p.Ground.G), int(p.Ground.B)}
	near := func(got [3]int, tol int) bool {
		for i := range got {
			if d := got[i] - want[i]; d > tol || d < -tol {
				return false
			}
		}
		return true
	}
	if got := at(b.X+6, b.Y+6); !near(got, 2) {
		t.Errorf("%s: card pixel %v, want ground %v", filepath.Base(path), got, want)
	}
	if got := at(1, float64(img.Bounds().Dy())/scale/2); !near(got, 14) {
		t.Errorf("%s: window pixel %v, want near ground %v", filepath.Base(path), got, want)
	}
}

func rgb(img image.Image, x, y int) [3]int {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}
