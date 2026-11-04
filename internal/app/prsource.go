package app

// The source of a worker's pull request (D104). The board needs to know whether a worker's
// branch has a pull request. [WorkerPRs] asks gh in the worker's worktree, off the UI thread,
// when the agents change. It keeps the answer per worktree and branch. A found number stays;
// "no pull request" is asked again after [prMissTTL], because the worker may open its PR later.
// There is no timer: a read starts only from an agents sync.

import (
	"context"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/github"
)

// prMissTTL is how long a "no pull request" answer holds.
const prMissTTL = time.Minute

type prKey struct{ dir, branch string }

type prAnswer struct {
	n  int
	at time.Time
}

// WorkerPRs maps a worker's worktree to the number of its pull request. Use it on the UI thread.
type WorkerPRs struct {
	// Run runs the reads off the UI thread.
	Run Runner
	// Client gives the GitHub client for a folder. Nil: [github.New].
	Client func(dir string) github.Client
	// Branch returns the checked-out branch of a folder, "" when unknown. It runs off the UI
	// thread. Nil: reads HEAD with package git.
	Branch func(dir string) string
	// Invalidate redraws after an answer. May be nil.
	Invalidate func()
	// Now is the clock. Nil: time.Now.
	Now func() time.Time

	branch   map[string]string
	answers  map[prKey]prAnswer
	inflight map[string]bool
}

func (w *WorkerPRs) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// PR returns the pull request number of the worktree dir on its last known branch, 0 for none
// or not known yet.
func (w *WorkerPRs) PR(dir string) int {
	if w == nil || dir == "" {
		return 0
	}
	b, ok := w.branch[dir]
	if !ok {
		return 0
	}
	return w.answers[prKey{dir, b}].n
}

// Sync starts a read for each worktree of live workers that has none running and no fresh
// answer.
func (w *WorkerPRs) Sync(workers []agents.Worker) {
	seen := map[string]bool{}
	for _, wk := range workers {
		if wk.Worktree == "" || seen[wk.Worktree] || !wk.Live {
			continue
		}
		seen[wk.Worktree] = true
		w.read(wk.Worktree)
	}
}

func (w *WorkerPRs) read(dir string) {
	if w.inflight == nil {
		w.inflight, w.branch, w.answers = map[string]bool{}, map[string]string{}, map[prKey]prAnswer{}
	}
	if w.inflight[dir] {
		return
	}
	w.inflight[dir] = true
	branchOf := w.Branch
	if branchOf == nil {
		branchOf = headBranchOf
	}
	mk := w.Client
	if mk == nil {
		mk = github.New
	}
	w.Run.Do(func() func() {
		branch := branchOf(dir)
		return func() {
			if branch == "" { // not a repository, or no HEAD
				delete(w.inflight, dir)
				return
			}
			w.branch[dir] = branch
			key := prKey{dir, branch}
			if a, ok := w.answers[key]; ok && (a.n > 0 || w.now().Sub(a.at) < prMissTTL) {
				delete(w.inflight, dir)
				w.redraw() // the branch may have moved back to a known one
				return
			}
			w.Run.Do(func() func() {
				n, err := mk(dir).BranchPR(context.Background())
				return func() {
					delete(w.inflight, dir)
					if err != nil {
						n = 0
					}
					w.answers[key] = prAnswer{n, w.now()}
					w.redraw()
				}
			})
		}
	})
}

func (w *WorkerPRs) redraw() {
	if w.Invalidate != nil {
		w.Invalidate()
	}
}

// headBranchOf reads the branch of the checkout that holds dir.
func headBranchOf(dir string) string {
	co, ok := git.Discover(dir)
	if !ok {
		return ""
	}
	b, _ := git.HeadBranch(co.GitDir)
	return b
}
