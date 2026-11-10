package ghsource

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/github"
)

// refRunner answers `gh pr view N` with Out, or Err.
type refRunner struct {
	Out   string
	Err   error
	Calls int
}

func (r *refRunner) Run(context.Context, string, ...string) ([]byte, error) {
	r.Calls++
	if r.Err != nil {
		return nil, r.Err
	}
	return []byte(r.Out), nil
}

func newRefRig(out string) (*apptest.GHRig, *refRunner, *Refs) {
	rig := &apptest.GHRig{Now: time.Unix(5000, 0)}
	run := &refRunner{Out: out}
	return rig, run, &Refs{Env: rig.Env(), Client: apptest.ClientOf(run), Now: rig.Clock}
}

const passedPR = `{"number":667,"state":"OPEN","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}`

func TestRefIsReadOffThreadOnceAndKept(t *testing.T) {
	rig, run, refs := newRefRig(passedPR)
	if _, ok := refs.Get("/wt", 667); ok || len(rig.Queue) != 1 {
		t.Fatalf("known at once, or %d reads queued", len(rig.Queue))
	}
	refs.Get("/wt", 667) // in flight: no second read
	rig.Flush()
	got, ok := refs.Get("/wt", 667)
	if !ok || !got.PR || got.Checks.Pass != 1 || rig.Redraw != 1 {
		t.Fatalf("got %+v %v, redraws %d", got, ok, rig.Redraw)
	}
	for range 3 {
		refs.Get("/wt", 667)
		rig.Flush()
	}
	if run.Calls != 1 || rig.Redraw != 1 {
		t.Errorf("gh ran %d times, %d redraws: the answer is not kept", run.Calls, rig.Redraw)
	}
	rig.Now = rig.Now.Add(prHitTTL + time.Second)
	refs.Get("/wt", 667)
	rig.Flush()
	if run.Calls != 2 {
		t.Errorf("gh ran %d times after the TTL, want 2", run.Calls)
	}
}

func TestRefWithPendingChecksIsAskedAgainSooner(t *testing.T) {
	rig, run, refs := newRefRig(`{"number":7,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`)
	refs.Get("/wt", 7)
	rig.Flush()
	rig.Now = rig.Now.Add(prMissTTL + time.Second)
	refs.Get("/wt", 7)
	rig.Flush()
	if run.Calls != 2 {
		t.Errorf("gh ran %d times, want 2", run.Calls)
	}
}

func TestRefErrorKeepsWhatIsKnownAndUnknownStaysUnknown(t *testing.T) {
	rig, run, refs := newRefRig(passedPR)
	refs.Get("/wt", 667)
	rig.Flush()
	run.Err = errors.New("no network")
	rig.Now = rig.Now.Add(prHitTTL + time.Second)
	refs.Get("/wt", 667)
	rig.Flush()
	if got, ok := refs.Get("/wt", 667); !ok || got.Number != 667 {
		t.Errorf("an error dropped the known answer: %+v", got)
	}
	run.Err = errors.New("GraphQL: Could not resolve to an issue")
	if _, ok := refs.Get("/wt", 3); ok {
		t.Error("3 is known before its read")
	}
	rig.Flush()
	if got, ok := refs.Get("/wt", 3); ok || got != (github.Ref{}) {
		t.Errorf("an unknown number is known: %+v", got)
	}
	var none *Refs
	if _, ok := none.Get("/wt", 1); ok {
		t.Error("a nil Refs knows a number")
	}
	if _, ok := refs.Get("", 1); ok {
		t.Error("no folder knows a number")
	}
}
