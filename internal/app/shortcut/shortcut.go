// Package shortcut is the one list of the app's keyboard shortcuts (#640, D245). Each entry has
// an id, its modifiers and key, and the key cap that the tooltips, the menus and the key hints
// show. The handlers, the menus, the tooltips and the hints all read this list, so they cannot
// disagree.
package shortcut

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// ID names a shortcut.
type ID string

// The shortcuts. All use ⌘. The terminal keeps ⌘C, ⌘V, ⌘A and ⌘K (and its font keys ⌘=, ⌘-
// and ⌘0): no window shortcut takes them.
const (
	Quit       ID = "quit"
	Hide       ID = "hide"
	HideOthers ID = "hide-others"
	Minimize   ID = "minimize"
	Copy       ID = "copy"
	Paste      ID = "paste"
	SelectAll  ID = "select-all"
	// NewTab opens a plain terminal (D82). File menu item.
	NewTab ID = "new-tab"
	// CloseTab closes the selected plain terminal tab (D85). File menu item.
	CloseTab ID = "close-tab"
	// Settings opens the Settings modal (D115). App menu item.
	Settings ID = "settings"
	// Sidebar shows or hides the sidebar (D172). View menu item.
	Sidebar ID = "sidebar"
	// Files shows or hides the files panel. The issue asked for ⌘⌥B; ⌘⇧E was there first and stays.
	Files ID = "files"
	// Find opens the find bar over the terminal (D130).
	Find ID = "find"
	// Questions opens or closes the questions card (D125).
	Questions ID = "questions"
	// OpenProject opens the projects modal. Inside the modal ⌘O opens the folder dialog.
	OpenProject ID = "open-project"
	// NewOrchestrator starts a new orchestrator: the folder dialog, then the spawn.
	NewOrchestrator ID = "new-orchestrator"
	PrevTab         ID = "prev-tab"
	NextTab         ID = "next-tab"
	// PrevAgent and NextAgent select the agent row above or below in the sidebar.
	PrevAgent ID = "prev-agent"
	NextAgent ID = "next-agent"
	// PrevProject and NextProject select the project before or after the open one in the sidebar,
	// around the ends.
	PrevProject ID = "prev-project"
	NextProject ID = "next-project"
	// Tab1 to Tab8 select that tab of the title bar; LastTab selects the last tab.
	Tab1    ID = "tab-1"
	Tab2    ID = "tab-2"
	Tab3    ID = "tab-3"
	Tab4    ID = "tab-4"
	Tab5    ID = "tab-5"
	Tab6    ID = "tab-6"
	Tab7    ID = "tab-7"
	Tab8    ID = "tab-8"
	LastTab ID = "last-tab"
)

// Shortcut is one key of the list.
type Shortcut struct {
	ID   ID
	Mods ui.Modifiers
	Key  ui.Key
	// Cap is the key as its cap shows it: "T", ",", "[", "↑", "1".
	Cap string
}

// NumberedTabs is the count of tabs that have a key of their own (⌘1 to ⌘8). ⌘9 is the last tab.
const NumberedTabs = 8

var list = []Shortcut{
	{Quit, ui.Cmd, ui.KeyQ, "Q"},
	{Hide, ui.Cmd, ui.KeyH, "H"},
	{HideOthers, ui.Cmd | ui.Alt, ui.KeyH, "H"},
	{Minimize, ui.Cmd, ui.KeyM, "M"},
	{Copy, ui.Cmd, ui.KeyC, "C"},
	{Paste, ui.Cmd, ui.KeyV, "V"},
	{SelectAll, ui.Cmd, ui.KeyA, "A"},
	{NewTab, ui.Cmd, ui.KeyT, "T"},
	{CloseTab, ui.Cmd, ui.KeyW, "W"},
	{Settings, ui.Cmd, ui.KeyComma, ","},
	{Sidebar, ui.Cmd, ui.KeyB, "B"},
	{Files, ui.Cmd | ui.Shift, ui.KeyE, "E"},
	{Find, ui.Cmd, ui.KeyF, "F"},
	{Questions, ui.Cmd, ui.KeyJ, "J"},
	{OpenProject, ui.Cmd, ui.KeyO, "O"},
	{NewOrchestrator, ui.Cmd, ui.KeyN, "N"},
	{PrevTab, ui.Cmd | ui.Shift, ui.KeyBracketLeft, "["},
	{NextTab, ui.Cmd | ui.Shift, ui.KeyBracketRight, "]"},
	{PrevAgent, ui.Cmd | ui.Alt, ui.KeyUp, "↑"},
	{NextAgent, ui.Cmd | ui.Alt, ui.KeyDown, "↓"},
	{PrevProject, ui.Cmd | ui.Alt, ui.KeyLeft, "←"},
	{NextProject, ui.Cmd | ui.Alt, ui.KeyRight, "→"},
	{Tab1, ui.Cmd, ui.Key1, "1"},
	{Tab2, ui.Cmd, ui.Key2, "2"},
	{Tab3, ui.Cmd, ui.Key3, "3"},
	{Tab4, ui.Cmd, ui.Key4, "4"},
	{Tab5, ui.Cmd, ui.Key5, "5"},
	{Tab6, ui.Cmd, ui.Key6, "6"},
	{Tab7, ui.Cmd, ui.Key7, "7"},
	{Tab8, ui.Cmd, ui.Key8, "8"},
	{LastTab, ui.Cmd, ui.Key9, "9"},
}

