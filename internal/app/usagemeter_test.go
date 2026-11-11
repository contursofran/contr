package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/files"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/usage"
)

// fakeLeft gives the percent left per plan. A plan that is not in the map has no data.
type fakeLeft map[usage.PlanID]float64

func (f fakeLeft) Left(plan usage.PlanID, _ uint64) (float64, bool) {
	v, ok := f[plan]
	return v, ok
}

func (f fakeLeft) Context(string) (usage.Context, bool) { return usage.Context{}, false }

// fakeCtx adds the context of some terminals to fakeLeft.
type fakeCtx struct {
	fakeLeft
	ctx map[string]usage.Context
}

func (f fakeCtx) Context(id string) (usage.Context, bool) { c, ok := f.ctx[id]; return c, ok }

func TestContextMeterShowsTheContextOfTheSelectedTerminal(t *testing.T) {
	src := fakeCtx{fakeLeft{}, map[string]usage.Context{"t1": {Tokens: 90000, Size: 200000}, "t2": {Tokens: 200000, Size: 200000}}}
	got := contextMeter(src, "t1", usage.Context{})
	want := kit.Meter{Label: "Context", Detail: "90k of 200k", Value: "45%", Percent: 45, Has: true}
	if got != want {
		t.Errorf("context %+v, want %+v", got, want)
	}
	if got := contextMeter(src, "t2", usage.Context{}); got.Percent != 100 || got.Detail != "200k of 200k · compacts soon" || got.Value != "100%" || !got.Soon {
		t.Errorf("full context %+v", got)
	}
	// No terminal selected, a terminal with no file and no labels, or no sampler: no data.
	for _, id := range []string{"", "t3"} {
		if got := contextMeter(src, id, usage.Context{}); got.Has || got.Value != "" {
			t.Errorf("terminal %q: context %+v has data", id, got)
		}
	}
	if got := contextMeter(nil, "t1", usage.Context{}); got.Has {
		t.Errorf("nil source: context %+v has data", got)
	}
}

// Without a status line file the meter falls back to the Stop hook's labels,
// in the same format; the file wins when there is one (D205).
func TestContextMeterFallsBackToTheHookLabels(t *testing.T) {
	src := fakeCtx{fakeLeft{}, map[string]usage.Context{"t1": {Tokens: 90000, Size: 200000}}}
	labels := usage.Context{Tokens: 90000, Size: 160000}
	if got := contextMeter(src, "t1", labels); got.Detail != "90k of 200k" || got.Value != "45%" {
		t.Errorf("with a file: %+v, want the file", got)
	}
	want := kit.Meter{Label: "Context", Detail: "90k of 160k", Value: "56%", Percent: 56, Has: true}
	if got := contextMeter(src, "t2", labels); got != want {
		t.Errorf("no file: %+v, want %+v", got, want)
	}
	if got := contextMeter(nil, "t2", labels); got != want {
		t.Errorf("no sampler: %+v, want %+v", got, want)
	}
	if got := contextMeter(src, "", labels); got.Has {
		t.Errorf("no selection: %+v has data", got)
	}
}

