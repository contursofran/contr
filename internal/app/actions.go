// Adapted from Herdr UI (Apache-2.0); changes: ported to Go; only the app menu's actions and
// keys (Quit, Hide, Hide Others, Minimize, About, Copy, Paste, Select All, New Terminal, Close Terminal); fixed macOS keystrokes
// instead of a keymap catalog; no config, palette, daemon or update actions.

package app

import (
	"sync/atomic"

	"github.com/egoist/mygo"

	"github.com/contursofran/contr/internal/app/shortcut"
)

// The shortcut of each menu item, from the one list of shortcuts (#640, D245). The menus show
// these labels, so a menu, its key, the tooltips and the key hints cannot drift apart.
var (
	keyQuit          = shortcut.Of(shortcut.Quit).Accelerator()
	keyHide          = shortcut.Of(shortcut.Hide).Accelerator()
	keyHideOthers    = shortcut.Of(shortcut.HideOthers).Accelerator()
	keyMinimize      = shortcut.Of(shortcut.Minimize).Accelerator()
	keyCopy          = shortcut.Of(shortcut.Copy).Accelerator()
	keyPaste         = shortcut.Of(shortcut.Paste).Accelerator()
	keySelectAll     = shortcut.Of(shortcut.SelectAll).Accelerator()
	keyNewTerminal   = shortcut.Of(shortcut.NewTab).Accelerator()
	keyCloseTerminal = shortcut.Of(shortcut.CloseTab).Accelerator()
	keySettings      = shortcut.Of(shortcut.Settings).Accelerator()
	keySidebar       = shortcut.Of(shortcut.Sidebar).Accelerator()
)

// action is one item of the app menus: the MyGo role that does the work, and its shortcut.
// MyGo roles run Quit, Hide, Hide Others, Minimize and About natively, so there is no handler
// to register. The Edit items have no Click either: the focused view handles Copy, Paste and
// Select All with its own key handler, and a second handler would run them twice.
type action struct {
	label string
	role  mygo.MenuRole
	key   string
}

var (
	actionAbout      = action{"About contr", mygo.RoleAbout, ""}
	actionHide       = action{"Hide contr", mygo.RoleHide, keyHide}
	actionHideOthers = action{"Hide Others", mygo.RoleHideOthers, keyHideOthers}
	actionQuit       = action{"Quit contr", mygo.RoleQuit, keyQuit}
	actionMinimize   = action{"Minimize", mygo.RoleMinimize, keyMinimize}
	actionCopy       = action{"Copy", mygo.RoleCopy, keyCopy}
	actionPaste      = action{"Paste", mygo.RolePaste, keyPaste}
	actionSelectAll  = action{"Select All", mygo.RoleSelectAll, keySelectAll}
	// New Terminal has no role: its Click runs the frame's handler.
	actionNewTerminal = action{"New Terminal", "", keyNewTerminal}
	// Close Terminal has no role either: it closes a plain terminal only.
	actionCloseTerminal = action{"Close Terminal", "", keyCloseTerminal}
	// Settings has no role: its Click opens the Settings modal (D115).
	actionSettings = action{"Settings…", "", keySettings}
	// Toggle Sidebar has no role: its Click shows or hides the sidebar (the title bar has no button for it).
	actionSidebar = action{"Toggle Sidebar", "", keySidebar}
)

// item makes the menu item of the action.
func (a action) item() *mygo.MenuItem {
	return &mygo.MenuItem{Label: a.label, Role: a.role, Accelerator: a.key}
}

// handlerID names a menu item whose Click runs a handler of the open window.
type handlerID int

const (
	handlerNewTerminal handlerID = iota
	handlerCloseTerminal
	handlerSettings
	handlerSidebar
	handlerCount
)

// handlerActions is the menu action of each handler.
var handlerActions = [handlerCount]action{
	handlerNewTerminal:   actionNewTerminal,
	handlerCloseTerminal: actionCloseTerminal,
	handlerSettings:      actionSettings,
	handlerSidebar:       actionSidebar,
}

// handlers holds the open window's handler of each item. The menus are built
// before any window exists, so the items call through this. The frame sets
// the handlers in Attach and clears them when it stops.
var handlers [handlerCount]atomic.Pointer[func()]

// setHandler sets the handler of id; nil clears it.
func setHandler(id handlerID, fn func()) {
	if fn == nil {
		handlers[id].Store(nil)
		return
	}
	handlers[id].Store(&fn)
}

// handlerItem is the menu item of id. Its Click runs on the main thread and
// does nothing when no handler is set.
func handlerItem(id handlerID) *mygo.MenuItem {
	it := handlerActions[id].item()
	it.Click = func(*mygo.MenuItem, *mygo.Window) {
		if fn := handlers[id].Load(); fn != nil {
			(*fn)()
		}
	}
	return it
}

// InitActions installs the menu bar. Call it once, before the first window opens.
func InitActions() {
	InstallMenus()
}
