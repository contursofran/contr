package ghsource

import (
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
)

type titleRig struct {
	*apptest.GHRig
	t      *IssueTitles
	runner *apptest.TitleRunner
}

func newTitleRig() *titleRig {
	r := &titleRig{GHRig: &apptest.GHRig{Now: time.Unix(5000, 0)},
		runner: &apptest.TitleRunner{Out: map[string]string{"241": `{"title":"Agent rows on ui.List"}`}}}
	r.t = &IssueTitles{Env: r.Env(), Client: apptest.ClientOf(r.runner), Now: r.Clock}
	return r
}

func TestIssueTitleIsReadOffThreadAndShown(t *testing.T) {
	r := newTitleRig()
	r.t.Sync(apptest.IssueWorker("/wt/a", "#241"))
	if r.t.Title("/wt/a", "#241") != "" || len(r.Queue) == 0 {
		t.Fatal("the read ran on the calling thread, or nothing was queued")
	}
	r.Flush()
	if got := r.t.Title("/wt/a", "241"); got != "Agent rows on ui.List" {
		t.Errorf("title %q", got)
	}
	if r.Redraw == 0 {
		t.Error("no redraw after the answer")
	}
	if want := []string{"issue", "view", "241", "--json", "title"}; len(r.runner.Args) != 5 || r.runner.Args[2] != want[2] || r.runner.Args[4] != "title" {
		t.Errorf("gh args %v", r.runner.Args)
	}
}

func TestIssueTitleIsCachedAndAskedAgainAfterTheTTL(t *testing.T) {
	r := newTitleRig()
	ws := apptest.IssueWorker("/wt/a", "241")
	for range 3 {
		r.t.Sync(ws)
		r.Flush()
	}
	if r.runner.Calls != 1 {
		t.Fatalf("gh ran %d times inside the TTL, want 1", r.runner.Calls)
	}
	r.runner.Out["241"] = `{"title":"New title"}`
	r.Now = r.Now.Add(TitleHitTTL + time.Second)
	r.t.Sync(ws)
	r.Flush()
	if r.runner.Calls != 2 || r.t.Title("/wt/a", "241") != "New title" {
		t.Errorf("calls %d, title %q", r.runner.Calls, r.t.Title("/wt/a", "241"))
	}
}

func TestAnIssueReadRunsOnceWhileInFlight(t *testing.T) {
	r := newTitleRig()
	ws := apptest.IssueWorker("/wt/a", "241")
	r.t.Sync(ws)
	r.t.Sync(ws)
	if len(r.Queue) != 1 {
		t.Errorf("queued %d reads, want 1", len(r.Queue))
	}
}

func TestAGhErrorKeepsTheKnownTitleAndRetriesLater(t *testing.T) {
	r := newTitleRig()
	ws := apptest.IssueWorker("/wt/a", "241")
	r.t.Sync(ws)
	r.Flush()
	r.runner.Err = errors.New("no network")
	r.Now = r.Now.Add(TitleHitTTL + time.Second)
	r.t.Sync(ws)
	r.Flush()
	if got := r.t.Title("/wt/a", "241"); got != "Agent rows on ui.List" {
		t.Errorf("title %q after a failed read, want the known one", got)
	}
	// A failed read is not asked again at once.
	r.t.Sync(ws)
	r.Flush()
	if r.runner.Calls != 2 {
		t.Errorf("calls %d, want 2", r.runner.Calls)
	}
	r.Now = r.Now.Add(titleMissTTL + time.Second)
	r.runner.Err = nil
	r.t.Sync(ws)
	r.Flush()
	if r.runner.Calls != 3 {
		t.Errorf("calls %d after the retry time, want 3", r.runner.Calls)
	}
}

func TestAnIssueWithNoTitleYetHasNoneAfterAnError(t *testing.T) {
	r := newTitleRig()
	r.runner.Err = errors.New("gh: not found")
	r.t.Sync(apptest.IssueWorker("/wt/a", "241"))
	r.Flush()
	if got := r.t.Title("/wt/a", "241"); got != "" {
		t.Errorf("title %q", got)
	}
}

func TestIssueTitlesSkipWorkersWithNothingToRead(t *testing.T) {
	r := newTitleRig()
	ended := apptest.IssueWorker("/wt/a", "241")
	ended[0].Live = false
	r.t.Sync(ended)
	r.t.Sync(apptest.IssueWorker("", "241"))
	r.t.Sync(apptest.IssueWorker("/wt/a", ""))
	r.t.Sync(apptest.IssueWorker("/wt/a", "abc"))
	orch := apptest.IssueWorker("/wt/a", "241")
	orch[0].Role = agents.RoleOrchestrator
	r.t.Sync(orch)
	if len(r.Queue) != 0 {
		t.Errorf("queued %d reads, want none", len(r.Queue))
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
		r.runner.Out[n] = `{"title":"t` + n + `"}`
		ws = append(ws, apptest.IssueWorker("/wt/"+n, n)...)
	}
	r.t.Sync(ws)
	if len(r.Queue) != maxGH {
		t.Fatalf("%d reads started, want %d", len(r.Queue), maxGH)
	}
	r.Flush()
	for _, n := range []string{"1", "2", "3", "4"} {
		if r.t.Title("/wt/"+n, n) == "" {
			t.Errorf("no title for %s", n)
		}
	}
}

func TestSyncDropsTheAnswersOfGoneWorkers(t *testing.T) {
	r := newTitleRig()
	r.t.Sync(apptest.IssueWorker("/wt/a", "241"))
	r.Flush()
	if r.t.Title("/wt/a", "241") == "" {
		t.Fatal("no title read")
	}
	r.t.Sync(nil)
	if len(r.t.answers) != 0 {
		t.Errorf("answers of a gone worker stay: %v", r.t.answers)
	}
}
