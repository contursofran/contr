// Package app holds the content of contr's window. For now it is the empty
// frame of milestone 0; docs/PORT-PLAN.md lists the views that come next.
package app

import "github.com/egoist/mygo/ui"

// Frame is the window's view: one centered line until the frame milestone
// adds the sidebar, title bar, terminal, board panel and status bar.
func Frame(c *ui.Context) {
	ui.Column(c).Fill().Center().Children(func() {
		ui.Text(c, "contr")
	})
}
