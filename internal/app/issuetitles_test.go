package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/github"
)

// titleRunner answers `issue view N --json title` and counts the calls.
type titleRunner struct {
	out   map[string]string // args joined by "|" are not needed: issue number -> reply
	err   error
	calls int
	args  []string
}

func (r *titleRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls++
	r.args = args
	if r.err != nil {
		return nil, r.err
	}
	return []byte(r.out[args[2]]), nil
}

type titleRig struct {
	t      *IssueTitles
	runner *titleRunner
	queue  []func()
	now    time.Time
	redraw int
}

func newTitleRig() *titleRig {
	r := &titleRig{runner: &titleRunner{out: map[string]string{"241": `{"title":"Agent rows on ui.List"}`}}, now: time.Unix(5000, 0)}
	r.t = &IssueTitles{
		Run:        Runner{Go: func(f func()) { r.queue = append(r.queue, f) }},
		Client:     func(dir string) github.Client { return github.Client{R: r.runner, Dir: dir} },
		Invalidate: func() { r.redraw++ },
		Now:        func() time.Time { return r.now },
	}
	return r
}

func (r *titleRig) flush() {
	for len(r.queue) > 0 {
		f := r.queue[0]
		r.queue = r.queue[1:]
		f()
	}
}

func issueWorker(dir, issue string) []agents.Worker {
	return []agents.Worker{{Name: "b", Role: "builder", Worktree: dir, Issue: issue, Live: true}}
}

func TestIssueTitleIsReadOffThreadAndShown(t *testing.T) {
	r := newTitleRig()
	r.t.Sync(issueWorker("/wt/a", "#241"))
	if r.t.Title("/wt/a", "#241") != "" || len(r.queue) == 0 {
		t.Fatal("the read ran on the calling thread, or nothing was queued")
	}
	r.flush()
	if got := r.t.Title("/wt/a", "241"); got != "Agent rows on ui.List" {
		t.Errorf("title %q", got)
	}
	if r.redraw == 0 {
		t.Error("no redraw after the answer")
	}
	if want := []string{"issue", "view", "241", "--json", "title"}; len(r.runner.args) != 5 || r.runner.args[2] != want[2] || r.runner.args[4] != "title" {
		t.Errorf("gh args %v", r.runner.args)
	}
}

func TestIssueTitleIsCachedAndAskedAgainAfterTheTTL(t *testing.T) {
	r := newTitleRig()
	ws := issueWorker("/wt/a", "241")
	for range 3 {
		r.t.Sync(ws)
		r.flush()
	}
	if r.runner.calls != 1 {
		t.Fatalf("gh ran %d times inside the TTL, want 1", r.runner.calls)
	}
	r.runner.out["241"] = `{"title":"New title"}`
	r.now = r.now.Add(titleHitTTL + time.Second)
	r.t.Sync(ws)
	r.flush()
	if r.runner.calls != 2 || r.t.Title("/wt/a", "241") != "New title" {
		t.Errorf("calls %d, title %q", r.runner.calls, r.t.Title("/wt/a", "241"))
	}
}

func TestAnIssueReadRunsOnceWhileInFlight(t *testing.T) {
	r := newTitleRig()
	ws := issueWorker("/wt/a", "241")
	r.t.Sync(ws)
	r.t.Sync(ws)
	if len(r.queue) != 1 {
		t.Errorf("queued %d reads, want 1", len(r.queue))
	}
}

func TestAGhErrorKeepsTheKnownTitleAndRetriesLater(t *testing.T) {
	r := newTitleRig()
	ws := issueWorker("/wt/a", "241")
	r.t.Sync(ws)
	r.flush()
	r.runner.err = errors.New("no network")
	r.now = r.now.Add(titleHitTTL + time.Second)
	r.t.Sync(ws)
	r.flush()
	if got := r.t.Title("/wt/a", "241"); got != "Agent rows on ui.List" {
		t.Errorf("title %q after a failed read, want the known one", got)
	}
	// A failed read is not asked again at once.
	r.t.Sync(ws)
	r.flush()
	if r.runner.calls != 2 {
		t.Errorf("calls %d, want 2", r.runner.calls)
	}
	r.now = r.now.Add(titleMissTTL + time.Second)
	r.runner.err = nil
	r.t.Sync(ws)
	r.flush()
	if r.runner.calls != 3 {
		t.Errorf("calls %d after the retry time, want 3", r.runner.calls)
	}
}

func TestAnIssueWithNoTitleYetHasNoneAfterAnError(t *testing.T) {
	r := newTitleRig()
	r.runner.err = errors.New("gh: not found")
	r.t.Sync(issueWorker("/wt/a", "241"))
	r.flush()
	if got := r.t.Title("/wt/a", "241"); got != "" {
		t.Errorf("title %q", got)
	}
}

func TestIssueTitlesSkipWorkersWithNothingToRead(t *testing.T) {
	r := newTitleRig()
	ended := issueWorker("/wt/a", "241")
	ended[0].Live = false
	r.t.Sync(ended)
	r.t.Sync(issueWorker("", "241"))
	r.t.Sync(issueWorker("/wt/a", ""))
	r.t.Sync(issueWorker("/wt/a", "abc"))
	orch := issueWorker("/wt/a", "241")
	orch[0].Role = agents.RoleOrchestrator
	r.t.Sync(orch)
	if len(r.queue) != 0 {
		t.Errorf("queued %d reads, want none", len(r.queue))
	}
	var nilTitles *IssueTitles
	if nilTitles.Title("/wt/a", "241") != "" {
		t.Error("a nil cache gave a title")
	}
}

