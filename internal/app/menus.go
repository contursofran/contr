package app

import "github.com/egoist/mygo"

// BuildMenus returns the menu bar in order: the app menu, File, Edit, View and Window. macOS titles the
// first one with the app name. Each item gets its shortcut from its action.
func BuildMenus() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "contr", Submenu: []*mygo.MenuItem{
			actionAbout.item(),
			mygo.Separator(),
			handlerItem(handlerSettings),
			mygo.Separator(),
			actionHide.item(),
			actionHideOthers.item(),
			mygo.Separator(),
			actionQuit.item(),
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			handlerItem(handlerNewTerminal),
			handlerItem(handlerCloseTerminal),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			actionCopy.item(),
			actionPaste.item(),
			actionSelectAll.item(),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			handlerItem(handlerSidebar),
		}},
		{Label: "Window", Submenu: []*mygo.MenuItem{
			actionMinimize.item(),
		}},
	})
}

// InstallMenus replaces the menu bar with the menus from BuildMenus.
func InstallMenus() {
	mygo.App.SetMenu(BuildMenus())
}
