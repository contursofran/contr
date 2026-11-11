package files

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/review"
)

func diffTester(d *DiffSummary, open func()) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Background(theme.Current().Ground).Children(func() { d.View(c, open) })
	}, 600, 40)
}

func TestDiffSummaryShowsTheCountsAndOpensReviewOnAClick(t *testing.T) {
	opened := 0
	tt := diffTester(ReadyDiff(review.Summary{Added: 120, Removed: 126, Files: 9}), func() { opened++ })
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
	tt := diffTester(ReadyDiff(review.Summary{}), nil)
	if len(tt.Texts()) != 0 {
		t.Errorf("a project with no change shows %q", tt.Texts())
	}
}

func TestDiffSummaryHidesTheRemovedCountWithOnlyAdditions(t *testing.T) {
	tt := diffTester(ReadyDiff(review.Summary{Added: 4, Files: 1}), nil)
	if !tt.HasText("+4") || !tt.HasText("1 file") || tt.HasText("−0") {
		t.Errorf("texts %q, want +4 and 1 file only", tt.Texts())
	}
	if _, ok := tt.Find("4 lines added, 1 file. Open review"); !ok {
		t.Errorf("label missing; texts %q", tt.Texts())
	}
}

func TestDiffSummaryHidesBeforeTheFirstReadWithNoFolderAndAfterAReadError(t *testing.T) {
	for name, d := range map[string]*DiffSummary{
		"nil":          nil,
		"no tree":      {},
		"no folder":    {Tree: &FileTree{}},
		"not read yet": {Tree: &FileTree{dir: "/x", changes: []review.File{{Path: "a", Added: 1}}}},
	} {
		if _, ok := d.Summary(); ok {
			t.Errorf("%s: the summary shows", name)
		}
	}
	// A git error keeps the tree's old list but the summary hides.
	dir := t.TempDir()
	ft := NewFileTree(func() string { return dir })
	ft.Debounce = -1
	ft.Watch = func(string, func()) (func(), error) { return func() {}, nil }
	ft.Env.Run.Go = func(fn func()) { fn() }
	ft.Sync()
	ft.changesOK = false
	ft.fail(errors.New("git: boom"))
	if _, ok := (&DiffSummary{Tree: ft}).Summary(); ok {
		t.Error("the summary shows after a failed read")
	}
}

// TestDiffSummaryAndFileTreeAgreeOnAWorkTreeWithASecondAgentsBranch is the rule of D131: the
// title bar counts what the tree lists, for the selected agent only. The repository has a
// committed change, an uncommitted change and an untracked file in the work tree, and a second
// agent's branch with its own commit that must not count.
func TestDiffSummaryAndFileTreeAgreeOnAWorkTreeWithASecondAgentsBranch(t *testing.T) {
	s := apptest.NewRepo(t)
	apptest.Write(t, s.Dir, "mod.go", "1\n2\n3\n")
	apptest.Write(t, s.Dir, "edit.go", "a\nb\n")
	s.Git("add", "-A")
	s.Git("commit", "-m", "files")
	// The second agent: its own worktree and branch, one commit ahead of main.
	other := filepath.Join(t.TempDir(), "other")
	s.Git("worktree", "add", "-b", "agent/other", other)
	apptest.Write(t, other, "theirs.go", "x\ny\nz\nw\nv\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-m", "theirs")
	// The selected agent: a committed change, an uncommitted one and an untracked file.
	s.Git("checkout", "-b", "feature")
	apptest.Write(t, s.Dir, "mod.go", "1\n2\n3\n4\n5\n6\n") // committed: +3
	s.Git("add", "-A")
	s.Git("commit", "-m", "mod")
	apptest.Write(t, s.Dir, "edit.go", "a\n")           // uncommitted: −1
	apptest.Write(t, s.Dir, "fresh.go", "a\nb\nc\nd\n") // untracked: +4

	r := newTreeRig(t, s.Dir)
	r.frame()
	d := &DiffSummary{Tree: r.ft}
	got, ok := d.Summary()
	if !ok {
		t.Fatal("the summary is not ready after the tree read")
	}
	if want := (review.Summary{Added: 7, Removed: 1, Files: 3}); got != want {
		t.Errorf("summary = %+v, want %+v (the second agent's file must not count)", got, want)
	}
	if r.has("theirs.go") || r.has("theirs.go, +5") {
		t.Error("the tree lists the second agent's file")
	}
	// What the tree draws adds up to the same totals.
	var tree review.Summary
	for _, row := range []struct {
		label        string
		added, rmved int
	}{{"mod.go, +3 −0", 3, 0}, {"edit.go, +0 −1", 0, 1}, {"fresh.go, +4", 4, 0}} {
		if !r.has(row.label) {
			t.Errorf("the tree has no row %q; texts %q", row.label, r.tt.Texts())
			continue
		}
		tree.Added += row.added
		tree.Removed += row.rmved
		tree.Files++
	}
	if tree != got {
		t.Errorf("the tree rows add up to %+v, the title bar shows %+v", tree, got)
	}
	// The button's own label carries the same numbers.
	tt := diffTester(d, nil)
	if _, ok := tt.Find("7 lines added, 1 removed, 3 files. Open review"); !ok {
		t.Errorf("title bar label wrong; texts %q", tt.Texts())
	}
	// A change in the work tree and a new read: both move together.
	apptest.Write(t, s.Dir, "fresh.go", "a\n")
	r.notify()
	r.frame()
	if got, _ := d.Summary(); got != (review.Summary{Added: 4, Removed: 1, Files: 3}) {
		t.Errorf("after an edit the summary is %+v", got)
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
