package ghsource

// The numbers of a repository that the chat shows as chips (D262). [Refs] asks gh, off the UI
// thread, what number n is: a pull request with its checks, an issue, or nothing. It reads a
// number only when a view asks for it (a chip on screen), keeps the answer, and asks again only
// when a view asks after the answer is old: an open pull request with pending checks after
// [prMissTTL], another answer after [prHitTTL], a failed read after [titleMissTTL]. There is no
// timer. At most [maxGH] gh runs go at once, with the other gh reads ([GHQueue]).

import (
	"context"
	"log"
	"time"

	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/github"
)

type refAnswer struct {
	ref    github.Ref
	at     time.Time
	failed bool
}

// Refs maps a number of the repository of a folder to what it is. Use it on the UI thread.
type Refs struct {
	// Env runs the reads off the UI thread and redraws after an answer.
	Env view.Env
	// Client gives the GitHub client for a folder. Nil: [github.New].
	Client func(dir string) github.Client
	// Now is the clock. Nil: time.Now.
	Now func() time.Time
	// GH is the queue of gh runs, shared with the other reads. Nil: a queue of its own.
	GH *GHQueue

	answers  map[titleKey]refAnswer
	inflight map[titleKey]bool
}

func (r *Refs) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Get returns what number n of the repository in dir is, ok false while it is not known or the
// repository has no such number. It starts a read when there is no fresh answer.
func (r *Refs) Get(dir string, n int) (github.Ref, bool) {
	if r == nil || dir == "" || n <= 0 {
		return github.Ref{}, false
	}
	k := titleKey{dir, n}
	r.read(k)
	a := r.answers[k]
	return a.ref, a.ref.Number > 0
}

func (r *Refs) fresh(a refAnswer) bool {
	ttl := prHitTTL
	switch {
	case a.failed:
		ttl = titleMissTTL
	case a.ref.PR && a.ref.State == "OPEN" && a.ref.Checks.Pending > 0:
		ttl = prMissTTL
	}
	return r.now().Sub(a.at) < ttl
}

func (r *Refs) read(k titleKey) {
	if r.answers == nil {
		r.answers, r.inflight = map[titleKey]refAnswer{}, map[titleKey]bool{}
	}
	if r.inflight[k] {
		return
	}
	if a, ok := r.answers[k]; ok && r.fresh(a) {
		return
	}
	r.inflight[k] = true
	mk := r.Client
	if mk == nil {
		mk = github.New
	}
	if r.GH == nil {
		r.GH = &GHQueue{}
	}
	r.GH.Do(r.Env, func() func() {
		ref, err := mk(k.dir).RefState(context.Background(), k.n)
		return func() {
			delete(r.inflight, k)
			if err != nil {
				log.Printf("ref #%d: %v", k.n, err)
				ref = r.answers[k].ref // an error keeps what is known
			}
			r.answers[k] = refAnswer{ref, r.now(), err != nil}
			r.Env.Redraw()
		}
	})
}
