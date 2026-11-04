package app

import (
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

const contrText = "contr " + Version

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

// fakeUsage gives the percent left per plan. A plan that is not in the map has no data.
type fakeUsage struct{ left map[usage.PlanID]float64 }

func (f *fakeUsage) Left(plan usage.PlanID, _ uint64) (float64, bool) {
	v, ok := f.left[plan]
	return v, ok
}

func plansLeft(claude, codex float64) *fakeUsage {
	return &fakeUsage{left: map[usage.PlanID]float64{usage.PlanClaude: claude, usage.PlanCodex: codex}}
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
func barFrame(t *testing.T, h *fakeHost, v string, width int) (*Frame, *fakeCore, *fakeUsage, *fakeMessage, *ui.Tester) {
	t.Helper()
	core := &fakeCore{v: v}
	u, m := &fakeUsage{left: map[usage.PlanID]float64{}}, &fakeMessage{}
	f := NewFrame(nil)
	f.agentSnap = core.snap
	f.status.host, f.status.usage, f.status.message = h, u, m
	tt := ui.NewTester(f.View, width, theme.WindowHeight)
	return f, core, u, m, tt
}

func readingHost(s host.Stats) *fakeHost { return &fakeHost{stats: s, has: true} }

func rightTexts(f *Frame, tt *ui.Tester) []string {
	tt.Frame()
	return f.status.rightTexts()
}

func TestShowsCPUAndMemoryThenTheVersions(t *testing.T) {
	f, _, _, _, tt := barFrame(t, readingHost(stats(37.6, 62, 100)), "0.1.18", theme.WindowWidth)
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
	f, _, _, _, tt := barFrame(t, &fakeHost{}, "0.1.18", theme.WindowWidth)
	want := []string{"CPU –", "MEM –", contrText, "hc 0.1.18"}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
}

func TestTheCoreVersionShowsAfterContr(t *testing.T) {
	f, _, _, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	if got := rightTexts(f, tt)[3]; got != "hc 0.1.18" {
		t.Errorf("fourth item %q, want the hc version", got)
	}
	contr, hv := box(t, tt, LabelContrVersion), box(t, tt, LabelCoreVersion)
	if hv.X <= contr.X {
		t.Errorf("hc at x=%v should be right of contr at x=%v", hv.X, contr.X)
	}
}

func TestNoCoreShowsNoVersion(t *testing.T) {
	f, _, _, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "", theme.WindowWidth)
	want := []string{"CPU 40%", "MEM 50%", contrText}
	if got := rightTexts(f, tt); !reflect.DeepEqual(got, want) {
		t.Errorf("right texts %q, want %q", got, want)
	}
	if !absent(tt, LabelCoreVersion) {
		t.Error("the hc version should not show")
	}
}

func TestTheCoreVersionFollowsTheAgents(t *testing.T) {
	f, core, _, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "", theme.WindowWidth)
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
	b := &statusBar{host: h, usage: &fakeUsage{}, message: &fakeMessage{}, core: core.version}
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

func fillOf(t *testing.T, tt *ui.Tester, label string) float32 {
	t.Helper()
	return box(t, tt, label).W
}

func TestEachPlanShowsItsLeftPercentAndABarToMatch(t *testing.T) {
	f, _, u, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	u.left = plansLeft(61.6, 40).left
	tt.Frame()
	if got, want := f.status.leftTexts(), []string{"claude 62%", "codex 40%"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left texts %q, want %q", got, want)
	}
	// The track is 36 px: 61.6 % of it is 22.2 px, 40 % is 14.4 px.
	claude, codex := fillOf(t, tt, "usage-claude-fill"), fillOf(t, tt, "usage-codex-fill")
	if math.Abs(float64(claude)-22.18) > 0.5 || math.Abs(float64(codex)-14.4) > 0.5 {
		t.Errorf("fills %v and %v px, want 22.2 and 14.4", claude, codex)
	}
	box(t, tt, "usage-claude-pct")
}

func TestALowPlanFillsWhiteAndTheOthersStayGrey(t *testing.T) {
	_, _, u, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	// Claude 62 % left, Codex 12 % left (under 20).
	u.left = plansLeft(62, 12).left
	tt.Frame()
	if absent(tt, "usage-codex-low") || !absent(tt, "usage-codex-fill") ||
		!absent(tt, "usage-claude-low") || absent(tt, "usage-claude-fill") {
		t.Errorf("only codex should be low; texts %q", tt.Texts())
	}
	// Exactly 20 % left is not under the limit.
	u.left = plansLeft(62, 20).left
	tt.Frame()
	if !absent(tt, "usage-codex-low") || absent(tt, "usage-codex-fill") {
		t.Error("20 % left should be grey")
	}
}

func TestALowPlanShowsItsPercentInWhiteAndTheOthersInSecondaryGrey(t *testing.T) {
	// At 0 to 2 % the fill is under 1 px, so the percent carries the low state.
	for _, left := range []float64{0, 2, 19} {
		if got := pctColour(left, true); got != theme.YouBg {
			t.Errorf("%v %% left: colour %06x, want white", left, got)
		}
	}
	// Exactly 20 % left is not under the limit, the same as the fill.
	for _, left := range []float64{20, 100} {
		if got := pctColour(left, true); got != theme.Text2 {
			t.Errorf("%v %% left: colour %06x, want secondary", left, got)
		}
	}
	// No data: no owner action, so no white.
	if got := pctColour(0, false); got != theme.Text2 {
		t.Errorf("no data: colour %06x, want secondary", got)
	}
}

func TestAPlanWithoutDataShowsADashAndAnEmptyTrack(t *testing.T) {
	f, _, u, _, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
	tt.Frame()
	if got, want := f.status.leftTexts(), []string{"claude –", "codex –"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left texts %q, want %q", got, want)
	}
	box(t, tt, "usage-claude-bar")
	if !absent(tt, "usage-claude-fill") || !absent(tt, "usage-claude-low") {
		t.Error("an empty track has no fill")
	}

	u.left = map[usage.PlanID]float64{usage.PlanClaude: 62}
	tt.Frame()
	if got, want := f.status.leftTexts(), []string{"claude 62%", "codex –"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left texts %q, want %q", got, want)
	}
	u.left = nil
	tt.Frame()
	if got, want := f.status.leftTexts(), []string{"claude –", "codex –"}; !reflect.DeepEqual(got, want) {
		t.Errorf("left texts %q, want %q", got, want)
	}
	if !absent(tt, "usage-claude-fill") {
		t.Error("no fill without data")
	}
}

// The Rust test also let the message time out. The Go message clears itself in state.StatusMessage,
// and its test covers the time. Here the slot is a fake that the test empties.
func TestMessageSitsLeftOfTheReadingsAndClearsAfterItsTTL(t *testing.T) {
	f, _, _, m, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", theme.WindowWidth)
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
	_, _, _, m, tt := barFrame(t, readingHost(stats(40, 50, 100)), "0.1.18", width)
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
