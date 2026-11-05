package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
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

func TestUsageMetersShowTheContextOfTheSelectedTerminal(t *testing.T) {
	src := fakeCtx{fakeLeft{}, map[string]usage.Context{"t1": {Tokens: 90000, Size: 200000}, "t2": {Tokens: 200000, Size: 200000}}}
	got := usageMeters(src, "t1")[0]
	want := kit.Meter{Label: "Context", Value: "90k of 200k  45%", Percent: 45, Has: true}
	if got != want {
		t.Errorf("context %+v, want %+v", got, want)
	}
	if got := usageMeters(src, "t2")[0]; got.Percent != 100 || got.Value != "200k of 200k  100%" {
		t.Errorf("full context %+v", got)
	}
	// No terminal selected, or a terminal with no file: no data.
	for _, id := range []string{"", "t3"} {
		if got := usageMeters(src, id)[0]; got.Has || got.Value != "" {
			t.Errorf("terminal %q: context %+v has data", id, got)
		}
	}
}

func TestUsageMetersShowThePercentUsedOfEachPlan(t *testing.T) {
	got := usageMeters(fakeLeft{usage.PlanClaude: 62, usage.PlanCodex: 0}, "")
	want := []kit.Meter{
		{Label: "Context"},
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

func TestUsageMetersWithoutDataHaveNone(t *testing.T) {
	for name, src := range map[string]planPercent{"nil": nil, "empty": fakeLeft{}} {
		for _, m := range usageMeters(src, "t1") {
			if m.Has || m.Value != "" || m.Percent != 0 {
				t.Errorf("%s source: meter %+v has data", name, m)
			}
		}
	}
	// One plan with data leaves the other plan and the context empty.
	got := usageMeters(fakeLeft{usage.PlanClaude: 75}, "")
	if !got[1].Has || got[2].Has || got[0].Has {
		t.Errorf("meters %+v, want only Claude with data", got)
	}
}

func TestTheFilesPanelShowsTheMetersUnderTheTree(t *testing.T) {
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
	tree, meters := box(t, tt, LabelFileTree), box(t, tt, "usage")
	if meters.Y < tree.Y+tree.H-1 {
		t.Errorf("meters %+v are not under the tree %+v", meters, tree)
	}
	for _, want := range []string{"Context", "Claude", "Codex", "42%", kit.NoValue} {
		if !tt.HasText(want) {
			t.Errorf("no text %q; texts %q", want, tt.Texts())
		}
	}
	// The context and Codex have no data.
	if n := countText(tt.Texts(), kit.NoValue); n != 2 {
		t.Errorf("%d dashes, want 2 (context and codex)", n)
	}
}

func TestTheFilesPanelShowsDashesWithoutASampler(t *testing.T) {
	f, tt := newTester(t)
	f.Usage = nil
	tt.Frame()
	if n := countText(tt.Texts(), kit.NoValue); n != 3 {
		t.Errorf("%d dashes, want 3; texts %q", n, tt.Texts())
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
	tt := ui.NewTester(NewFrame().View, theme.WindowWidth, theme.WindowHeight)
	tt.Frame()
	for _, label := range []string{"usage-claude", "usage-codex", "usage-claude-bar"} {
		if !absent(tt, label) {
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
	if tt.HasText("90k of 200k  45%") {
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
	if !tt.HasText("90k of 200k  45%") {
		t.Errorf("no context text; texts %q", tt.Texts())
	}
	f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "w2"})
	tt.Frame()
	if tt.HasText("90k of 200k  45%") {
		t.Error("the context of w1 shows for w2")
	}
}
