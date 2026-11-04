// Adapted from Herdr UI (Apache-2.0); changes: ported to Go; only the app menu's actions and
// keys (Quit, Hide, Hide Others, Minimize, About, Copy, Paste, Select All, New Terminal); fixed macOS keystrokes
// instead of a keymap catalog; no config, palette, daemon or update actions.

package app

import (
	"sync/atomic"

	"github.com/egoist/mygo"
)

// The shortcut of each menu item. The menus show these labels, so a menu and its key cannot
// drift apart.
const (
	keyQuit        = "Cmd+Q"
	keyHide        = "Cmd+H"
	keyHideOthers  = "Alt+Cmd+H"
	keyMinimize    = "Cmd+M"
	keyCopy        = "Cmd+C"
	keyPaste       = "Cmd+V"
	keySelectAll   = "Cmd+A"
	keyNewTerminal = "Cmd+T"
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
)

// item makes the menu item of the action.
func (a action) item() *mygo.MenuItem {
	return &mygo.MenuItem{Label: a.label, Role: a.role, Accelerator: a.key}
}

// newTerminalHandler is the open window's "new terminal" action. The menus
// are built before any window exists, so the item calls through this. The
// frame sets it in Attach and clears it when it stops.
var newTerminalHandler atomic.Pointer[func()]

func setNewTerminalHandler(fn func()) {
	if fn == nil {
		newTerminalHandler.Store(nil)
		return
	}
	newTerminalHandler.Store(&fn)
}

// newTerminalItem is the New Terminal item. Its Click runs on the main thread.
func newTerminalItem() *mygo.MenuItem {
	it := actionNewTerminal.item()
	it.Click = func(*mygo.MenuItem, *mygo.Window) {
		if fn := newTerminalHandler.Load(); fn != nil {
			(*fn)()
		}
	}
	return it
}

// InitActions installs the menu bar. Call it once, before the first window opens.
func InitActions() {
	InstallMenus()
}
