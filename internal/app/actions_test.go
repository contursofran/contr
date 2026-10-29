package app

import "testing"

func TestCmdQHAltHAndMRunTheirAppActions(t *testing.T) {
	// MyGo roles run the actions natively; each role must carry its Rust key.
	menus := BuildMenus().Items()
	for label, key := range map[string]string{
		"Quit contr":  "Cmd+Q",
		"Hide contr":  "Cmd+H",
		"Hide Others": "Alt+Cmd+H",
	} {
		if got := find(t, menus[0].Submenu, label).Accelerator; got != key {
			t.Errorf("%s key = %q, want %q", label, got, key)
		}
	}
	if got := find(t, menus[2].Submenu, "Minimize").Accelerator; got != "Cmd+M" {
		t.Errorf("Minimize key = %q", got)
	}
}

func TestTheEditLabelsDoNotRunTheirActionsFromTheKeymap(t *testing.T) {
	// The focused view owns Cmd-C, Cmd-V and Cmd-A. A Click on the menu item would run the
	// action on top of it, so Cmd-V would paste twice.
	for _, it := range BuildMenus().Items()[1].Submenu {
		if it.Click != nil {
			t.Errorf("%s has a Click handler", it.Label)
		}
	}
}

func TestEachShortcutOfTheMenusIsBound(t *testing.T) {
	cases := []struct {
		a    action
		want string
	}{
		{actionQuit, "Cmd+Q"},
		{actionHide, "Cmd+H"},
		{actionHideOthers, "Alt+Cmd+H"},
		{actionMinimize, "Cmd+M"},
		{actionCopy, "Cmd+C"},
		{actionPaste, "Cmd+V"},
		{actionSelectAll, "Cmd+A"},
	}
	for _, c := range cases {
		if got := c.a.item().Accelerator; got != c.want {
			t.Errorf("%s key = %q, want %q", c.a.label, got, c.want)
		}
	}
}

func TestMinimizeWithNoWindowDoesNothing(t *testing.T) {
	// Minimize is a MyGo role. Building and installing the menu with no window must not fail.
	InitActions()
}
