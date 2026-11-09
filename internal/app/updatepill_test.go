package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/app/view"
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
	f.Settings = &Settings{Updates: r.u, Version: "1.2.3",
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
	pill, toast := apptest.Box(t, r.tt, LabelUpdatePill), apptest.Box(t, r.tt, LabelUpdateToast)
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
	f.Settings = &Settings{Version: "0.4.0"}
	f.deps.Updated.Load = func() (updated.Note, error) { return n, err }
	f.deps.Updated.Save = func(n updated.Note) error { r.saved = append(r.saved, n); return nil }
	tt.Frame()
	f.loadUpdated(view.Env{})
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
	pill, panel := apptest.Box(t, r.tt, updatedPill), apptest.Box(t, r.tt, LabelUpdatePanel)
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
	f.deps.Updated.Save = func(n updated.Note) error { saved = append(saved, n); return nil }
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	if err := f.recordInstall(UpdateInfo{Version: "0.4.0", Notes: "- x"}, "0.3.2", at); err != nil {
		t.Fatal(err)
	}
	want := updated.Note{Version: "0.4.0", From: "0.3.2", Notes: "- x", InstalledAt: at}
	if len(saved) != 1 || saved[0] != want {
		t.Errorf("saved %+v, want %+v", saved, want)
	}
	f.deps.Updated.Save = nil
	if err := f.recordInstall(UpdateInfo{Version: "0.4.0"}, "0.3.2", at); err != nil {
		t.Errorf("no saver: %v", err)
	}
}

// The update panel and What's new have an opaque fill, float-solid, so the rows behind them do
// not cut through their text (#524, D212). float-solid is glass-float at full alpha.
func TestTheUpdatePanelFillIsFloatSolid(t *testing.T) {
	for name, p := range map[string]*theme.Palette{"dark": theme.Dark, "light": theme.Light} {
		if floatCardFill(p) != p.FloatSolid {
			t.Errorf("%s: the update panel fill is not float-solid", name)
		}
		s, g := p.FloatSolid, p.GlassFloat
		if s.A != 255 || s.R != g.R || s.G != g.G || s.B != g.B {
			t.Errorf("%s: float-solid %v is not glass-float %v at full alpha", name, s, g)
		}
	}
}

// whatsNewThenReady starts version 1.2.3 after an update with What's new open, closes its panel
// by hand when closed is true (the pill stays), then makes 1.3.0 ready (#533).
func whatsNewThenReady(t *testing.T, closed bool) (*pillRig, *[]updated.Note) {
	t.Helper()
	r := newPillRig(t, nil)
	var saved []updated.Note
	r.f.deps.Updated.Save = func(n updated.Note) error { saved = append(saved, n); return nil }
	n := dueNote()
	n.Version, n.From = "1.2.3", "1.2.0"
	r.f.deps.Updated.Load = func() (updated.Note, error) { return n, nil }
	r.f.loadUpdated(view.Env{})
	r.tt.Frame()
	if !r.has(UpdatedPrefix+"1.2.3") || !r.has(LabelUpdatePanel) {
		t.Fatalf("no What's new at start: %q", r.tt.Texts())
	}
	if closed {
		r.f.upd.afterOpen = false
	}
	r.ready()
	return r, &saved
}

func checkReadyAfterWhatsNew(t *testing.T, r *pillRig, saved []updated.Note) {
	t.Helper()
	if !r.has(LabelUpdatePill) {
		t.Fatalf("no ready pill: %q", r.tt.Texts())
	}
	if r.has(UpdatedPrefix + "1.2.3") {
		t.Error("the Updated to pill stays next to the ready pill")
	}
	if r.has(LabelUpdatePanel) || r.f.upd.open {
		t.Error("the ready panel opened by itself")
	}
	if !r.has(LabelUpdateToast) {
		t.Error("no ready toast")
	}
	if len(saved) != 1 || !saved[0].Shown || saved[0].Version != "1.2.3" {
		t.Errorf("saved %+v, want one shown note of 1.2.3", saved)
	}
	r.tt.Key(0, ui.KeyEnter)
	r.tt.Frame()
	if r.relaunch != 0 {
		t.Errorf("relaunch = %d after Return with the ready panel closed", r.relaunch)
	}
}

func TestAReadyUpdateWhileWhatsNewIsOpenKeepsItsPanelClosed(t *testing.T) {
	r, saved := whatsNewThenReady(t, false)
	checkReadyAfterWhatsNew(t, r, *saved)
}

func TestAReadyUpdateAfterWhatsNewClosedKeepsItsPanelClosed(t *testing.T) {
	r, saved := whatsNewThenReady(t, true)
	checkReadyAfterWhatsNew(t, r, *saved)
}

func TestAClickOnTheUpdatedPillClosesWhatsNew(t *testing.T) {
	r := newUpdatedRig(t, dueNote(), nil)
	if err := r.tt.Click(updatedPill); err != nil {
		t.Fatal(err)
	}
	r.closedAndShown(t)
}
