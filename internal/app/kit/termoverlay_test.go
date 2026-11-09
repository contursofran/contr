package kit

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTerminalOverlayNamesFollowAddAndRemove(t *testing.T) {
	var o TerminalOverlay
	if !o.Empty() || len(o.Names()) != 0 {
		t.Fatalf("new overlay has layers %v", o.Names())
	}
	removeA := o.Add("a", func(*ui.Context) {})
	o.Add("b", func(*ui.Context) {})
	if got := o.Names(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("names %v, want [a b]", got)
	}
	removeA()
	if got := o.Names(); !slices.Equal(got, []string{"b"}) || o.Empty() {
		t.Errorf("names %v after a remove, want [b]", got)
	}
}
