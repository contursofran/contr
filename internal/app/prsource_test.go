package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/github"
)

// branchRunner answers `pr view --json number` per folder.
type branchRunner struct {
	out   map[string]string // dir -> reply; missing dir: no pull request
	err   error
	calls int
}

func (r *branchRunner) Run(_ context.Context, dir string, _ ...string) ([]byte, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	if s, ok := r.out[dir]; ok {
		return []byte(s), nil
	}
	return nil, errors.New(`no pull requests found for branch "x"`)
}

type prSourceRig struct {
	w      *WorkerPRs
	runner *branchRunner
	queue  []func()
	branch string
	now    time.Time
	redraw int
}

func newPRSourceRig(out map[string]string) *prSourceRig {
	r := &prSourceRig{runner: &branchRunner{out: out}, branch: "agent/1-x", now: time.Unix(1000, 0)}
	r.w = &WorkerPRs{
		Run:        Runner{Go: func(f func()) { r.queue = append(r.queue, f) }},
		Client:     func(dir string) github.Client { return github.Client{R: r.runner, Dir: dir} },
		Branch:     func(string) string { return r.branch },
		Invalidate: func() { r.redraw++ },
		Now:        func() time.Time { return r.now },
	}
	return r
}

// flush runs queued work until none is left.
func (r *prSourceRig) flush() {
	for len(r.queue) > 0 {
		f := r.queue[0]
		r.queue = r.queue[1:]
		f()
	}
}

func liveWorker(dir string) []agents.Worker {
	return []agents.Worker{{Name: "b", Role: "builder", Worktree: dir, Live: true}}
}

func TestWorkerPRFromBranch(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	if r.w.PR("/wt/a") != 0 {
		t.Fatal("answer before any read")
	}
	r.w.Sync(liveWorker("/wt/a"))
	r.flush()
	if got := r.w.PR("/wt/a"); got != 307 {
		t.Errorf("PR = %d, want 307", got)
	}
	if r.redraw == 0 {
		t.Error("no redraw after the answer")
	}
}

func TestWorkerNoPRFallsBackToIssue(t *testing.T) {
	r := newPRSourceRig(nil)
	r.w.Sync(liveWorker("/wt/a"))
	r.flush()
	if got := r.w.PR("/wt/a"); got != 0 {
		t.Errorf("PR = %d, want 0", got)
	}
}

func TestWorkerPRCachesPerWorktreeAndBranch(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.flush()
	r.w.Sync(ws)
	r.w.Sync(ws)
	r.flush()
	if r.runner.calls != 1 {
		t.Errorf("gh ran %d times for one worktree and branch, want 1", r.runner.calls)
	}
	r.branch = "agent/2-y" // a new branch asks again, once the answer is old
	r.now = r.now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.flush()
	if r.runner.calls != 2 {
		t.Errorf("gh ran %d times after a branch change, want 2", r.runner.calls)
	}
}

