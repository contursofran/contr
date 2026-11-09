package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// The progress line has the track `hairline` and the bar `ink-62` (spec).
func TestCloneProgressThemeColours(t *testing.T) {
	p := theme.Current()
	base := &ui.Theme{Border: p.InputLine, Accent: p.Ink100}
	got := progressTheme(base, p)
	if got.Border != p.Hairline || got.Accent != p.Ink62 {
		t.Errorf("track %v bar %v, want %v and %v", got.Border, got.Accent, p.Hairline, p.Ink62)
	}
	if base.Border != p.InputLine || base.Accent != p.Ink100 {
		t.Error("the base theme changed")
	}
}

func TestCloneRowStaysOnTopWhileTheFieldChanges(t *testing.T) {
	var calls [][]string
	r, parent := cloneRig(t, okRunner(&calls))
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	r.m.query = "a" // the owner edits the field: it filters the open rows
	r.tt.Frame()
	r.tt.Frame()
	if !r.shows("Cloning acme/widget") || !r.has(ProjectsCancel) || !r.has(LabelCloneProgress) || !r.has(ProjectsRow("a")) {
		t.Fatalf("no clone row above the matches: %v", r.tt.Texts())
	}
	if clone, open := r.rect(t, LabelCloneRow), r.rect(t, ProjectsRow("a")); clone.Y >= open.Y {
		t.Errorf("clone row at y %v is not above the open row at y %v", clone.Y, open.Y)
	}
	if !r.shows("into " + parent + "/widget") {
		t.Errorf("no destination of the running clone: %v", r.tt.Texts())
	}
	// The clone ends: the field holds other text, so it stays.
	r.held[0]()
	if r.m.query != "a" || len(r.store.Folders()) != 1 || r.m.cloning {
		t.Errorf("query %q folders %v cloning %v", r.m.query, r.store.Folders(), r.m.cloning)
	}
}

func TestCloneEndClearsTheFieldOnlyIfUnchanged(t *testing.T) {
	r, _ := cloneRig(t, okRunner(new([][]string)))
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	r.held[0]()
	if r.m.query != "" {
		t.Errorf("the field kept %q", r.m.query)
	}
}

func TestCloneFailureAfterAnEditGoesToStatus(t *testing.T) {
	r, _ := cloneRig(t, func(context.Context, string, ...string) (string, error) { return "", errors.New("disk full") })
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	r.m.query = "acme/other"
	r.held[0]()
	if r.m.cloneErr != "" || len(r.status) != 1 || !strings.Contains(r.status[0], "acme/widget") || !strings.Contains(r.status[0], "disk full") {
		t.Errorf("err %q status %v", r.m.cloneErr, r.status)
	}
}
