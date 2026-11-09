package app

import (
	"slices"
	"testing"
	"time"
)

// toastCall is what the modal gave to its Toast hook.
type toastCall struct {
	msg, action string
	undo        func()
}

func hideRig(t *testing.T) (*modalRig, *[]toastCall) {
	t.Helper()
	r := newModalRig(t)
	var calls []toastCall
	r.m.Toast = func(msg, action string, undo func()) { calls = append(calls, toastCall{msg, action, undo}) }
	return r, &calls
}

func TestHideShowsToastWithUndo(t *testing.T) {
	r, calls := hideRig(t)
	r.open()
	r.m.hide("/w/b/.git")
	if len(*calls) != 1 {
		t.Fatalf("toasts: %d", len(*calls))
	}
	if c := (*calls)[0]; c.msg != "b hidden from the sidebar" || c.action != "Undo" || c.undo == nil {
		t.Errorf("toast = %q %q", c.msg, c.action)
	}
}

func TestHideWithoutStoreNoToast(t *testing.T) {
	r, calls := hideRig(t)
	r.m.Store = nil
	r.m.hide("/w/b/.git")
	if len(*calls) != 0 {
		t.Error("a toast shows with no store")
	}
}

func TestHidePutsTheProjectFirstInRecent(t *testing.T) {
	r, _ := hideRig(t)
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	r.m.Now = func() time.Time { return at }
	r.m.hide("/w/b/.git")
	rec := r.store.Recent()
	if len(rec) == 0 || rec[0].Dir != "/w/b/.git" || !rec[0].At.Equal(at) {
		t.Fatalf("recent = %v", rec)
	}
	if got := r.m.recent(); len(got) == 0 || got[0].Dir != "/w/b/.git" {
		t.Errorf("modal recent = %v", got)
	}
}

func TestUndoRestoresProjectAndOrder(t *testing.T) {
	r, calls := hideRig(t)
	r.store.SetOrder([]string{"/w/c/.git", "/w/a/.git", "/w/b/.git"})
	r.m.hide("/w/a/.git")
	if !r.store.Hidden("/w/a/.git") {
		t.Fatal("not hidden")
	}
	// A reorder after the hide stays; a goes back after c.
	r.store.SetOrder([]string{"/w/b/.git", "/w/c/.git"})
	saves := len(r.saved)
	(*calls)[0].undo()
	if r.store.Hidden("/w/a/.git") {
		t.Error("still hidden")
	}
	if got, want := r.names(), []string{"b", "c", "a"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if len(r.saved) != saves+1 || r.saved[len(r.saved)-1].Hidden("/w/a/.git") {
		t.Errorf("undo did not save: %d saves", len(r.saved))
	}
}

func TestUndoPutsTheProjectAtItsIndexWhenBothNeighboursAreGone(t *testing.T) {
	r, calls := hideRig(t)
	r.store.SetOrder([]string{"/w/a/.git", "/w/b/.git", "/w/c/.git"})
	r.m.hide("/w/b/.git")
	r.store.SetOrder([]string{"/w/d/.git"}) // a and c are gone
	r.dirs = []string{"/w/b/.git", "/w/d/.git"}
	(*calls)[0].undo()
	if got, want := r.names(), []string{"d", "b"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestTwoHidesAndAnOutOfOrderUndo(t *testing.T) {
	r, calls := hideRig(t)
	r.store.SetOrder([]string{"/w/a/.git", "/w/b/.git", "/w/c/.git"})
	r.m.hide("/w/a/.git")
	r.m.hide("/w/b/.git")
	(*calls)[0].undo() // a first: b stays hidden
	if !r.store.Hidden("/w/b/.git") || r.store.Hidden("/w/a/.git") {
		t.Fatal("the first Undo touched b")
	}
	(*calls)[1].undo()
	// b had no previous neighbour, so it goes before its next one, c.
	if got, want := r.names(), []string{"a", "b", "c"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestUndoRunsOnce(t *testing.T) {
	r, calls := hideRig(t)
	r.m.hide("/w/a/.git")
	(*calls)[0].undo()
	n := len(r.saved)
	r.m.hide("/w/b/.git") // another hide after the first Undo
	(*calls)[0].undo()    // a second press of the first Undo
	if len(r.saved) != n+1 {
		t.Errorf("a second Undo saved again: %d saves", len(r.saved))
	}
}

func TestUndoAfterModalClosed(t *testing.T) {
	r, calls := hideRig(t)
	r.open()
	before := r.names()
	r.m.hide("/w/b/.git")
	r.m.Close()
	(*calls)[0].undo()
	if r.store.Hidden("/w/b/.git") || !slices.Equal(r.names(), before) {
		t.Errorf("undo after close: %v, want %v", r.names(), before)
	}
}

func TestHideToastOfTheFrameRunsUndoOnClick(t *testing.T) {
	f, tt := toastFrame()
	ran := false
	f.Toasts.ShowAction("b hidden from the sidebar", ProjectsUndo, func() { ran = true })
	tt.Frame()
	tt.Frame()
	if !slices.Contains(tt.Texts(), "b hidden from the sidebar") {
		t.Fatalf("no toast in %q", tt.Texts())
	}
	if err := tt.Click(ProjectsUndo); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("Undo did not run")
	}
}

func TestHideToastsGetUniqueIDs(t *testing.T) {
	f, tt := toastFrame()
	f.Toasts.ShowAction("b hidden from the sidebar", ProjectsUndo, func() {})
	f.Toasts.ShowAction("b hidden from the sidebar", ProjectsUndo, func() {})
	a := f.Toasts.TakeActions()
	if len(a) != 2 || a[0].ID == a[1].ID {
		t.Errorf("ids = %v", a)
	}
	_ = tt
}

func TestUndoGoesBeforeTheNextNeighbourWhenThePreviousIsGone(t *testing.T) {
	r, calls := hideRig(t)
	r.store.SetOrder([]string{"/w/a/.git", "/w/b/.git", "/w/c/.git"})
	r.m.hide("/w/b/.git")
	r.store.SetOrder([]string{"/w/c/.git"}) // a is gone
	r.dirs = []string{"/w/b/.git", "/w/c/.git"}
	(*calls)[0].undo()
	if got, want := r.names(), []string{"b", "c"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
