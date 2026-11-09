//go:build e2e

package main

import (
	"image"
	"image/color"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/hc/hctest"
)

// TestWindowGaps opens the built app in Dark and in Light and measures, in the capture, the
// gaps around and between the panels: left edge to the sidebar, sidebar to terminal, terminal
// to files, files to the right edge, the panels to the bottom edge and the title bar to the
// panels (D167). All must be theme.PanelGap points. PNGs go to tmp/e2e/gaps-dark.png and
// gaps-light.png. HOME is a scratch folder and the core is a scratch core:
//
//	go test -tags e2e -run TestWindowGaps ./cmd/contr
func TestWindowGaps(t *testing.T) {
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
	core := hctest.New(t, "contr-gp", hctest.Session("gp"))
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

	c := dial(t, sock)
	defer c.conn.Close()
	c.waitWindow(t)
	has := func(label string) bool { return c.labels(t)[label] }
	for deadline := time.Now().Add(20 * time.Second); !has(app.LabelNewTab) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	// A plain terminal gives the tab row a tab to measure.
	nb := c.boundsOf(t, app.LabelNewTab)
	c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": nb.X + nb.Width/2, "y": nb.Y + nb.Height/2}}})
	for deadline := time.Now().Add(20 * time.Second); c.boundsOf(t, app.LabelTabs).Width == 0 && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
	for deadline := time.Now().Add(20 * time.Second); !has(settings.LabelSettings) && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
	}
	click := func(label string) {
		t.Helper()
		b := c.boundsOf(t, label)
		c.call(t, "input", map[string]any{"events": []map[string]any{{"type": "click", "x": b.X + b.Width/2, "y": b.Y + b.Height/2}}})
		time.Sleep(600 * time.Millisecond)
	}
	for _, name := range []string{"Dark", "Light"} {
		click(name)
		// Close Settings so the panels are not under its veil.
		c.key(t, "Escape")
		time.Sleep(600 * time.Millisecond)
		path := filepath.Join(out, "gaps-"+name+".png")
		c.capture(t, path)
		measureGaps(t, c, path)
		if name == "Dark" {
			c.call(t, "menu", map[string]any{"path": []string{"contr", "Settings…"}})
			time.Sleep(600 * time.Millisecond)
		}
	}
}

// measureGaps prints the gaps in points: from the node bounds (the layout) and from the pixels of
// the capture at path (the scan of a row and a column through the panels).
func measureGaps(t *testing.T, c *client, path string) {
	t.Helper()
	sb, tm, fl := c.boundsOf(t, app.LabelPanelSidebar), c.boundsOf(t, app.LabelPanelTerminal), c.boundsOf(t, app.LabelPanelFiles)
	bar := c.boundsOf(t, app.LabelTitleBar)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	scale := float64(b.Dx()) / (fl.X + fl.Width + float64(theme.PanelGap))
	t.Logf("capture %dx%d px, scale %.2f", b.Dx(), b.Dy(), scale)
	t.Logf("layout: left %.1f  sidebar|terminal %.1f  terminal|files %.1f  right %.1f  bottom/title bar %.1f",
		sb.X, tm.X-(sb.X+sb.Width), fl.X-(tm.X+tm.Width), float64(b.Dx())/scale-(fl.X+fl.Width), tm.Y-(bar.Y+bar.Height))
	// Pixels: the gap color is the window's own pixel in the bottom-left corner area.
	px := func(x, y float64) color.Color { return img.At(int(x*scale), int(y*scale)) }
	ym := tm.Y + tm.Height/2
	gap := px(2, ym) // inside the left gap
	var runs []float64
	run := 0
	for x := 0; x < b.Dx(); x++ {
		if near(img.At(x, int(ym*scale)), gap) {
			run++
			continue
		}
		if run > 0 {
			runs = append(runs, float64(run)/scale)
		}
		run = 0
	}
	if run > 0 {
		runs = append(runs, float64(run)/scale)
	}
	t.Logf("pixel row gaps (points, left to right): %.1f", runs)
	wantGaps(t, "row", runs, 4)
	xm := tm.X + tm.Width/2
	var cols []float64
	run = 0
	for y := int((bar.Y + bar.Height) * scale); y < b.Dy(); y++ {
		if near(img.At(int(xm*scale), y), gap) {
			run++
			continue
		}
		if run > 0 {
			cols = append(cols, float64(run)/scale)
		}
		run = 0
	}
	if run > 0 {
		cols = append(cols, float64(run)/scale)
	}
	t.Logf("pixel column gaps under the title bar (points, top to bottom): %.1f", cols)
	wantGaps(t, "column", cols, 2)
	// The tab row (D168): down a column through the first tab, in points: the glass above the tab
	// (window rim to the tab), the tab, and the glass below it to the terminal panel's border.
	tabs := c.boundsOf(t, app.LabelTabs)
	tx := int((tabs.X + 16) * scale) // past the panel's corner radius
	var seg []float64
	run, last := 0, false
	for y := 0; y < b.Dy() && float64(y) < (tm.Y+4)*scale; y++ {
		g := near(img.At(tx, y), gap)
		if g != last && y > 0 {
			seg = append(seg, float64(run)/scale)
			run = 0
		}
		run++
		last = g
	}
	t.Logf("pixel column through the first tab (points, glass/tab/glass, down to the panel top %.1f): %.1f", tm.Y, seg)
	if len(seg) < 3 {
		t.Fatalf("tab column: %d segments %.1f, want glass, tab, glass", len(seg), seg)
	}
	if top := seg[0]; math.Abs(top-(bar.Height-theme.SizeRowHeight)/2) > 0.5 {
		t.Errorf("rim to tab %.1f points, want %.1f", top, (bar.Height-theme.SizeRowHeight)/2)
	}
	if h := seg[1]; math.Abs(h-theme.SizeRowHeight) > 0.5 {
		t.Errorf("tab %.1f points high, want %d", h, theme.SizeRowHeight)
	}
}

// wantGaps checks that the runs of gap color, 1 point or more (a thinner run is the edge of a
// panel's border), are n and each theme.PanelGap points wide.
func wantGaps(t *testing.T, what string, runs []float64, n int) {
	t.Helper()
	var got []float64
	for _, r := range runs {
		if r >= 1 {
			got = append(got, r)
		}
	}
	if len(got) != n {
		t.Errorf("%s: %d gaps %.1f, want %d", what, len(got), got, n)
	}
	for _, r := range got {
		if math.Abs(r-theme.PanelGap) > 0.5 {
			t.Errorf("%s: gap %.1f points, want %d", what, r, theme.PanelGap)
		}
	}
}

func near(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	d := func(x, y uint32) float64 { return math.Abs(float64(x>>8) - float64(y>>8)) }
	return d(ar, br) < 3 && d(ag, bg) < 3 && d(ab, bb) < 3
}
