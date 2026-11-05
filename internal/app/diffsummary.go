package app

// The data of the title bar's DiffSummary (docs/spec/DiffSummary.md): the changed lines and
// files of the selected project. git runs on a goroutine and the result lands through the
// window's Update. The counts are read again when a branch file changes: no timer. Its methods
// run on the UI thread.

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/review"
)

// DiffSummary holds the counts of one folder and the watch that keeps them fresh.
type DiffSummary struct {
	// Read gives the counts of the repository in dir. It runs git, so it runs off the UI thread.
	// Nil: [ReadSummary].
	Read func(dir string) (review.Summary, error)
	// Watch calls fn once and again after each change of a branch of the repository in dir,
	// until ctx ends. It blocks. Nil: [WatchBranches].
	Watch func(ctx context.Context, dir string, fn func()) error
	// Post runs fn on the UI thread (Window.Update). Invalidate repaints.
	Post       func(fn func())
	Invalidate func()

	dir    string
	gen    int // the watch of dir; a result from an older one is dropped
	cancel context.CancelFunc
	sum    review.Summary
	ok     bool // sum is the count of dir
}

// ReadSummary adds up the branches of dir that are ahead of the Review base.
func ReadSummary(dir string) (review.Summary, error) {
	return review.Repo{Dir: dir, Base: reviewBase}.Summary()
}

// WatchBranches calls fn now and after each change of the branch files of the repository of dir.
// The watch needs only a hint that something changed, so it stays cheap.
func WatchBranches(ctx context.Context, dir string, fn func()) error {
	refs, err := review.Repo{Dir: dir, Base: reviewBase}.RefsDir()
	if err != nil {
		return err
	}
	w, err := mygo.WatchFiles(ctx, refs, mygo.FileWatchOptions{Recursive: true, HintsOnly: true})
	if err != nil {
		return fmt.Errorf("diff summary: watch: %w", err)
	}
	defer w.Close()
	fn() // a change before the watch started has no event
	for {
		if _, err := w.Next(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("diff summary: watch: %w", err)
		}
		fn()
	}
}

// SetDir points the summary at the repository of dir. The same dir does nothing. An empty dir
// stops the watch and hides the counts.
func (d *DiffSummary) SetDir(dir string) {
	if dir == d.dir && (d.cancel != nil || dir == "") {
		return
	}
	d.Stop()
	d.dir, d.ok = dir, false
	if dir == "" {
		return
	}
	d.gen++
	gen := d.gen
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	read, watch := d.Read, d.Watch
	if read == nil {
		read = ReadSummary
	}
	if watch == nil {
		watch = WatchBranches
	}
	go func() {
		err := watch(ctx, dir, func() {
			sum, err := read(dir)
			d.Post(func() { d.set(gen, sum, err) })
		})
		if err != nil && ctx.Err() == nil {
			slog.Debug("diff summary: watch ended", "dir", dir, "err", err)
		}
	}()
}

// Stop ends the watch.
func (d *DiffSummary) Stop() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

// set takes in one read. A git error hides the counts and is logged.
func (d *DiffSummary) set(gen int, sum review.Summary, err error) {
	if gen != d.gen {
		return
	}
	if err != nil {
		slog.Debug("diff summary: read failed", "dir", d.dir, "err", err)
		d.sum, d.ok = review.Summary{}, false
	} else {
		d.sum, d.ok = sum, true
	}
	if d.Invalidate != nil {
		d.Invalidate()
	}
}

// View builds the button. open runs on a click.
func (d *DiffSummary) View(c *ui.Context, open func()) {
	if !d.ok {
		return
	}
	kit.DiffSummary(c, kit.DiffSummaryOpts{Added: d.sum.Added, Removed: d.sum.Removed, Files: d.sum.Files, OnClick: open})
}
