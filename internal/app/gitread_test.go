package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/apptest"
)

func blockedTree(t *testing.T, timeout time.Duration) (*FileTree, *apptest.BlockingGit, apptest.PostQueue) {
	g, q := apptest.NewBlockingGit(), make(apptest.PostQueue, 100)
	dir := t.TempDir()
	ft := NewFileTree(func() string { return dir })
	ft.Git, ft.Timeout, ft.Env.Run.Post = g, timeout, q.Post
	ft.Env.Run.Go = func(fn func()) { go fn() }
	ft.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	return ft, g, q
}

func TestFileTreeNewerReadEndsTheOldOne(t *testing.T) {
	ft, g, _ := blockedTree(t, time.Hour)
	ft.Sync()
	old := g.First(t)
	ft.refresh()
	apptest.Ended(t, old, context.Canceled)
	ft.Close()
}

func TestFileTreeCloseEndsTheRead(t *testing.T) {
	ft, g, _ := blockedTree(t, time.Hour)
	ft.Sync()
	ctx := g.First(t)
	ft.Close()
	apptest.Ended(t, ctx, context.Canceled)
}

func TestFileTreeReadAfterCloseIsDropped(t *testing.T) {
	ft, g, q := blockedTree(t, time.Hour)
	ft.Sync()
	ctx := g.First(t)
	ft.Close()
	apptest.Ended(t, ctx, context.Canceled)
	q.Run(t) // the old result is dropped
	if ft.err != "" || len(ft.loaded) != 0 {
		t.Fatalf("result applied after Close: err %q, loaded %d", ft.err, len(ft.loaded))
	}
}

func TestFileTreeReadPastTheTimeLimitFails(t *testing.T) {
	ft, g, q := blockedTree(t, 50*time.Millisecond)
	ft.Sync()
	apptest.Ended(t, g.First(t), context.DeadlineExceeded)
	q.Run(t)
	if !strings.Contains(ft.err, "deadline exceeded") {
		t.Fatalf("err %q, want the time limit", ft.err)
	}
	ft.Close()
}
