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

// TestSpecSheet draws the files panel's tree on Changed (D264) for scripts/sheet.sh (D179).
func TestSpecSheet(t *testing.T) {
	tree := sheetTree()
	apptest.SpecSheet(t, []apptest.SheetPart{{Name: "FileTree", W: 282, H: 380, View: func(c *ui.Context) {
		kit.Panel(c, kit.PanelOpts{Name: "Files", Head: func() { tree.Head(c) }, Body: func() { tree.View(c) }})
	}}}, nil)
}
