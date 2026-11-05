package app

import (
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
)

var contrText = "contr " + Version

type fakeHost struct {
	mu    sync.Mutex
	stats host.Stats
	has   bool
}

func (f *fakeHost) Stats() (host.Stats, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats, f.has
}

func (f *fakeHost) set(s host.Stats) {
	f.mu.Lock()
	f.stats, f.has = s, true
	f.mu.Unlock()
}

func stats(cpu float32, used, total uint64) host.Stats {
	return host.Stats{CPUPercent: cpu, MemUsed: used, MemTotal: total}
}

type fakeMessage struct {
	text string
	has  bool
}

func (f *fakeMessage) Text() (string, bool) { return f.text, f.has }

// fakeCore is the hc core version the agents report; empty while contr does
// not follow a core.
type fakeCore struct {
	mu sync.Mutex
	v  string
}

func (f *fakeCore) set(v string) { f.mu.Lock(); f.v = v; f.mu.Unlock() }
func (f *fakeCore) snap() agentstate.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return agentstate.Snapshot{CoreVersion: f.v}
}
func (f *fakeCore) version() (string, bool) { s := f.snap(); return s.CoreVersion, s.CoreVersion != "" }

// barFrame builds a frame of width x 900 whose agents follow a core of
// version v ("" for none), with the fake sources.
func barFrame(t *testing.T, h *fakeHost, v string, width int) (*Frame, *fakeCore, *fakeMessage, *ui.Tester) {
	t.Helper()
	core := &fakeCore{v: v}
	m := &fakeMessage{}
	f := NewFrame()
	f.agentSnap = core.snap
	f.status.host, f.status.message = h, m
	tt := ui.NewTester(f.View, width, theme.WindowHeight)
	return f, core, m, tt
}

func readingHost(s host.Stats) *fakeHost { return &fakeHost{stats: s, has: true} }

func rightTexts(f *Frame, tt *ui.Tester) []string {
	tt.Frame()
	return f.status.rightTexts()
}

func TestShowsCPUAndMemoryThenTheVersions(t *testing.T) {
	f, _, _, tt := barFrame(t, readingHost(stats(37.6, 62, 100)), "0.1.18", theme.WindowWidth)
	want := []string{"CPU 38%", "MEM 62%", contrText, "hc 0.1.18"}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
	box(t, tt, LabelHostCPU)
	mem, version := box(t, tt, LabelHostMem), box(t, tt, LabelContrVersion)
	if version.X <= mem.X {
		t.Errorf("the version at x=%v should be right of MEM at x=%v", version.X, mem.X)
	}
	for _, text := range []string{"CPU", "38%", "MEM", "62%", contrText, "hc 0.1.18"} {
		if !tt.HasText(text) {
			t.Errorf("the bar does not draw %q; texts %q", text, tt.Texts())
		}
	}
}

func TestNoReadingShowsDashes(t *testing.T) {
	f, _, _, tt := barFrame(t, &fakeHost{}, "0.1.18", theme.WindowWidth)
	want := []string{"CPU –", "MEM –", contrText, "hc 0.1.18"}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
}

func TestTheCoreVersionShowsAfterContr(t *testing.T) {
	f, _, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	if got := rightTexts(f, tt)[3]; got != "hc 0.1.18" {
		t.Errorf("fourth item %q, want the hc version", got)
	}
	contr, hv := box(t, tt, LabelContrVersion), box(t, tt, LabelCoreVersion)
	if hv.X <= contr.X {
		t.Errorf("hc at x=%v should be right of contr at x=%v", hv.X, contr.X)
	}
}

func TestNoCoreShowsNoVersion(t *testing.T) {
	f, _, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "", theme.WindowWidth)
	want := []string{"CPU 40%", "MEM 50%", contrText}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
	if !absent(tt, LabelCoreVersion) {
		t.Error("the hc version should not show")
	}
}

func TestTheCoreVersionFollowsTheAgents(t *testing.T) {
	f, core, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "", theme.WindowWidth)
	tt.Frame()
	core.set("0.1.18")
	tt.Frame()
	if got := f.status.rightTexts()[3]; got != "hc 0.1.18" {
		t.Errorf("fourth item %q, want the hc version", got)
	}
	core.set("")
	if got, want := rightTexts(f, tt), []string{"CPU 40%", "MEM 50%", contrText}; !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
}

