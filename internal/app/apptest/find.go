package apptest

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Box gives the rect of the element with label. It fails the test when there
// is none.
func Box(t *testing.T, tt *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no element %q; texts %q", label, tt.Texts())
	}
	return r
}

// Absent reports that no element has label.
func Absent(tt *ui.Tester, label string) bool {
	_, ok := tt.Find(label)
	return !ok
}
