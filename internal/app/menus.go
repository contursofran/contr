package app

import "github.com/egoist/mygo"

// BuildMenus returns the menu bar in order: the app menu, File, Edit and Window. macOS titles the
// first one with the app name. Each item gets its shortcut from its action.
func BuildMenus() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "contr", Submenu: []*mygo.MenuItem{
			actionAbout.item(),
			mygo.Separator(),
			settingsItem(),
			mygo.Separator(),
			actionHide.item(),
			actionHideOthers.item(),
			mygo.Separator(),
			actionQuit.item(),
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			newTerminalItem(),
			closeTerminalItem(),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			actionCopy.item(),
			actionPaste.item(),
			actionSelectAll.item(),
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