func TestWorkerPRMissIsAskedAgainAfterTheTTL(t *testing.T) {
	r := newPRSourceRig(nil)
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.flush()
	r.w.Sync(ws)
	r.flush()
	if r.runner.calls != 1 {
		t.Fatalf("asked again within the TTL: %d calls", r.runner.calls)
	}
	r.runner.out = map[string]string{"/wt/a": `{"number":9}`}
	r.now = r.now.Add(prMissTTL + time.Second)
	r.w.Sync(ws)
	r.flush()
	if r.w.PR("/wt/a") != 9 {
		t.Errorf("PR = %d after the PR opened, want 9", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRSkipsRunningReadsEndedAndFolderless(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":1}`})
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.w.Sync(ws) // a read is in flight: no second one
	if len(r.queue) != 1 {
		t.Errorf("queued %d reads, want 1", len(r.queue))
	}
	r.flush()
	r2 := newPRSourceRig(nil)
	r2.w.Sync([]agents.Worker{{Worktree: "/wt/b"}, {Live: true}})
	if len(r2.queue) != 0 {
		t.Error("read for an ended worker or one without a folder")
	}
}

func TestWorkerPRErrorsAndUnknownBranchGiveNone(t *testing.T) {
	r := newPRSourceRig(nil)
	r.runner.err = errors.New("gh: boom")
	r.w.Sync(liveWorker("/wt/a"))
	r.flush()
	if r.w.PR("/wt/a") != 0 {
		t.Error("an error gave a PR")
	}
	r.branch = ""
	r.w.Sync(liveWorker("/wt/c"))
	r.flush()
	if r.w.PR("/wt/c") != 0 || r.runner.calls != 1 {
		t.Errorf("no branch: PR %d, calls %d", r.w.PR("/wt/c"), r.runner.calls)
	}
	var nilW *WorkerPRs
	if nilW.PR("/x") != 0 {
		t.Error("nil source gave a PR")
	}
}

func TestBoardWorkOpensRealPR(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "b-9", Address: "t1", Role: "builder", Issue: "151", Worktree: "/wt/a", Project: "a", Live: true, Status: "working"},
		{Name: "b-8", Address: "t2", Role: "builder", Project: "a", Live: true},
	}}
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	r.w.Sync(snap.Workers)
	r.flush()
	p, _ := newBoardPanel(snap, "a")
	p.PRs = r.w
	var dir string
	var n int
	var issueOpened bool
	picked := ""
	p.OpenPR = func(d string, num int) { dir, n = d, num }
	p.OpenIssue = func(string, int) { issueOpened = true }
	p.Select = func(a string) { picked = a }
	tt := boardTester(p)
	tt.Frame()
	if err := tt.Click(LabelWorkPR("#307")); err != nil {
		t.Fatal(err)
	}
	if dir != "/wt/a" || n != 307 || picked != "" || issueOpened {
		t.Errorf("OpenPR(%q, %d), picked %q, issue %v", dir, n, picked, issueOpened)
	}
	if _, ok := tt.Find(LabelWorkPR("b-8")); ok {
		t.Error("a worker without an issue has a PR button")
	}
}

func TestBoardWorkWithoutPROpensTheIssue(t *testing.T) {
	snap := agentstate.Snapshot{Workers: []agents.Worker{
		{Name: "b-9", Address: "t1", Role: "builder", Issue: "151", Worktree: "/wt/a", Project: "a", Live: true, Status: "working"},
	}}
	r := newPRSourceRig(nil)
	r.w.Sync(snap.Workers)
	r.flush()
	p, _ := newBoardPanel(snap, "a")
	p.PRs = r.w
	var dir string
	var n int
	prOpened := false
	p.OpenPR = func(string, int) { prOpened = true }
	p.OpenIssue = func(d string, num int) { dir, n = d, num }
	tt := boardTester(p)
	tt.Frame()
	if err := tt.Click(LabelWorkIssue("#151")); err != nil {
		t.Fatal(err)
	}
	if dir != "/wt/a" || n != 151 || prOpened {
		t.Errorf("OpenIssue(%q, %d), pr opened %v", dir, n, prOpened)
	}
}

func TestFrameOpenIssueRunsOffThreadAndShowsAnError(t *testing.T) {
	f := NewFrame()
	var gotDir string
	var gotN int
	var queued []func()
	f.WorkerPRs = &WorkerPRs{Run: Runner{Go: func(fn func()) { queued = append(queued, fn) }}}
	f.OpenIssueWeb = func(d string, n int) error { gotDir, gotN = d, n; return errors.New("gh: no browser") }
	f.OpenIssue("/wt/a", 151)
	if gotN != 0 || len(queued) != 1 {
		t.Fatal("the issue opened on the calling thread")
	}
	queued[0]()
	if gotDir != "/wt/a" || gotN != 151 {
		t.Errorf("opened %q #%d", gotDir, gotN)
	}
}

