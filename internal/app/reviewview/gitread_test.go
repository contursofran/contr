package reviewview

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/view"
)

func blockedReview(timeout time.Duration) (*Review, *apptest.BlockingGit, apptest.PostQueue) {
	g, q := apptest.NewBlockingGit(), make(apptest.PostQueue, 100)
	r := NewReview(view.Env{Run: view.Runner{Post: q.Post}})
	r.git, r.timeout = g, timeout
	return r, g, q
}

func TestReviewNewerReadEndsTheOldOne(t *testing.T) {
	r, g, q := blockedReview(time.Hour)
	r.SetRepo(t.TempDir())
	old := g.First(t)
	r.SetRepo(t.TempDir())
	apptest.Ended(t, old, context.Canceled)
	q.Run(t) // the old result is dropped
	if r.Err() != "" || r.loaded() {
		t.Fatalf("old result applied: err %q, loaded %v", r.Err(), r.loaded())
	}
	r.Close()
}

func TestReviewCloseEndsTheRead(t *testing.T) {
	r, g, q := blockedReview(time.Hour)
	r.SetRepo(t.TempDir())
	ctx := g.First(t)
	r.Close()
	apptest.Ended(t, ctx, context.Canceled)
	q.Run(t)
	if r.Err() != "" || r.loaded() {
		t.Fatalf("result applied after Close: err %q", r.Err())
	}
}

func TestReviewReadPastTheTimeLimitFails(t *testing.T) {
	r, g, q := blockedReview(50 * time.Millisecond)
	r.SetRepo(t.TempDir())
	apptest.Ended(t, g.First(t), context.DeadlineExceeded)
	q.Run(t)
	if !strings.Contains(r.Err(), "deadline exceeded") {
		t.Fatalf("err %q, want the time limit", r.Err())
	}
	if r.reads.Busy() {
		t.Fatal("no read is in flight after the result")
	}
}
