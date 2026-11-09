package app

import (
	"errors"
	"testing"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/host"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/usage"
)

func TestStoreAtUsesThePath(t *testing.T) {
	var loaded, saved string
	s := StoreAt(func() (string, error) { return "/x/v.json", nil },
		func(p string) (int, error) { loaded = p; return 7, nil },
		func(p string, v int) error { saved = p; return nil })
	if v, err := s.Load(); err != nil || v != 7 || loaded != "/x/v.json" {
		t.Fatalf("Load = %d, %v from %q", v, err, loaded)
	}
	if err := s.Save(3); err != nil || saved != "/x/v.json" {
		t.Fatalf("Save = %v to %q", err, saved)
	}
}

func TestStoreAtPathErrorStopsLoadAndSave(t *testing.T) {
	bad := errors.New("no config folder")
	calls := 0
	s := StoreAt(func() (string, error) { return "", bad },
		func(string) (int, error) { calls++; return 7, nil },
		func(string, int) error { calls++; return nil })
	if v, err := s.Load(); !errors.Is(err, bad) || v != 0 {
		t.Fatalf("Load = %d, %v; want 0, %v", v, err, bad)
	}
	if err := s.Save(3); !errors.Is(err, bad) {
		t.Fatalf("Save = %v; want %v", err, bad)
	}
	if calls != 0 {
		t.Fatalf("load or save ran %d times with no path", calls)
	}
}

// The zero Deps gives a frame with no stores and no agents; the Deps that NewFrame takes
// reach the actions that Attach makes.
func TestNewFrameTakesTheDeps(t *testing.T) {
	attach := func(d Deps) *Frame {
		f := NewFrame(d)
		f.Host = host.New(func() (host.Stats, error) { return host.Stats{}, nil }, theme.MemBarWidth, nil)
		f.Usage = usage.NewSampler(usage.Unread, nil)
		t.Cleanup(f.Attach(&fakeWindow{visible: true}))
		return f
	}
	f := attach(Deps{})
	if f.NewOrch != nil || f.NewTerm != nil || f.SpacesList.HoldLaunch {
		t.Fatalf("zero Deps: new orch %v, new term %v, hold launch %v", f.NewOrch, f.NewTerm, f.SpacesList.HoldLaunch)
	}
	f = attach(Deps{
		Projects:          Store[*projects.Store]{Load: func() (*projects.Store, error) { return nil, nil }},
		PickFolder:        func() (string, error) { return "", ErrPickCancelled },
		SpawnOrchestrator: func(string) error { return nil },
		OpenTerminal:      func(string, string) (string, error) { return "", nil },
	})
	if f.NewOrch == nil || f.NewTerm == nil || !f.SpacesList.HoldLaunch {
		t.Fatalf("full Deps: new orch %v, new term %v, hold launch %v", f.NewOrch, f.NewTerm, f.SpacesList.HoldLaunch)
	}
}
