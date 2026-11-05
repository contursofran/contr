package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/review"
)

// uiQueue stands for the window's Update: it holds what goroutines post, and the test runs it
// on its own goroutine, the "UI thread".
type uiQueue struct {
	mu sync.Mutex
	fn []func()
}

func (q *uiQueue) post(f func()) { q.mu.Lock(); q.fn = append(q.fn, f); q.mu.Unlock() }

// wait runs the posted functions until n have run, or fails after a second.
func (q *uiQueue) wait(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		q.mu.Lock()
		fns := q.fn
		q.fn = nil
		q.mu.Unlock()
		for _, f := range fns {
			f()
			n--
		}
		if n <= 0 {
			return
		}
	}
	t.Fatalf("%d posted functions never came", n)
}

// fakeWatch calls fn once, as the real watch does, and returns the function that fires another
// batch. It blocks until ctx ends.
type fakeWatch struct {
	mu   sync.Mutex
	fire map[string]func()
	stop map[string]bool
}

func (w *fakeWatch) watch(ctx context.Context, dir string, fn func()) error {
	w.mu.Lock()
	if w.fire == nil {
		w.fire, w.stop = map[string]func(){}, map[string]bool{}
	}
	w.fire[dir] = fn
	w.mu.Unlock()
	fn()
	<-ctx.Done()
	w.mu.Lock()
	w.stop[dir] = true
	w.mu.Unlock()
	return nil
}

func (w *fakeWatch) change(t *testing.T, dir string) {
	t.Helper()
	w.mu.Lock()
	fn := w.fire[dir]
	w.mu.Unlock()
	if fn == nil {
		t.Fatalf("no watch of %q", dir)
	}
	fn()
}

func (w *fakeWatch) stopped(dir string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stop[dir]
}

// readyDiff is a DiffSummary that already holds sum.
func readyDiff(sum review.Summary) *DiffSummary { return &DiffSummary{sum: sum, ok: true} }

func diffTester(d *DiffSummary, open func()) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Background(theme.Current().Ground).Children(func() { d.View(c, open) })
	}, 600, 40)
}

