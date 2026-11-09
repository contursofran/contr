// Package ghsourcetest holds test helpers that build ghsource values on the fakes of apptest. The
// tests of more than one package use them. Only _test.go files import it. The tests of ghsource
// do not, because this package imports ghsource.
package ghsourcetest

import (
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/ghsource"
)

// PRSourceRig is a WorkerPRs on a fake gh runner, a held queue and a fixed clock.
type PRSourceRig struct {
	*apptest.GHRig
	W      *ghsource.WorkerPRs
	Runner *apptest.BranchRunner
}

// NewPRSourceRig makes a rig whose runner answers out, on branch "agent/1-x".
func NewPRSourceRig(out map[string]string) *PRSourceRig {
	r := &PRSourceRig{GHRig: &apptest.GHRig{Branch: "agent/1-x", Now: time.Unix(1000, 0)},
		Runner: &apptest.BranchRunner{Out: out}}
	r.W = &ghsource.WorkerPRs{Env: r.Env(), Client: apptest.ClientOf(r.Runner), Branch: r.BranchOf, Now: r.Clock}
	return r
}

// EntryCalls records the folders and numbers that a WorkerEntry of [EntryRig] opened.
type EntryCalls struct{ PRs, Issues [][2]any }

// EntryRig makes a WorkerEntry with fake openers. The worktree /wt/a has the PR prN (0: none).
func EntryRig(prN int) (ghsource.WorkerEntry, *EntryCalls) {
	calls := &EntryCalls{}
	rig := NewPRSourceRig(nil)
	if prN > 0 {
		rig = NewPRSourceRig(map[string]string{"/wt/a": `{"number":` + itoa(prN) + `}`})
	}
	rig.W.Sync(apptest.LiveWorker("/wt/a"))
	rig.Flush()
	return ghsource.WorkerEntry{
		PRs:       rig.W,
		OpenPR:    func(dir string, n int) { calls.PRs = append(calls.PRs, [2]any{dir, n}) },
		OpenIssue: func(dir string, n int) { calls.Issues = append(calls.Issues, [2]any{dir, n}) },
	}, calls
}

func itoa(n int) string {
	return string(rune('0'+n/100)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}
