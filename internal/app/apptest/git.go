package apptest

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/git"
)

// BlockingGit is a git that blocks until its context ends. Each call sends
// its context on calls.
type BlockingGit struct{ calls chan context.Context }

// NewBlockingGit returns a BlockingGit.
func NewBlockingGit() *BlockingGit { return &BlockingGit{calls: make(chan context.Context, 100)} }

// Run blocks until ctx ends.
func (b *BlockingGit) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return b.RunInput(ctx, dir, nil, args...)
}

// RunInput blocks until ctx ends.
func (b *BlockingGit) RunInput(ctx context.Context, _ string, _ io.Reader, args ...string) ([]byte, error) {
	b.calls <- ctx
	<-ctx.Done()
	return nil, &git.Error{Args: args, Err: ctx.Err()}
}

// First waits for the first call of a read and gives its context.
func (b *BlockingGit) First(t *testing.T) context.Context {
	t.Helper()
	select {
	case ctx := <-b.calls:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("git did not run")
		return nil
	}
}

// Ended waits until ctx ends and checks why.
func Ended(t *testing.T, ctx context.Context, want error) {
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

// PostQueue runs the posted funcs on the test goroutine.
type PostQueue chan func()

// Post queues fn.
func (q PostQueue) Post(fn func()) { q <- fn }

// Run runs the next queued func. It fails the test when none comes in 5 s.
func (q PostQueue) Run(t *testing.T) {
	t.Helper()
	select {
	case fn := <-q:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was posted")
	}
}
