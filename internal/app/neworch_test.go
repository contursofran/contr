package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/sidebar"
	"github.com/contursofran/contr/internal/app/view"
)

// newOrchHarness shows the spaces column with `+ new` wired to a fake picker
// and a fake spawner. Runner is empty, so work runs at once.
type newOrchHarness struct {
	h      *spacesRig
	no     *NewOrchestrator
	picked int
	spawn  []string
	status []string
	pickFn func() (string, error)
	spawnE error
}

func newOrch(t *testing.T) *newOrchHarness {
	t.Helper()
	n := &newOrchHarness{h: newSpaces(t, apptest.SpTree(apptest.SpPane("w1:p1", t.TempDir(), nil)))}
	n.pickFn = func() (string, error) { return "/work/proj", nil }
	n.no = &NewOrchestrator{
		Pick:  func() (string, error) { n.picked++; return n.pickFn() },
		Spawn: func(dir string) error { n.spawn = append(n.spawn, dir); return n.spawnE },
		Env:   view.Env{Status: func(s string) { n.status = append(n.status, s) }},
	}
	n.h.list.OnNew = n.no.Start
	n.h.tt.Frame()
	return n
}

func TestPlusNewPicksAFolderAndSpawns(t *testing.T) {
	n := newOrch(t)
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if n.picked != 1 || len(n.spawn) != 1 || n.spawn[0] != "/work/proj" {
		t.Errorf("picked %d, spawn %q", n.picked, n.spawn)
	}
	if len(n.status) != 0 {
		t.Errorf("no message on success, got %q", n.status)
	}
}

func TestPlusNewCancelledPickerSpawnsNothing(t *testing.T) {
	n := newOrch(t)
	n.pickFn = func() (string, error) { return "", projectsview.ErrPickCancelled }
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if n.picked != 1 || len(n.spawn) != 0 || len(n.status) != 0 {
		t.Errorf("cancel: picked %d, spawn %q, status %q", n.picked, n.spawn, n.status)
	}
}

func TestPlusNewShowsASpawnError(t *testing.T) {
	n := newOrch(t)
	n.spawnE = errors.New(`no "orchestrator" profile in agents.json`)
	if err := addItem(n.h.tt, sidebar.LabelProjectsNew); err != nil {
		t.Fatal(err)
	}
	if len(n.status) != 1 || !strings.Contains(n.status[0], "agents.json") {
		t.Errorf("status %q", n.status)
	}
}

func TestPlusNewShowsAPickerError(t *testing.T) {
	n := newOrch(t)
	n.pickFn = func() (string, error) { return "", errors.New("dialog failed") }
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(n.spawn) != 0 || len(n.status) != 1 || !strings.Contains(n.status[0], "dialog failed") {
		t.Errorf("spawn %q, status %q", n.spawn, n.status)
	}
}

func TestPlusNewIgnoresAClickWhileOneRuns(t *testing.T) {
	n := newOrch(t)
	var queued []func()
	n.no.Env.Run = view.Runner{Go: func(f func()) { queued = append(queued, f) }}
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(queued) != 1 {
		t.Fatalf("queued %d, want 1", len(queued))
	}
	queued[0]()
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if len(queued) != 2 {
		t.Errorf("after it ends a click starts again; queued %d", len(queued))
	}
}

func TestPlusNewRunsOffTheCallingThread(t *testing.T) {
	n := newOrch(t)
	var queued []func()
	n.no.Env.Run = view.Runner{Go: func(f func()) { queued = append(queued, f) }}
	_ = addItem(n.h.tt, sidebar.LabelProjectsNew)
	if n.picked != 0 {
		t.Error("the picker must not run in the click handler")
	}
}

func TestStartInSpawnsWithoutPicker(t *testing.T) {
	n := newOrch(t)
	n.no.StartIn("/proj/gamma")
	if n.picked != 0 || len(n.spawn) != 1 || n.spawn[0] != "/proj/gamma" {
		t.Errorf("picked %d, spawn %v", n.picked, n.spawn)
	}
}

func TestStartInShowsSpawnError(t *testing.T) {
	n := newOrch(t)
	n.spawnE = errors.New("no profile")
	n.no.StartIn("/proj/gamma")
	if len(n.status) != 1 || !strings.Contains(n.status[0], "no profile") {
		t.Errorf("status %v", n.status)
	}
}
