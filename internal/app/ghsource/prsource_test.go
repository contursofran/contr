package ghsource

import (
	"errors"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/apptest"
)

type prSourceRig struct {
	*apptest.GHRig
	w      *WorkerPRs
	runner *apptest.BranchRunner
}

func newPRSourceRig(out map[string]string) *prSourceRig {
	r := &prSourceRig{GHRig: &apptest.GHRig{Branch: "agent/1-x", Now: time.Unix(1000, 0)},
		runner: &apptest.BranchRunner{Out: out}}
	r.w = &WorkerPRs{Env: r.Env(), Client: apptest.ClientOf(r.runner), Branch: r.BranchOf, Now: r.Clock}
	return r
}

func TestWorkerPRFromBranch(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	if r.w.PR("/wt/a") != 0 {
		t.Fatal("answer before any read")
	}
	r.w.Sync(apptest.LiveWorker("/wt/a"))
	r.Flush()
	if got := r.w.PR("/wt/a"); got != 307 {
		t.Errorf("PR = %d, want 307", got)
	}
	if r.Redraw == 0 {
		t.Error("no redraw after the answer")
	}
}

func TestWorkerNoPRFallsBackToIssue(t *testing.T) {
	r := newPRSourceRig(nil)
	r.w.Sync(apptest.LiveWorker("/wt/a"))
	r.Flush()
	if got := r.w.PR("/wt/a"); got != 0 {
		t.Errorf("PR = %d, want 0", got)
	}
}

func TestWorkerPRCachesPerWorktreeAndBranch(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	r.w.Sync(ws)
	r.w.Sync(ws)
	r.Flush()
	if r.runner.Calls != 1 {
		t.Errorf("gh ran %d times for one worktree and branch, want 1", r.runner.Calls)
	}
	r.Branch = "agent/2-y" // a new branch asks again, once the answer is old
	r.Now = r.Now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.Flush()
	if r.runner.Calls != 2 {
		t.Errorf("gh ran %d times after a branch change, want 2", r.runner.Calls)
	}
}

