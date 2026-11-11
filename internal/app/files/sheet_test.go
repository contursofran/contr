package files

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/kit"
	"github.com/contursofran/contr/internal/review"
)

// sheetTree is a tree that has read the folder of the R6 board: changed files
// in three folders and the root, one of them deleted. It runs no read.
func sheetTree() *FileTree {
	t := &FileTree{dir: "/p", changesOK: true, Dir: func() string { return "/p" }}
	t.setChanges([]review.File{
		{Path: "internal/app/panel.go", Status: review.Modified, Added: 38, Removed: 12},
		{Path: "internal/app/sidebar.go", Status: review.Modified, Added: 24, Removed: 9},
		{Path: "internal/app/tabs.go", Status: review.Added, Added: 31},
		{Path: "internal/app/topbar.go", Status: review.Deleted, Removed: 52},
		{Path: "internal/hc/events.go", Status: review.Modified, Added: 9, Removed: 4},
		{Path: "internal/hc/events_test.go", Status: review.Added, Added: 7},
		{Path: "docs/spec/AgentRow.md", Status: review.Modified, Added: 11, Removed: 15},
		{Path: "HANDOFF.md", Status: review.Deleted, Removed: 34},
	})
	t.loaded = map[string][]review.Entry{"": {{Name: "docs", Dir: true}, {Name: "internal", Dir: true}, {Name: "README.md"}}}
	t.loading, t.opened, t.openFiltered = map[string]bool{}, map[string]*bool{}, map[string]*bool{}
	return t
}

// sheetLongTree is sheetTree with three nested worktrees that git lists as
// untracked folders, so the Changed list is taller than the part (#693).
func sheetLongTree() *FileTree {
	t := sheetTree()
	list := append([]review.File(nil), t.changes...)
	for _, id := range []string{"a004bd4e7f1c9e5", "b19f2c06d3a8e21", "c7e3d9a2b5f4c10"} {
		list = append(list, review.File{Path: ".claude/worktrees/agent-" + id + "/", Status: review.Added, Binary: true})
	}
	t.setChanges(list)
	return t
}

// TestSpecSheet draws the files panel's tree on Changed (D264) for scripts/sheet.sh (D179), and
// FileTreeLong: a Changed list longer than the panel, with untracked folders (#693).
func TestSpecSheet(t *testing.T) {
	tree, long := sheetTree(), sheetLongTree()
	panel := func(tree *FileTree) func(c *ui.Context) {
		return func(c *ui.Context) {
			kit.Panel(c, kit.PanelOpts{Name: "Files", Head: func() { tree.Head(c) }, Body: func() { tree.View(c) }})
		}
	}
	apptest.SpecSheet(t, []apptest.SheetPart{
		{Name: "FileTree", W: 282, H: 380, View: panel(tree)},
		{Name: "FileTreeLong", W: 282, H: 380, View: panel(long)},
	}, []string{"FileTreeLong"})
}
