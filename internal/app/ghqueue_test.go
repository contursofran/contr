package app

import (
	"context"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/usage"
)

// fakeGH answers both gh reads: `pr view` with no pull request and `issue view` with a title.
type fakeGH struct{ prs, titles int }

func (r *fakeGH) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[0] == "issue" {
		r.titles++
		return []byte(`{"title":"t` + args[2] + `"}`), nil
	}
	r.prs++
	return []byte(`{"number":7,"state":"OPEN"}`), nil
}

// With 5 reads asked from the two types together, at most maxGH gh runs go at once, and all 5
// end (#568).
func TestOneGHQueueLimitsPRAndTitleReadsTogether(t *testing.T) {
	var queue []func()
	env := Env{Run: Runner{Go: func(f func()) { queue = append(queue, f) }}}
	gh, runner := &ghQueue{}, &fakeGH{}
	client := func(dir string) github.Client { return github.Client{R: runner, Dir: dir} }
	now := func() time.Time { return time.Unix(1000, 0) }
	prs := &WorkerPRs{Env: env, GH: gh, Client: client, Branch: func(string) string { return "agent/1-x" }, Now: now}
	titles := &IssueTitles{Env: env, GH: gh, Client: client, Now: now}

	var prWorkers, titleWorkers []agents.Worker
	for _, d := range []string{"/wt/a", "/wt/b", "/wt/c"} {
		prWorkers = append(prWorkers, liveWorker(d)...)
	}
	for _, n := range []string{"1", "2"} {
		titleWorkers = append(titleWorkers, issueWorker("/wt/"+n, n)...)
	}
	prs.Sync(prWorkers)
	titles.Sync(titleWorkers)
	// The queue holds the 3 branch reads, then the title gh runs. Run the branch reads: each
	// asks for a gh run. From then on each queued item is one gh run that has started.
	for i := 0; i < 3; i++ {
		f := queue[0]
		queue = queue[1:]
		f()
	}
	peak := 0
	for len(queue) > 0 {
		peak = max(peak, len(queue))
		f := queue[0]
		queue = queue[1:]
		f()
	}
	if peak != maxGH {
		t.Errorf("peak %d gh runs at once, want %d", peak, maxGH)
	}
	if runner.prs != 3 || runner.titles != 2 {
		t.Errorf("%d pr reads and %d title reads, want 3 and 2", runner.prs, runner.titles)
	}
	for _, d := range []string{"/wt/a", "/wt/b", "/wt/c"} {
		if prs.PR(d) != 7 {
			t.Errorf("no PR for %s", d)
		}
	}
	for _, n := range []string{"1", "2"} {
		if titles.Title("/wt/"+n, n) != "t"+n {
			t.Errorf("no title for %s", n)
		}
	}
	if gh.active != 0 || len(gh.waiting) != 0 {
		t.Errorf("active %d, waiting %d at the end", gh.active, len(gh.waiting))
	}
}

func TestAttachGivesOneGHQueueToPRsAndTitles(t *testing.T) {
	f, _ := themeFrame(t, nil)
	f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
	f.Usage = usage.NewSampler(usage.Unread, nil)
	stop := f.Attach(&fakeWindow{visible: true})
	defer stop()
	if f.WorkerPRs.GH == nil || f.WorkerPRs.GH != f.IssueTitles.GH {
		t.Errorf("queues %p and %p, want one shared queue", f.WorkerPRs.GH, f.IssueTitles.GH)
	}
}
