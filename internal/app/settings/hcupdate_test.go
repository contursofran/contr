package settings

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// fakeHC returns an HCRunner that writes out and returns err, and counts its runs.
func fakeHC(out string, err error, runs *int) HCRunner {
	return func(context.Context) ([]byte, error) {
		*runs++
		return []byte(out), err
	}
}

// updateHC opens the Updates tab with r.s.HCUpdate set to run, and clicks "Update hc".
func updateHC(t *testing.T, run HCRunner) *settingsRig {
	t.Helper()
	r := newSettingsRig(t, nil)
	r.s.HCUpdate = run
	r.s.CoreVersion = func() (string, bool) { return "0.1.18", true }
	r.openUpdates()
	if err := r.tt.Click(SettingsHCUpdate); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	return r
}

func TestUpdateHCShowsNewVersion(t *testing.T) {
	runs := 0
	r := updateHC(t, fakeHC(`{"now":"hc 0.1.19","restarted":true,"was":"hc 0.1.18"}`, nil, &runs))
	want := "hc 0.1.19 installed: the core restarted on it, terminals kept running"
	if runs != 1 || !slices.Contains(r.texts(), want) {
		t.Fatalf("runs %d, texts %q", runs, r.texts())
	}
}

func TestUpdateHCUpToDate(t *testing.T) {
	runs := 0
	r := updateHC(t, fakeHC(`{"now":"hc 0.1.18","release":"0.1.18","updated":false}`, nil, &runs))
	if !slices.Contains(r.texts(), TextHCUpToDate) {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestUpdateHCShowsError(t *testing.T) {
	runs := 0
	r := updateHC(t, fakeHC(`{"error":{"kind":"net","message":"download failed: 404"}}`, errors.New("exit status 1"), &runs))
	if !slices.Contains(r.texts(), "hc update failed: download failed: 404") {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestUpdateHCTimeout(t *testing.T) {
	old := HCUpdateTimeout
	HCUpdateTimeout = 20 * time.Millisecond
	t.Cleanup(func() { HCUpdateTimeout = old })
	r := updateHC(t, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done() // the fake hangs until the time limit
		return nil, ctx.Err()
	})
	if !slices.Contains(r.texts(), "hc update failed: no answer after "+HCUpdateTimeout.String()) {
		t.Fatalf("texts %q", r.texts())
	}
}

func TestUpdateHCBusyAndHidden(t *testing.T) {
	r := newSettingsRig(t, nil)
	r.openUpdates()
	if r.has(SettingsHCUpdate) {
		t.Fatal("Update hc shows with no runner")
	}
	runs := 0
	r.s.HCUpdate = fakeHC(`{}`, nil, &runs)
	r.s.hc.busy = true
	r.s.UpdateHC()
	if runs != 0 {
		t.Fatal("UpdateHC ran while busy")
	}
}

func TestHCOutcomeOtherErrors(t *testing.T) {
	if _, msg := hcOutcome(nil, errors.New("hc: program not found")); msg != "hc: program not found" {
		t.Fatal(msg)
	}
	if _, msg := hcOutcome([]byte("garbage"), nil); msg != "unexpected output: garbage" {
		t.Fatal(msg)
	}
	if line, _ := hcOutcome([]byte(`{"now":"hc 2","was":"hc 1"}`), nil); line != "hc 2 installed: restart the core to use it" {
		t.Fatal(line)
	}
}

func TestUpdateHCShowsProgressOffUIThread(t *testing.T) {
	r := newSettingsRig(t, nil)
	var held func()
	r.s.Env.Run.Go = func(fn func()) { held = fn }
	runs := 0
	r.s.HCUpdate = fakeHC(`{"now":"hc 0.1.18","updated":false}`, nil, &runs)
	r.openUpdates()
	if err := r.tt.Click(SettingsHCUpdate); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if runs != 0 || !slices.Contains(r.texts(), TextHCUpdating) {
		t.Fatalf("runs %d, texts %q", runs, r.texts())
	}
	held()
	r.tt.Frame()
	if !slices.Contains(r.texts(), TextHCUpToDate) {
		t.Fatalf("texts %q", r.texts())
	}
}
