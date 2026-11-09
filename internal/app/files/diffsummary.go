package files

// The title bar's DiffSummary (docs/spec/DiffSummary.md): the changed lines and files of the
// selected agent. It owns no git read and no watch. It counts what the file tree lists (D131):
// the work tree against the merge base, plus the new files. The tree reads on a goroutine and
// watches the work tree, so the two totals always agree. Its methods run on the UI thread.

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/review"
)

// DiffSummary shows the totals of a [FileTree].
type DiffSummary struct {
	Tree *FileTree
}

// Summary is the total of the files that the tree lists. ok is false until the tree has read
// its folder, and while it has an error: the button hides then.
func (d *DiffSummary) Summary() (sum review.Summary, ok bool) {
	if d == nil || d.Tree == nil {
		return review.Summary{}, false
	}
	return d.Tree.Summary()
}

// View builds the button. open runs on a click.
func (d *DiffSummary) View(c *ui.Context, open func()) {
	sum, ok := d.Summary()
	if !ok {
		return
	}
	kit.DiffSummary(c, kit.DiffSummaryOpts{Added: sum.Added, Removed: sum.Removed, Files: sum.Files, OnClick: open})
}

// ReadyDiff is a DiffSummary over a tree that has read sum.Files files; the first one carries
// the counts. It runs no read. It is for the tests of the frame and the title bar.
func ReadyDiff(sum review.Summary) *DiffSummary {
	t := &FileTree{dir: "/x", changesOK: true}
	for i := 0; i < sum.Files; i++ {
		f := review.File{Path: "f" + strconv.Itoa(i), Status: review.Modified}
		if i == 0 {
			f.Added, f.Removed = sum.Added, sum.Removed
		}
		t.changes = append(t.changes, f)
	}
	return &DiffSummary{Tree: t}
}
