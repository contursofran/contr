package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/updated"
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

func TestSinceText(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{{10 * time.Second, "Downloaded just now"}, {3 * time.Minute, "Downloaded 3 min ago"}, {2 * time.Hour, "Downloaded 2 h ago"}} {
		if got := sinceText("Downloaded", now, now.Add(-c.ago)); got != c.want {
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

// updatedRig is a frame that starts as version 0.4.0, after an update from 0.3.2 (D209).
type updatedRig struct {
	f     *Frame
	tt    *ui.Tester
	saved []updated.Note
}

// newUpdatedRig starts a frame whose loader gives note n (or err), with the work run at once.
func newUpdatedRig(t *testing.T, n updated.Note, err error) *updatedRig {
	t.Helper()
	f, tt := toastFrame()
	r := &updatedRig{f: f, tt: tt}
	f.Settings = &Settings{Version: "0.4.0", Run: Runner{Go: func(fn func()) { fn() }}}
	f.LoadUpdated = func() (updated.Note, error) { return n, err }
	f.SaveUpdated = func(n updated.Note) error { r.saved = append(r.saved, n); return nil }
	tt.Frame()
	f.loadUpdated(Runner{Go: func(fn func()) { fn() }}, nil)
	tt.Frame()
	tt.Frame()
	return r
}

func (r *updatedRig) has(label string) bool {
	_, ok := r.tt.Find(label)
	return ok
}

func dueNote() updated.Note {
	return updated.Note{Version: "0.4.0", From: "0.3.2", Notes: "## 0.4.0\n- What's new panel\n- Faster tabs",
		InstalledAt: time.Now().Add(-3*time.Minute - 10*time.Second)}
}

const updatedPill = UpdatedPrefix + "0.4.0"

// closedAndShown checks that the pill and the panel went and the note was saved as shown once.
func (r *updatedRig) closedAndShown(t *testing.T) {
	t.Helper()
	r.tt.Frame()
	if r.has(LabelUpdatePanel) || r.has(updatedPill) {
		t.Error("the pill or the panel stays after the panel closed")
	}
	if len(r.saved) != 1 || !r.saved[0].Shown || r.saved[0].Version != "0.4.0" || r.saved[0].From != "0.3.2" {
		t.Errorf("saved %+v, want one shown note of 0.4.0", r.saved)
	}
}

func TestWhatsNewOpensAtStartAfterAnUpdate(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	if !r.has(updatedPill) || !r.has(LabelUpdatePanel) {
		t.Fatalf("no pill or panel at start: %q", r.tt.Texts())
	}
	texts := r.tt.Texts()
	for _, want := range []string{updatedPill, "contr 0.4.0", "from 0.3.2", UpdateWhatsNew, "What's new panel", "Faster tabs", "Updated 3 min ago", UpdateGotIt + " ⏎"} {
		if !slices.Contains(texts, want) {
			t.Errorf("no %q: %q", want, texts)
		}
	}
	for _, not := range []string{UpdateAgentsNote, UpdateLater, SettingsRestart + " ⏎", LabelUpdatePill} {
		if slices.Contains(texts, not) {
			t.Errorf("the panel after an update has %q", not)
		}
	}
	pill, panel := box(t, r.tt, updatedPill), box(t, r.tt, LabelUpdatePanel)
	if panel.Y < pill.Y+pill.H {
		t.Errorf("the panel %v is not under the pill %v", panel, pill)
	}
	if len(r.saved) != 0 {
		t.Errorf("saved %+v before the panel closed", r.saved)
	}
}

func TestGotItClosesWhatsNewAndMarksItShown(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	if err := r.tt.Click(UpdateGotIt + " ⏎"); err != nil {
		t.Fatal(err)
	}
	r.closedAndShown(t)
}

func TestReturnPressesGotIt(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	r.tt.Key(0, ui.KeyEnter)
	r.closedAndShown(t)
}

func TestEscapeClosesWhatsNew(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	r.tt.Key(0, ui.KeyEscape)
	r.closedAndShown(t)
}

func TestAPressOutsideClosesWhatsNew(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	r.tt.ClickAt(theme.WindowWidth/2, theme.WindowHeight-40)
	r.closedAndShown(t)
}

func TestWhatsNewDoesNotShowAgain(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	if err := r.tt.Click(UpdateGotIt + " ⏎"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	again := newUpdatedRig(t, r.saved[0], nil)
	if again.has(updatedPill) || again.has(LabelUpdatePanel) {
		t.Error("What's new shows again after it was shown")
	}
}

func TestWhatsNewShowsNothingForOtherCases(t *testing.T) {
	other := dueNote()
	other.Version = "0.3.2"
	for name, c := range map[string]struct {
		n   updated.Note
		err error
	}{
		"missing file":  {updated.Note{}, nil},
		"corrupt file":  {updated.Note{}, errors.New("updated: parse: bad")},
		"other version": {other, nil},
	} {
		r := newUpdatedRig(t, c.n, c.err)
		if r.has(LabelUpdatePanel) || r.has(updatedPill) || r.has(UpdatedPrefix+"0.3.2") {
			t.Errorf("%s: What's new shows", name)
		}
		if len(r.saved) != 0 {
			t.Errorf("%s: saved %+v", name, r.saved)
		}
	}
}

func TestAnInstallRecordsTheNoteForTheNextStart(t *testing.T) {
	f, _ := toastFrame()
	var saved []updated.Note
	f.SaveUpdated = func(n updated.Note) error { saved = append(saved, n); return nil }
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	if err := f.recordInstall(UpdateInfo{Version: "0.4.0", Notes: "- x"}, "0.3.2", at); err != nil {
		t.Fatal(err)
	}
	want := updated.Note{Version: "0.4.0", From: "0.3.2", Notes: "- x", InstalledAt: at}
	if len(saved) != 1 || saved[0] != want {
		t.Errorf("saved %+v, want %+v", saved, want)
	}
	f.SaveUpdated = nil
	if err := f.recordInstall(UpdateInfo{Version: "0.4.0"}, "0.3.2", at); err != nil {
		t.Errorf("no saver: %v", err)
	}
}
