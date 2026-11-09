package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// itemNames lists the label of each item, with "-" for a separator.
func itemNames(items []*mygo.MenuItem) []string {
	var names []string
	for _, it := range items {
		if it.Type == mygo.MenuItemSeparator {
			names = append(names, "-")
		} else {
			names = append(names, it.Label)
		}
	}
	return names
}

// find returns the item with this label.
func find(t *testing.T, items []*mygo.MenuItem, label string) *mygo.MenuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("no item %q", label)
	return nil
}

func TestTheAppMenuHasAboutHideHideOthersAndQuit(t *testing.T) {
	menus := BuildMenus().Items()
	app := menus[0]
	if app.Label != "contr" {
		t.Fatalf("name = %q", app.Label)
	}
	want := []string{"About contr", "-", "Settings…", "-", "Hide contr", "Hide Others", "-", "Quit contr"}
	if got := itemNames(app.Submenu); !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for label, role := range map[string]mygo.MenuRole{
		"About contr": mygo.RoleAbout,
		"Hide contr":  mygo.RoleHide,
		"Hide Others": mygo.RoleHideOthers,
		"Quit contr":  mygo.RoleQuit,
	} {
		if got := find(t, app.Submenu, label).Role; got != role {
			t.Errorf("%s role = %q, want %q", label, got, role)
		}
	}
}

func TestTheEditMenuHasCopyPasteAndSelectAllAsOsActions(t *testing.T) {
	edit := BuildMenus().Items()[2]
	if edit.Label != "Edit" {
		t.Fatalf("name = %q", edit.Label)
	}
	want := []string{"Copy", "Paste", "Select All"}
	if got := itemNames(edit.Submenu); !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	for label, role := range map[string]mygo.MenuRole{
		"Copy":       mygo.RoleCopy,
		"Paste":      mygo.RolePaste,
		"Select All": mygo.RoleSelectAll,
	} {
		if got := find(t, edit.Submenu, label).Role; got != role {
			t.Errorf("%s role = %q, want %q", label, got, role)
		}
	}
}

func TestTheWindowMenuHasMinimize(t *testing.T) {
	window := BuildMenus().Items()[4]
	if window.Label != "Window" {
		t.Fatalf("name = %q", window.Label)
	}
	if got := itemNames(window.Submenu); !slices.Equal(got, []string{"Minimize"}) {
		t.Fatalf("items = %v", got)
	}
	if got := find(t, window.Submenu, "Minimize").Role; got != mygo.RoleMinimize {
		t.Errorf("role = %q", got)
	}
}

func TestInstallPutsTheMenusInTheMenuBar(t *testing.T) {
	// Before the app runs, SetMenu only stores the menu, so this must not panic.
	InstallMenus()
	InitActions()
	var names []string
	for _, m := range BuildMenus().Items() {
		names = append(names, m.Label)
	}
	if want := []string{"contr", "File", "Edit", "View", "Window"}; !slices.Equal(names, want) {
		t.Fatalf("menus = %v, want %v", names, want)
	}
}

func TestTheFileMenuHasNewTerminalOnCmdT(t *testing.T) {
	file := BuildMenus().Items()[1]
	if file.Label != "File" {
		t.Fatalf("name = %q", file.Label)
	}
	if got := itemNames(file.Submenu); !slices.Equal(got, []string{"New Terminal", "Close Terminal"}) {
		t.Fatalf("items = %v", got)
	}
	it := find(t, file.Submenu, "New Terminal")
	if it.Accelerator != "Cmd+T" || it.Click == nil {
		t.Errorf("accelerator %q, click set %v", it.Accelerator, it.Click != nil)
	}
}

func TestNewTerminalItemCallsTheFramesHandlerAndDoesNothingWithoutOne(t *testing.T) {
	it := find(t, BuildMenus().Items()[1].Submenu, "New Terminal")
	it.Click(it, nil) // no window: must not panic
	n := 0
	setHandler(handlerNewTerminal, func() { n++ })
	defer setHandler(handlerNewTerminal, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran %d times, want 1", n)
	}
	setHandler(handlerNewTerminal, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran after it was cleared")
	}
}

func TestTheFileMenuHasCloseTerminalOnCmdW(t *testing.T) {
	it := find(t, BuildMenus().Items()[1].Submenu, "Close Terminal")
	if it.Accelerator != "Cmd+W" || it.Click == nil {
		t.Errorf("accelerator %q, click set %v", it.Accelerator, it.Click != nil)
	}
}

func TestCloseTerminalItemCallsTheFramesHandlerAndDoesNothingWithoutOne(t *testing.T) {
	it := find(t, BuildMenus().Items()[1].Submenu, "Close Terminal")
	it.Click(it, nil) // no window: must not panic
	n := 0
	setHandler(handlerCloseTerminal, func() { n++ })
	defer setHandler(handlerCloseTerminal, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran %d times, want 1", n)
	}
	setHandler(handlerCloseTerminal, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran after it was cleared")
	}
}

// The projects modal opens from the Spaces header pill only (D113): no menu
// item and no shortcut.
func TestNoProjectsItemOrShortcutInTheMenus(t *testing.T) {
	for _, m := range BuildMenus().Items() {
		for _, it := range m.Submenu {
			if strings.HasPrefix(it.Label, "Projects") || it.Accelerator == "Shift+Cmd+P" {
				t.Errorf("menu %s has %q (%q)", m.Label, it.Label, it.Accelerator)
			}
		}
	}
}

func TestTheAppMenuHasSettingsOnCmdCommaAndCallsTheFramesHandler(t *testing.T) {
	it := find(t, BuildMenus().Items()[0].Submenu, "Settings…")
	if it.Accelerator != "Cmd+," || it.Click == nil {
		t.Fatalf("accelerator %q, click set %v", it.Accelerator, it.Click != nil)
	}
	it.Click(it, nil) // no window: must not panic
	n := 0
	setHandler(handlerSettings, func() { n++ })
	defer setHandler(handlerSettings, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran %d times, want 1", n)
	}
	setHandler(handlerSettings, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran after it was cleared")
	}
}

func TestTheViewMenuHasToggleSidebarOnCmdB(t *testing.T) {
	view := BuildMenus().Items()[3]
	if view.Label != "View" {
		t.Fatalf("name = %q", view.Label)
	}
	it := find(t, view.Submenu, "Toggle Sidebar")
	if it.Accelerator != "Cmd+B" || it.Click == nil {
		t.Errorf("accelerator %q, click set %v", it.Accelerator, it.Click != nil)
	}
	it.Click(it, nil) // no window: must not panic
	n := 0
	setHandler(handlerSidebar, func() { n++ })
	defer setHandler(handlerSidebar, nil)
	it.Click(it, nil)
	if n != 1 {
		t.Errorf("handler ran %d times, want 1", n)
	}
}

// Each item of the handler table runs its own handler only.
func TestEachHandlerItemRunsOnlyItsOwnHandler(t *testing.T) {
	var ran [handlerCount]int
	for id := range handlerCount {
		setHandler(id, func() { ran[id]++ })
		defer setHandler(id, nil)
	}
	for id := range handlerCount {
		it := handlerItem(id)
		if it.Label != handlerActions[id].label {
			t.Errorf("item %d label %q, want %q", id, it.Label, handlerActions[id].label)
		}
		it.Click(it, nil)
		for other := range handlerCount {
			want := 0
			if other <= id {
				want = 1
			}
			if ran[other] != want {
				t.Errorf("after item %d: handler %d ran %d times, want %d", id, other, ran[other], want)
			}
		}
	}
}