func TestAtMostMaxGHTitleReadsRunAtOnce(t *testing.T) {
	r := newTitleRig()
	var ws []agents.Worker
	for _, n := range []string{"1", "2", "3", "4"} {
		r.runner.out[n] = `{"title":"t` + n + `"}`
		ws = append(ws, issueWorker("/wt/"+n, n)...)
	}
	r.t.Sync(ws)
	if len(r.queue) != maxGH {
		t.Fatalf("%d reads started, want %d", len(r.queue), maxGH)
	}
	r.flush()
	for _, n := range []string{"1", "2", "3", "4"} {
		if r.t.Title("/wt/"+n, n) == "" {
			t.Errorf("no title for %s", n)
		}
	}
}

// The row shows the issue title on line 1, so the worker name moves to line 2. A row with no
// title and no task shows the name once, on line 1.
func TestAWorkerRowShowsTheIssueTitleAndTheNameMovesToLineTwo(t *testing.T) {
	h := newRows(t)
	r := newTitleRig()
	h.list.Titles = r.t
	snap := h.live.get()
	h.send(snap)
	if slices.Contains(h.tt.Texts(), "b-241") {
		t.Fatalf("the name is on line 2 before any title: %q", h.tt.Texts())
	}
	r.t.Sync(snap.Workers)
	r.flush()
	h.send(snap)
	if !slices.Contains(h.tt.Texts(), "b-241") {
		t.Errorf("the name is not on line 2 once the issue title is on line 1: %q", h.tt.Texts())
	}
	l := &SpacesList{Titles: r.t}
	a := agentLine{w: snap.Workers[0]}
	if got := titleOf(a, l.issueTitle(a)); got != "Agent rows on ui.List" {
		t.Errorf("line 1 %q", got)
	}
	// The issue title wins over the task; a failed read later keeps it.
	a.w.Task = "terminal title"
	r.runner.err = errors.New("offline")
	r.now = r.now.Add(titleHitTTL + time.Second)
	r.t.Sync(snap.Workers)
	r.flush()
	if got := titleOf(a, l.issueTitle(a)); got != "Agent rows on ui.List" {
		t.Errorf("line 1 %q after an error", got)
	}
	// With no issue title, the task is line 1.
	if got := titleOf(a, ""); got != "terminal title" {
		t.Errorf("fallback %q", got)
	}
}

func TestAnOrchestratorRowNeverShowsAnIssueTitle(t *testing.T) {
	a := agentLine{w: agents.Worker{Name: "lead", Issue: "241", Worktree: "/wt/a"}, orchestrator: true}
	l := &SpacesList{}
	if got := l.issueTitle(a); got != "" {
		t.Errorf("title %q", got)
	}
	if got := titleOf(a, "Some title"); got != "lead" {
		t.Errorf("line 1 %q", got)
	}
}

func TestTheModelComesFromTheStatusLineWhenArgvHasNone(t *testing.T) {
	h := newRows(t)
	h.list.ModelOf = func(string) string { return "" }
	h.list.StatusModel = func(addr string) string {
		if addr == "lead" {
			return "opus"
		}
		return ""
	}
	snap := h.live.get()
	snap.Orchestrators[0].Model = ""
	h.send(snap)
	if !h.tt.HasText("opus") {
		t.Errorf("no model from the status line in %q", h.tt.Texts())
	}
	// argv wins over the status line.
	snap.Orchestrators[0].Model = "haiku"
	h.send(snap)
	if !h.tt.HasText("haiku") {
		t.Errorf("no model from argv in %q", h.tt.Texts())
	}
}

func TestSyncDropsTheAnswersOfGoneWorkers(t *testing.T) {
	r := newTitleRig()
	r.t.Sync(issueWorker("/wt/a", "241"))
	r.flush()
	if r.t.Title("/wt/a", "241") == "" {
		t.Fatal("no title read")
	}
	r.t.Sync(nil)
	if len(r.t.answers) != 0 {
		t.Errorf("answers of a gone worker stay: %v", r.t.answers)
	}
}

func TestLineTwoShowsTheEffortAfterTheModel(t *testing.T) {
	h := newRows(t)
	h.list.ModelOf = func(string) string { return "" }
	h.list.EffortOf = func(string) string { return "" }
	h.list.StatusEffort = func(addr string) string {
		if addr == "lead" {
			return "medium"
		}
		return ""
	}
	snap := h.live.get()
	snap.Orchestrators[0].Model = "opus"
	h.send(snap)
	if !h.tt.HasText("opus medium") {
		t.Errorf("no effort from the status line in %q", h.tt.Texts())
	}
	// The hook's label (Worker.Effort) wins over the status line.
	snap.Orchestrators[0].Effort = "high"
	h.send(snap)
	if !h.tt.HasText("opus high") {
		t.Errorf("no effort from the label in %q", h.tt.Texts())
	}
	// The profile's effort wins over both.
	h.list.EffortOf = func(string) string { return "low" }
	h.send(snap)
	if !h.tt.HasText("opus low") {
		t.Errorf("no effort from the profile in %q", h.tt.Texts())
	}
}