func TestDiffSummaryShowsTheCountsAndOpensReviewOnAClick(t *testing.T) {
	opened := 0
	tt := diffTester(readyDiff(review.Summary{Added: 120, Removed: 126, Files: 9}), func() { opened++ })
	for _, text := range []string{"+120", "−126", "9 files"} {
		if !tt.HasText(text) {
			t.Errorf("no text %q; texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("120 lines added, 126 removed, 9 files. Open review"); err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Errorf("the click opened Review %d times, want 1", opened)
	}
}

func TestDiffSummaryHidesWithNoChange(t *testing.T) {
	tt := diffTester(readyDiff(review.Summary{}), nil)
	if len(tt.Texts()) != 0 {
		t.Errorf("a project with no change shows %q", tt.Texts())
	}
}

func TestDiffSummaryHidesTheRemovedCountWithOnlyAdditions(t *testing.T) {
	tt := diffTester(readyDiff(review.Summary{Added: 4, Files: 1}), nil)
	if !tt.HasText("+4") || !tt.HasText("1 file") || tt.HasText("−0") {
		t.Errorf("texts %q, want +4 and 1 file only", tt.Texts())
	}
	if _, ok := tt.Find("4 lines added, 1 file. Open review"); !ok {
		t.Errorf("label missing; texts %q", tt.Texts())
	}
}

func TestDiffSummaryReadsOffTheUIThreadAndShowsTheResultOnTheUIThread(t *testing.T) {
	q := &uiQueue{}
	release := make(chan struct{})
	reading := make(chan struct{})
	w := &fakeWatch{}
	d := &DiffSummary{
		Read: func(dir string) (review.Summary, error) {
			close(reading)
			<-release // a slow git: SetDir must not wait for it
			return review.Summary{Added: 2, Removed: 1, Files: 1}, nil
		},
		Watch: w.watch, Post: q.post,
	}
	done := make(chan struct{})
	go func() { d.SetDir("/repo"); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetDir waits for the read: git runs on the UI thread")
	}
	<-reading
	if d.ok {
		t.Fatal("the counts show before the read ended")
	}
	close(release)
	q.wait(t, 1)
	if !d.ok || d.sum.Added != 2 {
		t.Errorf("sum %+v ok %v after the read", d.sum, d.ok)
	}
	d.Stop()
}

func TestDiffSummaryReadsAgainOnAWatchEventNotOnTime(t *testing.T) {
	q := &uiQueue{}
	w := &fakeWatch{}
	var mu sync.Mutex
	reads, added := 0, 1
	d := &DiffSummary{
		Read: func(string) (review.Summary, error) {
			mu.Lock()
			defer mu.Unlock()
			reads++
			return review.Summary{Added: added, Files: 1}, nil
		},
		Watch: w.watch, Post: q.post,
	}
	d.SetDir("/repo")
	q.wait(t, 1)
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	if reads != 1 {
		t.Errorf("%d reads with no event, want 1", reads)
	}
	added = 7
	mu.Unlock()
	w.change(t, "/repo")
	q.wait(t, 1)
	if d.sum.Added != 7 {
		t.Errorf("Added = %d after a watch event, want 7", d.sum.Added)
	}
	d.Stop()
}

func TestDiffSummaryHidesAndLogsOnAGitError(t *testing.T) {
	q := &uiQueue{}
	w := &fakeWatch{}
	d := &DiffSummary{
		Read:  func(string) (review.Summary, error) { return review.Summary{}, errors.New("not a git repository") },
		Watch: w.watch, Post: q.post,
	}
	d.SetDir("/not-a-repo")
	q.wait(t, 1)
	if d.ok {
		t.Error("the counts show after a git error")
	}
	if tt := diffTester(d, nil); len(tt.Texts()) != 0 {
		t.Errorf("the button shows after a git error: %q", tt.Texts())
	}
	d.Stop()
}

func TestDiffSummaryDropsTheResultOfAnOlderFolderAndStopsItsWatch(t *testing.T) {
	q := &uiQueue{}
	w := &fakeWatch{}
	slow := make(chan struct{})
	d := &DiffSummary{
		Read: func(dir string) (review.Summary, error) {
			if dir == "/a" {
				<-slow
				return review.Summary{Added: 1, Files: 1}, nil
			}
			return review.Summary{Added: 5, Files: 2}, nil
		},
		Watch: w.watch, Post: q.post,
	}
	d.SetDir("/a")
	d.SetDir("/b")
	q.wait(t, 1) // /b
	close(slow)
	q.wait(t, 1) // the late /a result
	if d.sum.Added != 5 {
		t.Errorf("Added = %d, want the 5 of /b: the late result of /a won", d.sum.Added)
	}
	for deadline := time.Now().Add(time.Second); !w.stopped("/a") && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
	}
	if !w.stopped("/a") {
		t.Error("the watch of /a runs after the folder changed")
	}
	d.Stop()
}

func TestDiffSummaryWithAnEmptyFolderHidesAndStartsNoWatch(t *testing.T) {
	q := &uiQueue{}
	w := &fakeWatch{}
	d := readyDiff(review.Summary{Added: 1, Files: 1})
	d.Watch, d.Post = w.watch, q.post
	d.dir = "/old"
	d.SetDir("")
	if d.ok {
		t.Error("the counts stay with no folder")
	}
	if len(w.fire) != 0 {
		t.Error("a watch started for an empty folder")
	}
}

func TestDiffSummarySameFolderDoesNotRestartTheWatch(t *testing.T) {
	q := &uiQueue{}
	w := &fakeWatch{}
	var mu sync.Mutex
	reads := 0
	d := &DiffSummary{
		Read: func(string) (review.Summary, error) {
			mu.Lock()
			reads++
			mu.Unlock()
			return review.Summary{Added: 1, Files: 1}, nil
		},
		Watch: w.watch, Post: q.post,
	}
	d.SetDir("/repo")
	q.wait(t, 1)
	d.SetDir("/repo")
	d.SetDir("/repo")
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if reads != 1 {
		t.Errorf("%d reads for one folder, want 1", reads)
	}
	d.Stop()
}

// The real watch reads again when a branch moves, in a real scratch repository.
func TestWatchBranchesFiresOnACommit(t *testing.T) {
	dir := scratchRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fired := make(chan struct{}, 8)
	go WatchBranches(ctx, dir, func() { fired <- struct{}{} })
	select {
	case <-fired: // the first read, at start
	case <-time.After(5 * time.Second):
		t.Fatal("no first read")
	}
	time.Sleep(300 * time.Millisecond) // let the watch settle
	runGit(t, dir, "checkout", "-b", "feature")
	runGit(t, dir, "commit", "--allow-empty", "-m", "work")
	select {
	case <-fired:
	case <-time.After(5 * time.Second):
		t.Fatal("a new branch did not fire the watch")
	}
}

func TestKitDiffSummaryUsesGreysNotDiffColors(t *testing.T) {
	// The summary must not draw the green and red of the diff view (docs/spec/DiffSummary.md).
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Background(theme.Current().Ground).Children(func() {
			kit.DiffSummary(c, kit.DiffSummaryOpts{Added: 12, Removed: 3, Files: 2})
		})
	}, 300, 40)
	tt.Frame()
	img := tt.Image()
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if d := int(g>>8) - int(r>>8); d > 24 || -d > 24 { // a hue: green or red over grey
				t.Fatalf("pixel %d,%d is not grey: %d %d %d", x, y, r>>8, g>>8, bl>>8)
			}
		}
	}
}
