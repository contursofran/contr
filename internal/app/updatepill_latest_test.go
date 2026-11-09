package app

import (
	"slices"
	"sync"
	"testing"

	"github.com/contursofran/contr/internal/app/settings"
	"github.com/contursofran/contr/internal/updated"
)

// newerRig is a pill rig with 1.3.0 ready and its installs recorded through the frame (D209).
// The scheduled check then finds 1.4.0 (D226).
func newerRig(t *testing.T) (*pillRig, func() []updated.Note) {
	t.Helper()
	r := newPillRig(t, nil)
	var mu sync.Mutex
	var saved []updated.Note
	r.f.deps.Updated.Save = func(n updated.Note) error {
		mu.Lock()
		defer mu.Unlock()
		saved = append(saved, n)
		return nil
	}
	r.f.Settings.RecordInstall = r.f.recordInstall
	r.ready()
	r.u.Info = &settings.UpdateInfo{Version: "1.4.0", Notes: "## 1.4.0\n- Latest update"}
	return r, func() []updated.Note {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(saved)
	}
}

func TestANewerVersionWhileReadyShowsInThePillAndToast(t *testing.T) {
	r, saved := newerRig(t)
	r.f.Settings.AutoCheck()
	r.tt.Frame()
	r.tt.Frame()
	r.settle()
	texts := r.tt.Texts()
	if !slices.Contains(texts, "contr 1.4.0 is ready") || slices.Contains(texts, "contr 1.3.0 is ready") {
		t.Errorf("want only the 1.4.0 toast: %q", texts)
	}
	if err := r.tt.Click(LabelUpdatePill); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	texts = r.tt.Texts()
	for _, want := range []string{"contr 1.4.0", "from 1.2.3", "Latest update"} {
		if !slices.Contains(texts, want) {
			t.Errorf("the panel has no %q: %q", want, texts)
		}
	}
	var got []string
	for _, n := range saved() {
		got = append(got, n.Version+" from "+n.From)
	}
	if !slices.Equal(got, []string{"1.3.0 from 1.2.3", "1.4.0 from 1.2.3"}) {
		t.Errorf("updated.json notes %v", got)
	}
}

func TestRestartNowInThePanelInstallsTheNewerVersionFirst(t *testing.T) {
	r, saved := newerRig(t)
	if err := r.tt.Click(LabelUpdatePill); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.Click(settings.SettingsRestart + " ⏎"); err != nil {
		t.Fatal(err)
	}
	if r.relaunch != 1 || r.u.Installs != 2 {
		t.Errorf("relaunch %d, installs %d; want 1 and 2", r.relaunch, r.u.Installs)
	}
	if n := saved(); len(n) != 2 || n[1].Version != "1.4.0" || n[1].From != "1.2.3" {
		t.Errorf("updated.json notes %+v", n)
	}
}
