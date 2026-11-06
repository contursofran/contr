package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Keyboard focus shows the tip of an icon-only button at once.
func TestIconButtonShowsTipOnFocus(t *testing.T) {
	_, tt := newTester(t)
	const tip = "Search  ⌘F"
	if tt.HasText(tip) {
		t.Fatalf("tip shows before focus; texts %q", tt.Texts())
	}
	for i := 0; i < 12 && !tt.HasText(tip); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.HasText(tip) {
		t.Errorf("no tip for the search button; texts %q", tt.Texts())
	}
}
