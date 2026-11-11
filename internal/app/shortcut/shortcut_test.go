package shortcut

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestEveryShortcutHasAUniqueIDAndKeyAndUsesCmd(t *testing.T) {
	ids := map[ID]bool{}
	keys := map[[2]int]ID{}
	for _, s := range All() {
		if ids[s.ID] {
			t.Errorf("id %q twice", s.ID)
		}
		ids[s.ID] = true
		k := [2]int{int(s.Mods), int(s.Key)}
		if other, ok := keys[k]; ok {
			t.Errorf("%q and %q have the same key", s.ID, other)
		}
		keys[k] = s.ID
		if s.Mods&ui.Cmd == 0 || s.Cap == "" {
			t.Errorf("%q: mods %v cap %q, want ⌘ and a cap", s.ID, s.Mods, s.Cap)
		}
	}
}

// The terminal needs ⌘C, ⌘V, ⌘A and ⌘K (and its font keys): only the Edit menu roles, which
// give the key to the focused view, have one of them.
func TestNoWindowShortcutTakesAKeyOfTheTerminal(t *testing.T) {
	edit := map[ID]bool{Copy: true, Paste: true, SelectAll: true}
	for _, s := range All() {
		if s.Mods != ui.Cmd || edit[s.ID] {
			continue
		}
		switch s.Key {
		case ui.KeyC, ui.KeyV, ui.KeyA, ui.KeyK, ui.KeyEqual, ui.KeyMinus, ui.Key0:
			t.Errorf("%q takes ⌘%s", s.ID, s.Cap)
		}
	}
}

func TestGlyphsHintAndAccelerator(t *testing.T) {
	for _, c := range []struct {
		id                  ID
		glyphs, hint, accel string
	}{
		{NewTab, "⌘T", "T", "Cmd+T"},
		{Settings, "⌘,", ",", "Cmd+,"},
		{Files, "⌘⇧E", "⇧E", "Shift+Cmd+E"},
		{HideOthers, "⌘⌥H", "⌥H", "Alt+Cmd+H"},
		{NextTab, "⌘⇧]", "⇧]", "Shift+Cmd+]"},
		{PrevAgent, "⌘⌥↑", "⌥↑", "Alt+Cmd+Up"},
		{NextAgent, "⌘⌥↓", "⌥↓", "Alt+Cmd+Down"},
		{PrevProject, "⌘⌥←", "⌥←", "Alt+Cmd+Left"},
		{NextProject, "⌘⌥→", "⌥→", "Alt+Cmd+Right"},
		{Tab1, "⌘1", "1", "Cmd+1"},
		{LastTab, "⌘9", "9", "Cmd+9"},
	} {
		s := Of(c.id)
		if s.Glyphs() != c.glyphs || s.Hint() != c.hint || s.Accelerator() != c.accel {
			t.Errorf("%q: %q %q %q, want %q %q %q", c.id, s.Glyphs(), s.Hint(), s.Accelerator(), c.glyphs, c.hint, c.accel)
		}
	}
	var none Shortcut
	if none.Glyphs() != "" || none.Hint() != "" || none.Accelerator() != "" {
		t.Error("the zero Shortcut has a key")
	}
	if _, ok := Get("nothing"); ok {
		t.Error("Get found an id not in the list")
	}
	if Of(NewTab).HintIf(false) != "" || Of(NewTab).HintIf(true) != "T" {
		t.Error("HintIf")
	}
}

// The tab keys and the tab chips agree: the key that TabShortcut gives tab i selects tab i.
func TestTabKeysAndTabIndexAgree(t *testing.T) {
	for n := 0; n <= 12; n++ {
		for i := range n {
			s, ok := TabShortcut(i, n)
			if !ok {
				if i < NumberedTabs || i == n-1 {
					t.Errorf("tab %d of %d has no key", i, n)
				}
				continue
			}
			if got, ok := TabIndex(s.ID, n); !ok || got != i {
				t.Errorf("tab %d of %d: key %q selects %d %v", i, n, s.ID, got, ok)
			}
		}
	}
	if keys := TabKeys(); len(keys) != 9 || keys[0].ID != Tab1 || keys[8].ID != LastTab {
		t.Errorf("TabKeys %v", keys)
	}
	if _, ok := TabIndex(Tab3, 2); ok {
		t.Error("⌘3 selects a tab of 2")
	}
	if i, ok := TabIndex(LastTab, 12); !ok || i != 11 {
		t.Errorf("⌘9 of 12 tabs: %d %v", i, ok)
	}
	if _, ok := TabIndex(LastTab, 0); ok {
		t.Error("⌘9 with no tabs")
	}
	if _, ok := TabIndex(NewTab, 3); ok {
		t.Error("⌘T selects a tab")
	}
	if _, ok := TabShortcut(-1, 3); ok {
		t.Error("tab -1 has a key")
	}
}