func TestAgentRowOpenPRGoesToTheBrowserNotThePane(t *testing.T) {
	r := newPRRig(t)
	f := NewFrame()
	f.PR = r.pane
	var gotDir string
	var gotN int
	var queued []func()
	f.WorkerPRs = &WorkerPRs{Run: Runner{Go: func(fn func()) { queued = append(queued, fn) }}}
	f.Toasts = state.NewToasts(func() {})
	f.OpenPRWeb = func(d string, n int) error { gotDir, gotN = d, n; return errors.New("gh: no browser") }
	e := f.agentRowEntry()
	if e.OpenPR != nil {
		t.Error("the agent rows can reach the PR pane")
	}
	if e.OpenPRWeb == nil {
		t.Fatal("no browser open")
	}
	e.OpenPRWeb("/wt/a", 307)
	if gotN != 0 || len(queued) != 1 {
		t.Fatal("the pull request opened on the calling thread")
	}
	queued[0]()
	if gotDir != "/wt/a" || gotN != 307 {
		t.Errorf("opened %q #%d", gotDir, gotN)
	}
	if f.PROpen() {
		t.Error("the PR pane opened")
	}
	if msg, ok := lastToast(f); !ok || msg != "gh: no browser" {
		t.Errorf("status %q, %v", msg, ok)
	}
}

func TestWorkerPRFreshAnswerDoesNoGitRead(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	reads := 0
	r.w.Branch = func(string) string { reads++; return r.branch }
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.flush()
	r.w.Sync(ws)
	r.w.Sync(ws)
	if len(r.queue) != 0 || reads != 1 {
		t.Errorf("fresh answer: %d queued, %d git reads, want 0 and 1", len(r.queue), reads)
	}
}

func TestWorkerPRClosedPRLeavesTheRow(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307,"state":"OPEN"}`})
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.flush()
	if r.w.PR("/wt/a") != 307 {
		t.Fatal("open PR not found")
	}
	r.runner.out["/wt/a"] = `{"number":307,"state":"MERGED"}`
	r.w.Sync(ws) // still fresh: no read
	r.flush()
	if r.w.PR("/wt/a") != 307 || r.runner.calls != 1 {
		t.Fatalf("read before the TTL: PR %d, calls %d", r.w.PR("/wt/a"), r.runner.calls)
	}
	r.now = r.now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.flush()
	if r.w.PR("/wt/a") != 0 {
		t.Errorf("merged PR stays: %d", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRErrorOnRecheckKeepsTheNumber(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307,"state":"OPEN"}`})
	ws := liveWorker("/wt/a")
	r.w.Sync(ws)
	r.flush()
	r.runner.err = errors.New("gh: offline")
	r.now = r.now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.flush()
	if r.w.PR("/wt/a") != 307 {
		t.Errorf("PR = %d after a failed re-check, want 307", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRBoundsGHRuns(t *testing.T) {
	r := newPRSourceRig(nil)
	var ws []agents.Worker
	for _, d := range []string{"/wt/a", "/wt/b", "/wt/c", "/wt/d", "/wt/e"} {
		ws = append(ws, liveWorker(d)...)
	}
	r.w.Sync(ws)
	// run the branch reads and their answers: each queues a gh run
	for i := 0; i < 5; i++ {
		f := r.queue[0]
		r.queue = r.queue[1:]
		f()
	}
	peak := 0
	for len(r.queue) > 0 {
		if r.w.active > peak {
			peak = r.w.active
		}
		f := r.queue[0]
		r.queue = r.queue[1:]
		f()
	}
	if peak > 2 || r.runner.calls != 5 {
		t.Errorf("peak %d gh runs (max %d), %d calls, want 5", peak, 2, r.runner.calls)
	}
	if r.w.active != 0 || len(r.w.waiting) != 0 {
		t.Errorf("active %d, waiting %d at the end", r.w.active, len(r.w.waiting))
	}
}