// All returns a copy of the list.
func All() []Shortcut { return append([]Shortcut(nil), list...) }

// Get returns the shortcut of id, and false for an id not in the list.
func Get(id ID) (Shortcut, bool) {
	for _, s := range list {
		if s.ID == id {
			return s, true
		}
	}
	return Shortcut{}, false
}

// Of returns the shortcut of id; the zero Shortcut for an id not in the list.
func Of(id ID) Shortcut {
	s, _ := Get(id)
	return s
}

// Pressed reports the shortcut pressed in the window (c.Shortcut). The zero Shortcut is never pressed.
func (s Shortcut) Pressed(c *ui.Context) bool {
	return s.Key != ui.KeyUnknown && c.Shortcut(s.Mods, s.Key)
}

// mods returns the modifier glyphs in the app's order: ⌘, then ⌥, ⌃ and ⇧.
func (s Shortcut) mods(withCmd bool) string {
	var b strings.Builder
	if withCmd && s.Mods&ui.Cmd != 0 {
		b.WriteString("⌘")
	}
	if s.Mods&ui.Alt != 0 {
		b.WriteString("⌥")
	}
	if s.Mods&ui.Ctrl != 0 {
		b.WriteString("⌃")
	}
	if s.Mods&ui.Shift != 0 {
		b.WriteString("⇧")
	}
	return b.String()
}

// Glyphs is the whole key as a tooltip shows it: "⌘T", "⌘⇧E". Empty for the zero Shortcut.
func (s Shortcut) Glyphs() string {
	if s.Cap == "" {
		return ""
	}
	return s.mods(true) + s.Cap
}

// Hint is the text of the key chip while ⌘ is held: the key without ⌘, as "T", "⇧]", "⌥↑".
func (s Shortcut) Hint() string {
	if s.Cap == "" {
		return ""
	}
	return s.mods(false) + s.Cap
}

// HintIf is Hint while on, else "".
func (s Shortcut) HintIf(on bool) string {
	if !on {
		return ""
	}
	return s.Hint()
}

// accelNames are the menu names of the caps that are not a character (mygo menus: Accelerators).
var accelNames = map[string]string{"↑": "Up", "↓": "Down", "←": "Left", "→": "Right", "+": "Plus"}

// Accelerator is the key as a menu item takes it, modifiers in the macOS order: "Cmd+T",
// "Alt+Cmd+H". Empty for the zero Shortcut.
func (s Shortcut) Accelerator() string {
	if s.Cap == "" {
		return ""
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Cmd, "Cmd"}} {
		if s.Mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	key := s.Cap
	if n, ok := accelNames[key]; ok {
		key = n
	}
	return strings.Join(append(parts, key), "+")
}

// tabIDs are the ids of the numbered tabs, in order.
var tabIDs = [NumberedTabs]ID{Tab1, Tab2, Tab3, Tab4, Tab5, Tab6, Tab7, Tab8}

// TabKeys are the tab shortcuts in the order the checks run: ⌘1 to ⌘8, then ⌘9.
func TabKeys() []Shortcut {
	out := make([]Shortcut, 0, NumberedTabs+1)
	for _, id := range tabIDs {
		out = append(out, Of(id))
	}
	return append(out, Of(LastTab))
}

// TabIndex is the index of the tab that s selects among n tabs, in the visible order of the
// title bar (D190), and false when s selects none: a numbered tab past the last, or no tabs.
func TabIndex(s ID, n int) (int, bool) {
	if n <= 0 {
		return 0, false
	}
	if s == LastTab {
		return n - 1, true
	}
	for i, id := range tabIDs {
		if id == s {
			return i, i < n
		}
	}
	return 0, false
}

// TabShortcut is the shortcut that selects tab i of n: ⌘1 to ⌘8 for the first eight, ⌘9 for the
// last tab past them. false: the tab has no key.
func TabShortcut(i, n int) (Shortcut, bool) {
	switch {
	case i < 0 || i >= n:
		return Shortcut{}, false
	case i < NumberedTabs:
		return Of(tabIDs[i]), true
	case i == n-1:
		return Of(LastTab), true
	}
	return Shortcut{}, false
}