func TestWorkerPRMissIsAskedAgainAfterTheTTL(t *testing.T) {
	r := newPRSourceRig(nil)
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	r.w.Sync(ws)
	r.Flush()
	if r.runner.Calls != 1 {
		t.Fatalf("asked again within the TTL: %d calls", r.runner.Calls)
	}
	r.runner.Out = map[string]string{"/wt/a": `{"number":9}`}
	r.Now = r.Now.Add(prMissTTL + time.Second)
	r.w.Sync(ws)
	r.Flush()
	if r.w.PR("/wt/a") != 9 {
		t.Errorf("PR = %d after the PR opened, want 9", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRSkipsRunningReadsEndedAndFolderless(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":1}`})
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.w.Sync(ws) // a read is in flight: no second one
	if len(r.Queue) != 1 {
		t.Errorf("queued %d reads, want 1", len(r.Queue))
	}
	r.Flush()
	r2 := newPRSourceRig(nil)
	r2.w.Sync([]agents.Worker{{Worktree: "/wt/b"}, {Live: true}})
	if len(r2.Queue) != 0 {
		t.Error("read for an ended worker or one without a folder")
	}
}

func TestWorkerPRErrorsAndUnknownBranchGiveNone(t *testing.T) {
	r := newPRSourceRig(nil)
	r.runner.Err = errors.New("gh: boom")
	r.w.Sync(apptest.LiveWorker("/wt/a"))
	r.Flush()
	if r.w.PR("/wt/a") != 0 {
		t.Error("an error gave a PR")
	}
	r.Branch = ""
	r.w.Sync(apptest.LiveWorker("/wt/c"))
	r.Flush()
	if r.w.PR("/wt/c") != 0 || r.runner.Calls != 1 {
		t.Errorf("no branch: PR %d, calls %d", r.w.PR("/wt/c"), r.runner.Calls)
	}
	var nilW *WorkerPRs
	if nilW.PR("/x") != 0 {
		t.Error("nil source gave a PR")
	}
}

func TestWorkerPRFreshAnswerDoesNoGitRead(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307}`})
	reads := 0
	r.w.Branch = func(string) string { reads++; return r.Branch }
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	r.w.Sync(ws)
	r.w.Sync(ws)
	if len(r.Queue) != 0 || reads != 1 {
		t.Errorf("fresh answer: %d queued, %d git reads, want 0 and 1", len(r.Queue), reads)
	}
}

func TestWorkerPRClosedPRLeavesTheRow(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307,"state":"OPEN"}`})
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	if r.w.PR("/wt/a") != 307 {
		t.Fatal("open PR not found")
	}
	r.runner.Out["/wt/a"] = `{"number":307,"state":"MERGED"}`
	r.w.Sync(ws) // still fresh: no read
	r.Flush()
	if r.w.PR("/wt/a") != 307 || r.runner.Calls != 1 {
		t.Fatalf("read before the TTL: PR %d, calls %d", r.w.PR("/wt/a"), r.runner.Calls)
	}
	r.Now = r.Now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.Flush()
	if r.w.PR("/wt/a") != 0 {
		t.Errorf("merged PR stays: %d", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRErrorOnRecheckKeepsTheNumber(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307,"state":"OPEN"}`})
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	r.runner.Err = errors.New("gh: offline")
	r.Now = r.Now.Add(prHitTTL + time.Second)
	r.w.Sync(ws)
	r.Flush()
	if r.w.PR("/wt/a") != 307 {
		t.Errorf("PR = %d after a failed re-check, want 307", r.w.PR("/wt/a"))
	}
}

func TestWorkerPRBoundsGHRuns(t *testing.T) {
	r := newPRSourceRig(nil)
	var ws []agents.Worker
	for _, d := range []string{"/wt/a", "/wt/b", "/wt/c", "/wt/d", "/wt/e"} {
		ws = append(ws, apptest.LiveWorker(d)...)
	}
	r.w.Sync(ws)
	// run the branch reads and their answers: each queues a gh run
	for i := 0; i < 5; i++ {
		f := r.Queue[0]
		r.Queue = r.Queue[1:]
		f()
	}
	peak := 0
	for len(r.Queue) > 0 {
		if r.w.GH.active > peak {
			peak = r.w.GH.active
		}
		f := r.Queue[0]
		r.Queue = r.Queue[1:]
		f()
	}
	if peak > 2 || r.runner.Calls != 5 {
		t.Errorf("peak %d gh runs (max %d), %d calls, want 5", peak, 2, r.runner.Calls)
	}
	if r.w.GH.active != 0 || len(r.w.GH.waiting) != 0 {
		t.Errorf("active %d, waiting %d at the end", r.w.GH.active, len(r.w.GH.waiting))
	}
}

func TestWorkerPRWaitingTracksPendingChecksAndAsksSoonerThenIdles(t *testing.T) {
	pending := `{"number":307,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`
	r := newPRSourceRig(map[string]string{"/wt/a": pending})
	ws := apptest.LiveWorker("/wt/a")
	if r.w.Waiting("/wt/a") {
		t.Fatal("waiting before any read")
	}
	r.w.Sync(ws)
	r.Flush()
	if !r.w.Waiting("/wt/a") {
		t.Fatal("not waiting with a check in progress")
	}
	// Pending checks hold like a miss: a minute, not five.
	r.Now = r.Now.Add(prMissTTL + time.Second)
	r.runner.Out["/wt/a"] = `{"number":307,"state":"OPEN","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"}]}`
	r.w.Sync(ws)
	r.Flush()
	if r.w.Waiting("/wt/a") {
		t.Error("still waiting after the checks passed")
	}
	if r.runner.Calls != 2 {
		t.Errorf("gh ran %d times, want 2", r.runner.Calls)
	}
}

func TestWorkerPRKeepsPendingWhenGhFails(t *testing.T) {
	r := newPRSourceRig(map[string]string{"/wt/a": `{"number":307,"state":"OPEN","statusCheckRollup":[{"status":"QUEUED"}]}`})
	ws := apptest.LiveWorker("/wt/a")
	r.w.Sync(ws)
	r.Flush()
	r.runner.Err = errors.New("gh: offline")
	r.Now = r.Now.Add(prMissTTL + time.Second)
	r.w.Sync(ws)
	r.Flush()
	if !r.w.Waiting("/wt/a") || r.w.PR("/wt/a") != 307 {
		t.Error("a failed read lost what was known")
	}
}
