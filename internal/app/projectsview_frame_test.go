package app

// The tests of the hide toast of the projects modal, which the frame draws.

import (
	"testing"

	"slices"

	"github.com/contursofran/contr/internal/app/projectsview"
)

func TestHideToastOfTheFrameRunsUndoOnClick(t *testing.T) {
	f, tt := toastFrame()
	ran := false
	f.Toasts.ShowAction("b hidden from the sidebar", projectsview.ProjectsUndo, func() { ran = true })
	tt.Frame()
	tt.Frame()
	if !slices.Contains(tt.Texts(), "b hidden from the sidebar") {
		t.Fatalf("no toast in %q", tt.Texts())
	}
	if err := tt.Click(projectsview.ProjectsUndo); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("Undo did not run")
	}
}

func TestHideToastsGetUniqueIDs(t *testing.T) {
	f, tt := toastFrame()
	f.Toasts.ShowAction("b hidden from the sidebar", projectsview.ProjectsUndo, func() {})
	f.Toasts.ShowAction("b hidden from the sidebar", projectsview.ProjectsUndo, func() {})
	a := f.Toasts.TakeActions()
	if len(a) != 2 || a[0].ID == a[1].ID {
		t.Errorf("ids = %v", a)
	}
	_ = tt
}
