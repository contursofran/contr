package app

// The issue titles of the worker rows (D174). Line 1 of a worker row is the title of its issue.
// [IssueTitles] asks gh in the worker's folder, off the UI thread, when the agents change. A
// found title is asked again after [titleHitTTL] (the owner may edit it); a failed read (no
// gh, no network, no such issue) is asked again after [titleMissTTL] and keeps the title that was
// known. There is no timer: a read starts only from an agents sync, and only for an issue with
// no fresh answer, so an idle screen does no gh work. At most [maxGH] reads run at once, with
// the pull request reads counted apart.

import (
	"context"
	"log"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/github"
)

const (
	titleHitTTL  = 10 * time.Minute
	titleMissTTL = 5 * time.Minute
)

type titleKey struct {
	dir string
	n   int
}

type titleAnswer struct {
	title  string
	at     time.Time
	failed bool // the last read failed: ask again after the shorter time
}

// IssueTitles maps the issue of a worker to its title. Use it on the UI thread.
type IssueTitles struct {
	// Env runs the reads off the UI thread and redraws after an answer.
	Env Env
	// Client gives the GitHub client for a folder. Nil: [github.New].
	Client func(dir string) github.Client
	// Now is the clock. Nil: time.Now.
	Now func() time.Time

	answers  map[titleKey]titleAnswer
	inflight map[titleKey]bool
	active   int
	waiting  []func()
}

func (t *IssueTitles) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// Title is the last known title of the issue label (such as "#443") of the worker in dir, ""
// when none is known.
func (t *IssueTitles) Title(dir, issue string) string {
	n, ok := issueNumber(issue)
	if t == nil || !ok || dir == "" {
		return ""
	}
	return t.answers[titleKey{dir, n}].title
}

// Sync starts a read for the issue of each live worker that has none running and no fresh
// answer.
func (t *IssueTitles) Sync(workers []agents.Worker) {
	// Drop the answers of workers that are gone, so the cache stays small.
	keep := map[titleKey]bool{}
	for _, w := range workers {
		if n, ok := issueNumber(w.Issue); ok && w.Worktree != "" {
			keep[titleKey{w.Worktree, n}] = true
		}
	}
	for k := range t.answers {
		if !keep[k] {
			delete(t.answers, k)
		}
	}
	for _, w := range workers {
		if n, ok := issueNumber(w.Issue); ok && w.Live && w.Worktree != "" && !w.Orchestrator() {
			t.read(titleKey{w.Worktree, n})
		}
	}
}

func (t *IssueTitles) read(k titleKey) {
	if t.answers == nil {
		t.answers, t.inflight = map[titleKey]titleAnswer{}, map[titleKey]bool{}
	}
	if t.inflight[k] {
		return
	}
	if a, ok := t.answers[k]; ok {
		ttl := titleMissTTL
		if a.title != "" && !a.failed {
			ttl = titleHitTTL
		}
		if t.now().Sub(a.at) < ttl {
			return
		}
	}
	t.inflight[k] = true
	mk := t.Client
	if mk == nil {
		mk = github.New
	}
	t.gh(func() func() {
		title, err := mk(k.dir).IssueTitle(context.Background(), k.n)
		return func() {
			delete(t.inflight, k)
			if err != nil {
				log.Printf("issue title #%d: %v", k.n, err)
				title = t.answers[k].title // an error keeps what is known
			}
			t.answers[k] = titleAnswer{title, t.now(), err != nil}
			t.Env.Redraw()
		}
	})
}

// gh runs a read off the UI thread, with at most [maxGH] at once. Others wait their turn.
func (t *IssueTitles) gh(read func() func()) {
	start := func() {
		t.active++
		t.Env.Do(func() func() {
			done := read()
			return func() {
				t.active--
				done()
				if len(t.waiting) > 0 && t.active < maxGH {
					next := t.waiting[0]
					t.waiting = t.waiting[1:]
					next()
				}
			}
		})
	}
	if t.active >= maxGH {
		t.waiting = append(t.waiting, start)
		return
	}
	start()
}
