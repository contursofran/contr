package app

import (
	"context"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/prefs"
	"github.com/contursofran/contr/internal/setup"
)

// setupLaunch attaches a frame whose stored prefs are stored and whose setup pass gives rs,
// waits for the first pass and returns the frame and the prefs it saved.
func setupLaunch(t *testing.T, stored prefs.Prefs, rs []setup.Result) (*Frame, chan prefs.Prefs) {
	t.Helper()
	f, _ := themeFrame(t, func() (prefs.Prefs, error) { return stored, nil })
	saved := make(chan prefs.Prefs, 8)
	f.deps.Prefs.Save = func(v prefs.Prefs) error { saved <- v; return nil }
	ran := make(chan struct{}, 8)
	f.deps.Setup = &setup.Env{}
	f.deps.SetupRun = func(context.Context, setup.Env) []setup.Result { ran <- struct{}{}; return rs }
	f.StartTheme()
	win := &fakeWindow{visible: true}
	stop := f.Attach(win)
	t.Cleanup(stop)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("no setup pass at start")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && f.Settings.SetupSteps() == 0 && len(rs) > 0; {
		win.flush()
		time.Sleep(5 * time.Millisecond)
	}
	win.flush()
	return f, saved
}

var notReady = []setup.Result{{ID: setup.IDHC, Name: "hc", Required: true, State: setup.Missing}}

func TestFirstLaunchOpensSetupOnceAndStoresIt(t *testing.T) {
	f, saved := setupLaunch(t, prefs.Prefs{}, notReady)
	if !f.Settings.Opened() {
		t.Fatal("Settings did not open on the first launch with a step left")
	}
	select {
	case p := <-saved:
		if !p.SetupSeen {
			t.Errorf("saved SetupSeen = false, want true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SetupSeen not saved")
	}
	f.Settings.Close()
	f.firstSetup(1)
	if f.Settings.Opened() {
		t.Error("Settings opened a second time")
	}
}

func TestSetupDoesNotOpenWhenSeen(t *testing.T) {
	f, _ := setupLaunch(t, prefs.Prefs{SetupSeen: true}, notReady)
	if f.Settings.Opened() {
		t.Error("Settings opened though SetupSeen is set")
	}
	if !f.Settings.NeedsDot() {
		t.Error("no dot on the Settings entry after the start pass")
	}
}

func TestSetupDoesNotOpenWhenAllReady(t *testing.T) {
	f, saved := setupLaunch(t, prefs.Prefs{}, []setup.Result{{ID: setup.IDHC, Required: true, State: setup.Ready}})
	time.Sleep(20 * time.Millisecond)
	if f.Settings.Opened() {
		t.Error("Settings opened though every check is ready")
	}
	select {
	case p := <-saved:
		t.Errorf("saved %+v, want no save", p)
	default:
	}
}
