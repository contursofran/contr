package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/git"
)

// blockingGit is a git that blocks until its context ends. Each call sends
// its context on calls.
type blockingGit struct{ calls chan context.Context }

func newBlockingGit() *blockingGit { return &blockingGit{calls: make(chan context.Context, 100)} }

func (b *blockingGit) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return b.RunInput(ctx, dir, nil, args...)
}

func (b *blockingGit) RunInput(ctx context.Context, _ string, _ io.Reader, args ...string) ([]byte, error) {
	b.calls <- ctx
	<-ctx.Done()
	return nil, &git.Error{Args: args, Err: ctx.Err()}
}

// first waits for the first call of a read and gives its context.
func (b *blockingGit) first(t *testing.T) context.Context {
	t.Helper()
	select {
	case ctx := <-b.calls:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("git did not run")
		return nil
	}
}

// ended waits until ctx ends and checks why.
func ended(t *testing.T, ctx context.Context, want error) {
	t.Helper()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), want) {
			t.Fatalf("context ended with %v, want %v", ctx.Err(), want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not end")
	}
}

// postQueue runs the posted funcs on the test goroutine.
type postQueue chan func()

func (q postQueue) post(fn func()) { q <- fn }

func (q postQueue) run(t *testing.T) {
	t.Helper()
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was posted")
	}
}

func blockedReview(timeout time.Duration) (*Review, *blockingGit, postQueue) {
	g, q := newBlockingGit(), make(postQueue, 100)
	r := NewReview(view.Env{Run: view.Runner{Post: q.post}})
	r.git, r.timeout = g, timeout
	return r, g, q
}

func TestReviewNewerReadEndsTheOldOne(t *testing.T) {
	r, g, q := blockedReview(time.Hour)
	r.SetRepo(t.TempDir())
	old := g.first(t)
	r.SetRepo(t.TempDir())
	ended(t, old, context.Canceled)
	q.run(t) // the old result is dropped
	if r.Err() != "" || r.loaded() {
		t.Fatalf("old result applied: err %q, loaded %v", r.Err(), r.loaded())
	}
	r.Close()
}

func TestReviewCloseEndsTheRead(t *testing.T) {
	r, g, q := blockedReview(time.Hour)
	r.SetRepo(t.TempDir())
	ctx := g.first(t)
	r.Close()
	ended(t, ctx, context.Canceled)
	q.run(t)
	if r.Err() != "" || r.loaded() {
		t.Fatalf("result applied after Close: err %q", r.Err())
	}
}

func TestReviewReadPastTheTimeLimitFails(t *testing.T) {
	r, g, q := blockedReview(50 * time.Millisecond)
	r.SetRepo(t.TempDir())
	ended(t, g.first(t), context.DeadlineExceeded)
	q.run(t)
	if !strings.Contains(r.Err(), "deadline exceeded") {
		t.Fatalf("err %q, want the time limit", r.Err())
	}
	if r.reads.Busy() {
		t.Fatal("no read is in flight after the result")
	}
}

func blockedTree(t *testing.T, timeout time.Duration) (*FileTree, *blockingGit, postQueue) {
	g, q := newBlockingGit(), make(postQueue, 100)
	dir := t.TempDir()
	ft := NewFileTree(func() string { return dir })
	ft.Git, ft.Timeout, ft.Env.Run.Post = g, timeout, q.post
	ft.Env.Run.Go = func(fn func()) { go fn() }
	ft.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	return ft, g, q
}

func TestFileTreeNewerReadEndsTheOldOne(t *testing.T) {
	ft, g, _ := blockedTree(t, time.Hour)
	ft.Sync()
	old := g.first(t)
	ft.refresh()
	ended(t, old, context.Canceled)
	ft.Close()
}

func TestFileTreeCloseEndsTheRead(t *testing.T) {
	ft, g, _ := blockedTree(t, time.Hour)
	ft.Sync()
	ctx := g.first(t)
	ft.Close()
	ended(t, ctx, context.Canceled)
}

func TestFileTreeReadAfterCloseIsDropped(t *testing.T) {
	ft, g, q := blockedTree(t, time.Hour)
	ft.Sync()
	ctx := g.first(t)
	ft.Close()
	ended(t, ctx, context.Canceled)
	q.run(t) // the old result is dropped
	if ft.err != "" || len(ft.loaded) != 0 {
		t.Fatalf("result applied after Close: err %q, loaded %d", ft.err, len(ft.loaded))
	}
}

func TestFileTreeReadPastTheTimeLimitFails(t *testing.T) {
	ft, g, q := blockedTree(t, 50*time.Millisecond)
	ft.Sync()
	ended(t, g.first(t), context.DeadlineExceeded)
	q.run(t)
	if !strings.Contains(ft.err, "deadline exceeded") {
		t.Fatalf("err %q, want the time limit", ft.err)
	}
	ft.Close()
}
