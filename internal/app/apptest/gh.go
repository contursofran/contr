package apptest

import (
	"context"
	"errors"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/github"
)

// The fakes for the gh reads of package ghsource. They do not import ghsource, so that the
// tests in ghsource can use them too.

// BranchRunner answers `pr view --json number` per folder.
type BranchRunner struct {
	Out   map[string]string // dir -> reply; missing dir: no pull request
	Err   error
	Calls int
}

// Run gives Err, the reply of dir, or "no pull requests found".
func (r *BranchRunner) Run(_ context.Context, dir string, _ ...string) ([]byte, error) {
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	if s, ok := r.Out[dir]; ok {
		return []byte(s), nil
	}
	return nil, errors.New(`no pull requests found for branch "x"`)
}

// TitleRunner answers `issue view N --json title` and counts the calls.
type TitleRunner struct {
	Out   map[string]string // issue number -> reply
	Err   error
	Calls int
	Args  []string
}

// Run gives Err or the reply of the issue number in args.
func (r *TitleRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.Calls++
	r.Args = args
	if r.Err != nil {
		return nil, r.Err
	}
	return []byte(r.Out[args[2]]), nil
}

// GHRig holds what a test gives to the gh reads: a queue of the work off the UI thread, the
// checked-out branch of each folder, a clock and a count of the redraws.
type GHRig struct {
	Queue  []func()
	Branch string
	Now    time.Time
	Redraw int
}

// Env queues the work in Queue and counts the redraws in Redraw.
func (r *GHRig) Env() view.Env {
	return view.Env{Run: view.Runner{Go: func(f func()) { r.Queue = append(r.Queue, f) }},
		Invalidate: func() { r.Redraw++ }}
}

// BranchOf gives Branch for each folder.
func (r *GHRig) BranchOf(string) string { return r.Branch }

// Clock gives Now.
func (r *GHRig) Clock() time.Time { return r.Now }

// Flush runs queued work until none is left.
func (r *GHRig) Flush() {
	for len(r.Queue) > 0 {
		f := r.Queue[0]
		r.Queue = r.Queue[1:]
		f()
	}
}

// ClientOf gives the GitHub client of a folder on the runner run.
func ClientOf(run github.Runner) func(dir string) github.Client {
	return func(dir string) github.Client { return github.Client{R: run, Dir: dir} }
}

// LiveWorker is one live builder in the worktree dir.
func LiveWorker(dir string) []agents.Worker {
	return []agents.Worker{{Name: "b", Role: "builder", Worktree: dir, Live: true}}
}

// IssueWorker is one live builder in the worktree dir on the issue.
func IssueWorker(dir, issue string) []agents.Worker {
	return []agents.Worker{{Name: "b", Role: "builder", Worktree: dir, Issue: issue, Live: true}}
}

// EntryWorker is a live, blocked builder on issue 268 in the worktree /wt/a.
var EntryWorker = agents.Worker{Name: "b-1", Address: "t1", Role: "builder", Issue: "268", Worktree: "/wt/a",
	Project: "a", Live: true, Status: "blocked"}
