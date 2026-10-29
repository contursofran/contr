package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The view runs without a window: the tester builds it as the app would.
func TestFrameShowsName(t *testing.T) {
	tt := ui.NewTester(Frame, 1440, 900)
	if !tt.HasText("contr") {
		t.Errorf("frame texts %q, want one that is %q", tt.Texts(), "contr")
	}
}
