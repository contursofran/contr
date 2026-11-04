package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Keyboard focus shows the tip of an icon-only button at once.
func TestIconButtonShowsTipOnFocus(t *testing.T) {
	_, tt := newTester(t)
	const tip = "Show or hide the sidebar"
	if tt.HasText(tip) {
		t.Fatalf("tip shows before focus; texts %q", tt.Texts())
	}
	for i := 0; i < 6 && !tt.HasText(tip); i++ {
		tt.Key(0, ui.KeyTab)
	}
	if !tt.HasText(tip) {
		t.Errorf("no tip for the sidebar toggle; texts %q", tt.Texts())
	}
}
