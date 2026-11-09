package app

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
	"github.com/egoist/mygo/ui"
)

// countSource is a source that counts the reads of its snapshot.
type countSource struct {
	*source
	reads atomic.Int32
}

func (s *countSource) Snapshot() agentstate.Snapshot { s.reads.Add(1); return s.source.Snapshot() }

type countRig struct {
	f      *Frame
	tt     *ui.Tester
	src    *countSource
	w      *fakeWindow
	notify func()
}

// attachCounting attaches a frame to a counting source with two projects, a and b.
func attachCounting(t *testing.T) *countRig {
	t.Helper()
	w := &fakeWindow{visible: true}
	f := NewFrame(Deps{})
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	src := &countSource{source: &source{Snap: agentstate.Snapshot{
		Orchestrators: []agents.Worker{
			{Name: "lead-a", Address: "oa", Project: "a", Role: agents.RoleOrchestrator, Live: true},
			{Name: "lead-b", Address: "ob", Project: "b", Role: agents.RoleOrchestrator, Live: true},
		},
		Workers: []agents.Worker{{Name: "b-1", Address: "wa", Project: "a", Status: "working", Live: true}},
	}}}
	r := &countRig{f: f, src: src, w: w}
	f.deps.StartAgents = func(n func()) (agentstate.AgentSource, func()) { r.notify = n; return src, func() {} }
	stop := f.Attach(w)
	t.Cleanup(func() { w.flush(); stop() })
	r.tt = ui.NewTester(f.View, theme.WindowWidth, theme.WindowHeight)
	return r
}

// A redraw with no change reads no snapshot; a change reads one (#565).
func TestARedrawWithNoChangeReadsNoSnapshot(t *testing.T) {
	enableViewers(t) // the board, the worker panel and the PR pane read the agents too
	r := attachCounting(t)
	for _, sel := range []state.Selected{
		{Kind: state.KindOrchestrator, ID: "oa"},
		{Kind: state.KindWorker, ID: "wa"},
	} {
		r.f.Selection.Select(sel)
		r.w.flush()
		before := r.src.reads.Load()
		for range 3 {
			r.tt.Frame()
		}
		if got := r.src.reads.Load(); got != before {
			t.Errorf("%+v: three draws read the snapshot %d times, want none", sel, got-before)
		}
	}
	r.src.Snap.Workers = append(r.src.Snap.Workers, agents.Worker{Name: "b-2", Address: "wa2", Project: "a", Live: true})
	before := r.src.reads.Load()
	r.notify()
	r.w.flush()
	if got := r.src.reads.Load(); got != before+1 {
		t.Errorf("a change read the snapshot %d times, want 1", got-before)
	}
	if _, ok := r.f.agentAt("wa2"); !ok {
		t.Error("the new worker is not in the stored snapshot")
	}
}

// A selection change sets the open project and the tabs at once, before the next draw of the
// file tree or the title bar (#565).
func TestASelectionChangeSetsTheOpenProjectBeforeTheNextDraw(t *testing.T) {
	r := attachCounting(t)
	r.f.Selection.Select(state.Selected{Kind: state.KindOrchestrator, ID: "ob"})
	if r.f.openProject != "b" {
		t.Errorf("open project %q, want b", r.f.openProject)
	}
	var addrs []string
	for _, tab := range r.f.titleTabs() {
		addrs = append(addrs, tab.Address)
	}
	if !slices.Equal(addrs, []string{"ob"}) {
		t.Errorf("tabs %v, want lead-b", addrs)
	}
	r.f.Selection.Select(state.Selected{Kind: state.KindWorker, ID: "wa"})
	if r.f.openProject != "a" || r.f.tabAgent != "wa" {
		t.Errorf("open project %q, tab agent %q; want a and wa", r.f.openProject, r.f.tabAgent)
	}
}

// The index gives the first agent of an address, as a scan of the three lists did, and nothing
// for an empty address or without agents.
func TestAgentAtReadsTheIndex(t *testing.T) {
	f := &Frame{}
	f.agentSnap = func() agentstate.Snapshot {
		return agentstate.Snapshot{
			Orchestrators: []agents.Worker{{Address: "x", Name: "orch"}, {Name: "no address"}},
			Workers:       []agents.Worker{{Address: "x", Name: "worker"}, {Address: "w", Name: "w"}},
		}
	}
	f.takeSnapshot()
	if w, ok := f.agentAt("x"); !ok || w.Name != "orch" {
		t.Errorf("x gave %q %v, want the orchestrator", w.Name, ok)
	}
	if w, ok := f.agentAt("w"); !ok || w.Name != "w" {
		t.Errorf("w gave %q %v", w.Name, ok)
	}
	if _, ok := f.agentAt(""); ok {
		t.Error("an empty address gave an agent")
	}
	f.agentSnap = nil
	f.takeSnapshot()
	if _, ok := f.agentAt("x"); ok || f.titleTabs() != nil {
		t.Error("without agents the index or the tabs stay")
	}
}
