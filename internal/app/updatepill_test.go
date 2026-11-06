package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type pillRig struct {
	f        *Frame
	tt       *ui.Tester
	u        *fakeUpdates
	relaunch int
}

// newPillRig is a frame whose Settings runs its work at once, with a pending update of 1.3.0.
func newPillRig(t *testing.T, installErr error) *pillRig {
	t.Helper()
	f, tt := toastFrame()
	r := &pillRig{f: f, tt: tt}
	r.u = &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0", Notes: "## 1.3.0\n- Update pill\n- Faster tabs"}, installErr: installErr}
	f.Settings = &Settings{Updates: r.u, Version: "1.2.3", Run: Runner{Go: func(fn func()) { fn() }},
		Relaunch: func() { r.relaunch++ }, OnReady: f.updateReady}
	tt.Frame()
	return r
}

func (r *pillRig) ready() {
	r.f.Settings.autoCheck()
	r.tt.Frame()
	r.tt.Frame()
}

func (r *pillRig) has(label string) bool {
	_, ok := r.tt.Find(label)
	return ok
}

func (r *pillRig) settle() {
	for range 30 {
		r.tt.Advance(50 * time.Millisecond)
	}
}

func TestNoUpdatePillWithoutAnUpdate(t *testing.T) {
	r := newPillRig(t, nil)
	if r.has(LabelUpdatePill) || r.has(LabelUpdateToast) {
		t.Fatal("the pill shows without an update")
	}
}

func TestAReadyUpdateShowsThePillAndOneToast(t *testing.T) {
	r := newPillRig(t, nil)
	r.ready()
	if !r.has(LabelUpdatePill) {
		t.Fatal("no pill when the update is ready")
	}
	if !r.has(LabelUpdateToast) || !slices.Contains(r.tt.Texts(), "contr 1.3.0 is ready") ||
		!slices.Contains(r.tt.Texts(), UpdateToastBody) {
		t.Fatalf("no update toast: %q", r.tt.Texts())
	}
	pill, toast := box(t, r.tt, LabelUpdatePill), box(t, r.tt, LabelUpdateToast)
	if toast.Y < pill.Y+pill.H {
		t.Errorf("the toast %v is not under the pill %v", toast, pill)
	}
}

func TestTheUpdateToastDoesNotComeBackAfterLater(t *testing.T) {
	r := newPillRig(t, nil)
	r.ready()
	if err := r.tt.Click(UpdateLater); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if r.has(LabelUpdateToast) {
		t.Fatal("the toast stays after Later")
	}
	r.f.updateReady(UpdateInfo{Version: "1.3.0"})
	r.tt.Frame()
	r.settle()
	if r.has(LabelUpdateToast) {
		t.Error("the toast came back for the same version")
	}
	if !r.has(LabelUpdatePill) {
		t.Error("the pill went away with the toast")
	}
}

func TestThePillOpensThePanelAndRestartNowRelaunches(t *testing.T) {
	r := newPillRig(t, nil)
	r.ready()
	r.tt.Frame()
	if err := r.tt.Click(LabelUpdatePill); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if !r.has(LabelUpdatePanel) {
		t.Fatal("the pill does not open the panel")
	}
	texts := r.tt.Texts()
	for _, want := range []string{"contr 1.3.0", "from 1.2.3", UpdateWhatsNew, "Update pill", "Faster tabs", UpdateAgentsNote, "Downloaded just now"} {
		if !slices.Contains(texts, want) {
			t.Errorf("the panel has no %q: %q", want, texts)
		}
	}
	if err := r.tt.Click(SettingsRestart + " ⏎"); err != nil {
		t.Fatal(err)
	}
	if r.relaunch != 1 {
		t.Errorf("relaunch = %d after Restart now", r.relaunch)
	}
}

func TestReturnInThePanelRestarts(t *testing.T) {
	r := newPillRig(t, nil)
	r.ready()
	r.f.upd.open = true
	r.tt.Frame()
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.relaunch != 1 {
		t.Errorf("relaunch = %d after Return", r.relaunch)
	}
}

func TestAFailedDownloadShowsNoPill(t *testing.T) {
	r := newPillRig(t, errors.New("offline"))
	r.ready()
	if r.has(LabelUpdatePill) || r.has(LabelUpdateToast) {
		t.Fatal("a failed download shows the pill or the toast")
	}
	if !r.f.Settings.Ready() {
		t.Error("the Settings dot went away after a failed download")
	}
}

func TestDownloadedText(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{10 * time.Second, "Downloaded just now"}, {3 * time.Minute, "Downloaded 3 min ago"}, {2 * time.Hour, "Downloaded 2 h ago"}} {
		if got := downloadedText(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("%v ago: %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestNoteLinesListsItemsWithoutHeadings(t *testing.T) {
	got := noteLines("# 1.3.0\n\n- one\n* two\nplain")
	if !slices.Equal(got, []string{"one", "two"}) {
		t.Errorf("noteLines = %q", got)
	}
	if got := noteLines("just text\n"); !slices.Equal(got, []string{"just text"}) {
		t.Errorf("plain notes = %q", got)
	}
}