func TestPlanMetersShowThePercentUsedOfEachPlan(t *testing.T) {
	got := planMeters(fakeLeft{usage.PlanClaude: 62, usage.PlanCodex: 0})
	want := []kit.Meter{
		{Label: "Claude", Value: "38%", Percent: 38, Has: true},
		{Label: "Codex", Value: "100%", Percent: 100, Has: true},
	}
	if len(got) != len(want) {
		t.Fatalf("meters %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("meter %d %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPlanMetersWithoutDataHaveNone(t *testing.T) {
	for name, src := range map[string]planPercent{"nil": nil, "empty": fakeLeft{}} {
		for _, m := range planMeters(src) {
			if m.Has || m.Value != "" || m.Percent != 0 {
				t.Errorf("%s source: meter %+v has data", name, m)
			}
		}
	}
	got := planMeters(fakeLeft{usage.PlanClaude: 75})
	if !got[0].Has || got[1].Has {
		t.Errorf("meters %+v, want only Claude with data", got)
	}
}

// The files panel keeps only the Context meter; the plans are in the sidebar foot (D196).
func TestTheFilesPanelShowsOnlyTheContextUnderTheTree(t *testing.T) {
	f, tt := newTester(t)
	f.Usage = usage.NewSampler(func() usage.Reading {
		r := usage.Unread()
		r[0].Usage = &usage.PlanUsage{Plan: usage.PlanClaude, Windows: []usage.Window{{Kind: "five_hour", UsedPercent: 42}}}
		return r
	}, nil)
	defer f.Usage.Close()
	f.Usage.SetVisible(true)
	waitUntil(t, func() bool { _, ok := f.Usage.Left(usage.PlanClaude, usage.UnixNow()); return ok })
	tt.Frame()
	tree, meters := apptest.Box(t, tt, files.LabelFileTree), apptest.Box(t, tt, "usage")
	if meters.Y < tree.Y+tree.H-1 {
		t.Errorf("meters %+v are not under the tree %+v", meters, tree)
	}
	if !tt.HasText("Context") {
		t.Errorf("no Context; texts %q", tt.Texts())
	}
	for _, gone := range []string{"Claude meter", "Codex meter", "Claude line", "Codex line"} {
		if !apptest.Absent(tt, gone) {
			t.Errorf("the files panel still has %q", gone)
		}
	}
	if tt.HasText("42%") {
		t.Error("the files panel still shows the Claude percent")
	}
}

func TestTheFilesPanelSaysNoDataYetWithoutASampler(t *testing.T) {
	f, tt := newTester(t)
	f.Usage = nil
	tt.Frame()
	if n := countText(tt.Texts(), kit.NoValue); n != 1 || kit.NoValue != "no data yet" {
		t.Errorf("%d %q, want 1 \"no data yet\" (D255); texts %q", n, kit.NoValue, tt.Texts())
	}
}

// The sidebar foot shows the plan meters inline, left of the Settings button (D196).
func TestTheSidebarFootShowsThePlansLeftOfSettings(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.list.Plans = func() []kit.Meter { return planMeters(fakeLeft{usage.PlanClaude: 62}) }
	h.tt.Frame()
	for _, want := range []string{"Claude", "Codex", "38%", kit.NoValue} {
		if !h.tt.HasText(want) {
			t.Errorf("no text %q; texts %q", want, h.tt.Texts())
		}
	}
	gear := apptest.Box(t, h.tt, sidebar.LabelSettingsOpen)
	claude, codex := apptest.Box(t, h.tt, "Claude meter"), apptest.Box(t, h.tt, kit.NoValue)
	if claude.W != kit.PlanUsageWidth {
		t.Errorf("bar width %v, want %v", claude.W, kit.PlanUsageWidth)
	}
	// A plan with no data has no bar, only "no data yet" (D255), and it fits left of the gear.
	if !apptest.Absent(h.tt, "Codex meter") {
		t.Error("the Codex plan has no data but shows a bar")
	}
	if !(claude.X < codex.X && codex.X+codex.W < gear.X) {
		t.Errorf("order: Claude %+v, Codex no data %+v, gear %+v", claude, codex, gear)
	}
	midGear, midBar := gear.Y+gear.H/2, claude.Y+claude.H/2
	if d := midGear - midBar; d > 2 || d < -2 {
		t.Errorf("meters at y %v, gear at y %v: not one row", midBar, midGear)
	}
	// The icon column has no room: the button only.
	h.list.Compact = true
	h.tt.Frame()
	if !apptest.Absent(h.tt, "Claude meter") {
		t.Error("the icon column shows the plan meters")
	}
}

// Building the view reads the sampler's last result only. The files are read on
// the sampler's goroutine, once the window is visible.
func TestBuildingTheMetersReadsNoFile(t *testing.T) {
	var reads atomic.Int32
	f, tt := newTester(t)
	f.Usage = usage.NewSampler(func() usage.Reading { reads.Add(1); return usage.Unread() }, nil)
	defer f.Usage.Close()
	for range 3 {
		tt.Frame()
	}
	if n := reads.Load(); n != 0 {
		t.Errorf("%d reads while building the view, want 0", n)
	}
	f.Usage.SetVisible(true)
	waitUntil(t, func() bool { return reads.Load() == 1 })
}

func TestTheWindowHasNoV02PlanBars(t *testing.T) {
	tt := ui.NewTester(NewFrame(Deps{}).View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	for _, label := range []string{"usage-claude", "usage-codex", "usage-claude-bar"} {
		if !apptest.Absent(tt, label) {
			t.Errorf("the window still has %q", label)
		}
	}
	for _, s := range tt.Texts() {
		if s == "claude" || s == "codex" {
			t.Errorf("the window still shows the plan %q", s)
		}
	}
}

func countText(texts []string, want string) int {
	n := 0
	for _, s := range texts {
		if s == want {
			n++
		}
	}
	return n
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}

// The Context line follows the selected terminal: a context file per terminal,
// read by the sampler (D144).
func TestTheContextLineFollowsTheSelectedTerminal(t *testing.T) {
	f, tt := newTester(t)
	f.Selection = state.NewSelection(nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	defer f.Usage.Close()
	if got := f.selectedTerminal(); got != "" {
		t.Errorf("terminal %q with no selection", got)
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorktree, ID: "/tmp/x"})
	if got := f.selectedTerminal(); got != "" {
		t.Errorf("terminal %q for a worktree row", got)
	}
	for kind, id := range map[state.SelectedKind]string{state.KindOrchestrator: "o1", state.KindWorker: "w1", state.KindTerminal: "p1"} {
		f.Selection.Select(state.Selected{Kind: kind, ID: id})
		if got := f.selectedTerminal(); got != id {
			t.Errorf("kind %d: terminal %q, want %q", kind, got, id)
		}
	}
	tt.Frame()
	if tt.HasText("90k of 200k") {
		t.Fatal("a context shows with no file")
	}
	dir := t.TempDir()
	t.Setenv("CONTR_STATE_DIR", dir)
	if _, err := usage.WriteContext(dir, "w1", &usage.Context{Schema: 1, Tokens: 90000, Size: 200000}); err != nil {
		t.Fatal(err)
	}
	f.Usage.Close()
	f.Usage = usage.NewFileSampler(nil)
	defer f.Usage.Close()
	f.Usage.SetVisible(true)
	waitUntil(t, func() bool { _, ok := f.Usage.Context("w1"); return ok })
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w1"})
	tt.Frame()
	if !tt.HasText("90k of 200k") {
		t.Errorf("no context text; texts %q", tt.Texts())
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w2"})
	tt.Frame()
	if tt.HasText("90k of 200k") {
		t.Error("the context of w1 shows for w2")
	}
}

// At the narrowest sidebar the meters clip; the Settings button stays inside.
func TestTheSidebarFootKeepsSettingsInAtTheNarrowestWidth(t *testing.T) {
	h := newSpaces(t, apptest.SpTree())
	h.list.Plans = func() []kit.Meter { return planMeters(fakeLeft{usage.PlanClaude: 62, usage.PlanCodex: 88}) }
	tt := h.tt
	tt.SetSize(theme.SidebarMinWidth, theme.WindowHeight)
	tt.Frame()
	gear, group := apptest.Box(t, tt, sidebar.LabelSettingsOpen), apptest.Box(t, tt, "plan usage")
	if gear.X+gear.W > theme.SidebarMinWidth {
		t.Errorf("Settings ends at %v, past the sidebar %v", gear.X+gear.W, theme.SidebarMinWidth)
	}
	if group.X+group.W > gear.X {
		t.Errorf("plan meters end at %v, over Settings at %v", group.X+group.W, gear.X)
	}
}

// The composer's meta is the model of line 2 and the context size, each left out when unknown
// (D252).
func TestChatMeta(t *testing.T) {
	f := &Frame{SpacesList: &sidebar.SpacesList{}, byAddr: map[string]agents.Worker{
		"t1": {Address: "t1", Model: "opus", Effort: "high", ContextTokens: 212000, ContextLimit: 1000000},
		"t2": {Address: "t2", Model: "sonnet"},
		"t3": {Address: "t3", ContextTokens: 9000, ContextLimit: 200000},
	}}
	for addr, want := range map[string]string{"t1": "opus high · 212k", "t2": "sonnet", "t3": "9k", "t4": ""} {
		if got := f.chatMeta(addr); got != want {
			t.Errorf("chatMeta(%s) = %q, want %q", addr, got, want)
		}
	}
}