// counted returns a bar over fakes at 40 % CPU and 50 % memory and core
// version 0.1.18.
func counted() (*statusBar, *fakeHost, *fakeCore) {
	h, core := readingHost(stats(40, 50, 100)), &fakeCore{v: "0.1.18"}
	b := &statusBar{host: h, message: &fakeMessage{}, core: core.version}
	b.refresh()
	return b, h, core
}

func TestAChangedReadingRepaintsTheBarOnce(t *testing.T) {
	b, h, _ := counted()
	if got := b.rightTexts()[0]; got != "CPU 40%" {
		t.Fatalf("first item %q", got)
	}
	h.set(stats(55, 50, 100))
	if !b.refresh() {
		t.Error("a changed reading should repaint")
	}
	if b.refresh() {
		t.Error("the same reading should not repaint again")
	}
	if got := b.rightTexts()[0]; got != "CPU 55%" {
		t.Errorf("first item %q, want CPU 55%%", got)
	}
}

func TestANewCoreVersionRepaintsTheBarOnce(t *testing.T) {
	b, _, core := counted()
	core.set("0.1.19")
	if !b.refresh() {
		t.Error("a new version should repaint")
	}
	if b.refresh() {
		t.Error("the same version should not repaint again")
	}
	core.set("")
	if !b.refresh() {
		t.Error("a lost core should repaint")
	}
}

// The Rust test also let the message time out. The Go message clears itself in state.StatusMessage,
// and its test covers the time. Here the slot is a fake that the test empties.
func TestMessageSitsLeftOfTheReadingsAndClearsAfterItsTTL(t *testing.T) {
	f, _, m, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	m.text, m.has = "D7 → By type · sent", true
	if got := rightTexts(f, tt)[0]; got != m.text {
		t.Errorf("first item %q, want the message", got)
	}
	if msg, cpu := box(t, tt, LabelStatusMsg), box(t, tt, LabelHostCPU); msg.X >= cpu.X {
		t.Errorf("message at x=%v should be left of CPU at x=%v", msg.X, cpu.X)
	}

	m.text, m.has = "", false
	want := []string{"CPU 40%", "MEM 50%", contrText, "hc 0.1.18"}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
	if !absent(tt, LabelStatusMsg) {
		t.Error("the message slot should be empty")
	}
}

func TestNarrowWindowKeepsTheReadingsInsideTheBar(t *testing.T) {
	const width = 960
	_, _, m, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", width)
	m.text, m.has = strings20("a very long message "), true
	tt.Frame()
	msg, cpu := box(t, tt, LabelStatusMsg), box(t, tt, LabelHostCPU)
	if msg.X+msg.W > cpu.X {
		t.Errorf("message ends at %v, after CPU starts at %v: it should truncate, not CPU", msg.X+msg.W, cpu.X)
	}
	if right := box(t, tt, LabelStatusRight); right.X+right.W > width {
		t.Errorf("right side ends at %v, past the window", right.X+right.W)
	}
	for _, label := range []string{LabelHostCPU, LabelHostMem, LabelContrVersion, LabelCoreVersion} {
		if r := box(t, tt, label); r.X+r.W > width {
			t.Errorf("%s ends at %v, past the window", label, r.X+r.W)
		}
	}
	if got := box(t, tt, LabelStatusBar).H; got != theme.StatusBarHeight {
		t.Errorf("bar height %v, want %v", got, theme.StatusBarHeight)
	}
}

func strings20(s string) string {
	out := ""
	for range 20 {
		out += s
	}
	return out
}

func TestAnUpdateReadyItemShowsAndAClickInstalls(t *testing.T) {
	f, _, _, tt := barFrame(t, readingHost(stats(37.6, 62, 100)), "0.1.18", theme.WindowWidth)
	u := &fakeUpdates{enabled: true}
	f.Settings = &Settings{Updates: u, Run: Runner{Go: func(fn func()) { fn() }}}
	if got := rightTexts(f, tt); slices.Contains(got, TextUpdateReady) {
		t.Fatalf("the item shows without an update: %q", got)
	}
	f.Settings.found = &UpdateInfo{Version: "1.3.0"}
	got := rightTexts(f, tt)
	if len(got) == 0 || !slices.Contains(got, TextUpdateReady) {
		t.Fatalf("no update item in %q", got)
	}
	if u.installs != 0 {
		t.Fatal("contr installed without a click")
	}
	tt.Click(LabelUpdateReady)
	if u.installs != 1 {
		t.Errorf("a click ran %d installs, want 1", u.installs)
	}
}