var t0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestHintsShowAfterTheDelayAndNotBefore(t *testing.T) {
	var h Hints
	show, wait := h.Step(ui.Cmd, false, true, t0)
	if show || wait != HintDelay {
		t.Fatalf("at ⌘ down: %v %v", show, wait)
	}
	if show, wait = h.Step(ui.Cmd, false, true, t0.Add(HintDelay-time.Millisecond)); show || wait != time.Millisecond {
		t.Fatalf("before the delay: %v %v", show, wait)
	}
	if show, wait = h.Step(ui.Cmd, false, true, t0.Add(HintDelay)); !show || wait != 0 {
		t.Fatalf("after the delay: %v %v", show, wait)
	}
}

func TestHintsHideOnReleaseAnotherKeyBlurAndUse(t *testing.T) {
	cases := map[string]func(h *Hints) (bool, time.Duration){
		"release":     func(h *Hints) (bool, time.Duration) { return h.Step(0, false, true, t0.Add(time.Second)) },
		"shift too":   func(h *Hints) (bool, time.Duration) { return h.Step(ui.Cmd|ui.Shift, false, true, t0.Add(time.Second)) },
		"another key": func(h *Hints) (bool, time.Duration) { return h.Step(ui.Cmd, true, true, t0.Add(time.Second)) },
		"blur":        func(h *Hints) (bool, time.Duration) { return h.Step(ui.Cmd, false, false, t0.Add(time.Second)) },
		"used": func(h *Hints) (bool, time.Duration) {
			h.Used()
			return h.Step(ui.Cmd, false, true, t0.Add(time.Second))
		},
	}
	for name, step := range cases {
		var h Hints
		h.Step(ui.Cmd, false, true, t0)
		if show, _ := h.Step(ui.Cmd, false, true, t0.Add(HintDelay)); !show {
			t.Fatalf("%s: no chips before", name)
		}
		if show, wait := step(&h); show || wait != 0 {
			t.Errorf("%s: %v %v, want hidden and no wait", name, show, wait)
		}
	}
}

// A ⌘C: the key comes before the delay, so nothing shows while ⌘ stays down, and no frame waits.
// A new press of ⌘ starts again.
func TestAKeyWithCmdShowsNothingUntilCmdIsReleased(t *testing.T) {
	var h Hints
	h.Step(ui.Cmd, false, true, t0)
	h.Step(ui.Cmd, true, true, t0.Add(50*time.Millisecond))
	if show, wait := h.Step(ui.Cmd, false, true, t0.Add(time.Second)); show || wait != 0 {
		t.Fatalf("⌘ still down after ⌘C: %v %v", show, wait)
	}
	h.Step(0, false, true, t0.Add(2*time.Second))
	h.Step(ui.Cmd, false, true, t0.Add(3*time.Second))
	if show, _ := h.Step(ui.Cmd, false, true, t0.Add(3*time.Second+HintDelay)); !show {
		t.Error("a new ⌘ press shows no chips")
	}
}

// The project keys and the project chips agree, as the tab keys do (D289).
func TestProjectKeysAndProjectIndexAgree(t *testing.T) {
	for n := 0; n <= 12; n++ {
		for i := range n {
			s, ok := ProjectShortcut(i, n)
			if !ok {
				if i < NumberedTabs || i == n-1 {
					t.Errorf("project %d of %d has no key", i, n)
				}
				continue
			}
			if got, ok := ProjectIndex(s.ID, n); !ok || got != i {
				t.Errorf("project %d of %d: key %q selects %d %v", i, n, s.ID, got, ok)
			}
		}
	}
	keys := ProjectKeys()
	if len(keys) != 9 || keys[0].ID != Project1 || keys[8].ID != LastProject {
		t.Fatalf("ProjectKeys %v", keys)
	}
	if keys[0].Glyphs() != "⌘⇧1" || keys[0].Hint() != "⇧1" || keys[8].Hint() != "⇧9" {
		t.Errorf("project key caps %q %q %q", keys[0].Glyphs(), keys[0].Hint(), keys[8].Hint())
	}
	if _, ok := ProjectIndex(Project5, 3); ok {
		t.Error("⌘⇧5 selects a project of 3")
	}
	if i, ok := ProjectIndex(LastProject, 3); !ok || i != 2 {
		t.Errorf("⌘⇧9 of 3 projects: %d %v", i, ok)
	}
	if _, ok := ProjectIndex(LastProject, 0); ok {
		t.Error("⌘⇧9 with no projects")
	}
	if _, ok := ProjectIndex(Tab1, 3); ok {
		t.Error("⌘1 selects a project")
	}
	if _, ok := ProjectShortcut(9, 12); ok {
		t.Error("project 9 of 12 has a key")
	}
}
