package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/shortcut"
)

// windowKeys runs the window shortcuts of the list (package shortcut) that are not menu items.
// The menu items (⌘T, ⌘W, ⌘, and ⌘B) take their keys before the window. ⌘F is in findView, the
// layer that holds the find bar. A key that the terminal needs is not in the list. win is the
// window width. UI thread.
func (f *Frame) windowKeys(c *ui.Context, win float32) {
	// ⌘J opens or closes the questions card from the notch (D125, D147).
	if f.Notch != nil && shortcut.Of(shortcut.Questions).Pressed(c) {
		f.Notch.Toggle()
	}
	if shortcut.Of(shortcut.Chat).Pressed(c) {
		f.toggleChat()
	}
	if shortcut.Of(shortcut.Files).Pressed(c) && !narrowWindow(win) {
		f.TogglePanel()
	}
	// While the projects modal shows, ⌘O is its own (it opens the folder dialog): a window
	// shortcut built behind a modal waits.
	if f.ProjectsModal != nil && shortcut.Of(shortcut.OpenProject).Pressed(c) {
		f.ProjectsModal.Toggle()
	}
	if f.NewOrch != nil && shortcut.Of(shortcut.NewOrchestrator).Pressed(c) {
		f.NewOrch.Start()
	}
	if f.SpacesList != nil {
		if shortcut.Of(shortcut.PrevAgent).Pressed(c) {
			f.SpacesList.StepAgent(-1)
		}
		if shortcut.Of(shortcut.NextAgent).Pressed(c) {
			f.SpacesList.StepAgent(1)
		}
	}
	if shortcut.Of(shortcut.PrevTab).Pressed(c) {
		f.stepTab(-1)
	}
	if shortcut.Of(shortcut.NextTab).Pressed(c) {
		f.stepTab(1)
	}
	for _, s := range shortcut.TabKeys() {
		if s.Pressed(c) {
			f.pickTab(s.ID)
		}
	}
}

// pickTab selects the tab that the shortcut id stands for (⌘1 to ⌘8, ⌘9 the last), in the
// visible order of the title bar (D190). A number past the last tab does nothing.
func (f *Frame) pickTab(id shortcut.ID) {
	tabs := f.titleTabs()
	if i, ok := shortcut.TabIndex(id, len(tabs)); ok {
		f.selectTab(tabs[i])
	}
}

// stepTab selects the tab before (d -1) or after (d 1) the selected tab, around the ends
// (⌘⇧[, ⌘⇧]). With no tab selected, it selects the first or the last tab.
func (f *Frame) stepTab(d int) {
	tabs := f.titleTabs()
	n := len(tabs)
	if n == 0 {
		return
	}
	at := -1
	for i, t := range tabs {
		if t.Selected {
			at = i
		}
	}
	next := (at + d + n) % n
	if at < 0 {
		next = 0
		if d < 0 {
			next = n - 1
		}
	}
	f.selectTab(tabs[next])
}

// keyHints reads the state of ⌘ for this frame and gives the parts that show key chips whether
// they show (docs/spec/KeyHints.md, D245).
func (f *Frame) keyHints(c *ui.Context) {
	on := f.hints.Frame(c)
	f.bar.Hints = on
	if f.SpacesList != nil {
		f.SpacesList.Hints = on
	}
	if f.Notch != nil {
		f.Notch.Hints = on
	}
	if f.Chat != nil {
		f.Chat.Hints = on
	}
}

// usedKey runs fn, the action of a menu item, and hides the key chips until ⌘ is released: the
// menu takes its key before the window sees it.
func (f *Frame) usedKey(fn func()) func() {
	return func() {
		f.hints.Used()
		fn()
	}
}
