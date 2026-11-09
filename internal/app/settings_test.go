package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
)

// fakeUpdates is an update service that the test controls.
type fakeUpdates struct {
	enabled    bool
	info       *UpdateInfo
	checkErr   error
	installErr error
	checks     int
	installs   int
	// progress is sent to the install callback.
	progress [][2]int64
}

func (f *fakeUpdates) Enabled() bool { return f.enabled }

func (f *fakeUpdates) Check(context.Context) (*UpdateInfo, error) {
	f.checks++
	return f.info, f.checkErr
}

func (f *fakeUpdates) Install(_ context.Context, progress func(done, total int64)) error {
	f.installs++
	for _, p := range f.progress {
		progress(p[0], p[1])
	}
	return f.installErr
}

type settingsRig struct {
	t         *testing.T
	s         *Settings
	u         *fakeUpdates
	tt        *ui.Tester
	relaunch  int
	redrawals int
}

func newSettingsRig(t *testing.T, u *fakeUpdates) *settingsRig {
	t.Helper()
	r := &settingsRig{t: t, u: u}
	r.s = &Settings{Version: "1.2.3", Run: Runner{Go: func(f func()) { f() }},
		Relaunch: func() { r.relaunch++ }, Invalidate: func() { r.redrawals++ }}
	if u != nil {
		r.s.Updates = u
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Grow(1).Label("window")
			r.s.View(c)
		})
	}, 1000, 700)
	r.tt.Frame()
	return r
}

func (r *settingsRig) open() { r.s.Toggle(); r.tt.Frame() }

// openUpdates opens the modal on the Updates tab with a click on the tab.
func (r *settingsRig) openUpdates() {
	r.t.Helper()
	r.open()
	if err := r.tt.Click(SettingsTabs[TabUpdates]); err != nil {
		r.t.Fatal(err)
	}
	r.tt.Frame()
}
func (r *settingsRig) has(label string) bool {
	_, ok := r.tt.Find(label)
	return ok
}
func (r *settingsRig) texts() []string { return r.tt.Texts() }
func (r *settingsRig) hasText(want string) bool {
	return slices.ContainsFunc(r.texts(), func(s string) bool { return strings.Contains(s, want) })
}

func TestSettingsShowsTheVersionAndClosesWithEscAndDone(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	if r.has(LabelSettings) {
		t.Fatal("modal shows before it opens")
	}
	r.openUpdates()
	if !r.has(LabelSettings) || !r.hasText("contr 1.2.3") || !r.has(SettingsCheck) {
		t.Fatalf("modal incomplete: %v", r.texts())
	}
	r.tt.Key(0, ui.KeyEscape)
	if r.s.Opened() {
		t.Error("Esc did not close")
	}
	r.openUpdates()
	r.tt.Click(SettingsDone)
	if r.s.Opened() {
		t.Error("Done did not close")
	}
	r.openUpdates()
	r.tt.Click(SettingsEsc)
	if r.s.Opened() {
		t.Error("esc button did not close")
	}
	r.s.Toggle()
	r.s.Toggle()
	if r.s.Opened() {
		t.Error("a second Toggle did not close")
	}
}

func TestSettingsSaysUpdatesAreOffWhenTheBuildHasNoKey(t *testing.T) {
	u := &fakeUpdates{enabled: false}
	r := newSettingsRig(t, u)
	r.openUpdates()
	if !r.hasText(TextUpdatesOff) {
		t.Fatalf("no off text in %v", r.texts())
	}
	r.tt.Click(SettingsCheck)
	if u.checks != 0 {
		t.Error("Check for updates ran with updates off")
	}
}

func TestSettingsWithoutAnUpdateServiceSaysUpdatesAreOff(t *testing.T) {
	r := newSettingsRig(t, nil)
	r.openUpdates()
	if !r.hasText(TextUpdatesOff) {
		t.Fatalf("no off text in %v", r.texts())
	}
}

func TestSettingsReportsUpToDate(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	r := newSettingsRig(t, u)
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	if u.checks != 1 || !r.hasText(TextUpToDate) || r.has(SettingsInstall) {
		t.Fatalf("checks %d, texts %v", u.checks, r.texts())
	}
}

func TestSettingsShowsTheNotesOfANewerVersionAndInstalls(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0", Notes: "## 1.3.0\n\n- Fixed the sidebar"},
		progress: [][2]int64{{50, 100}, {100, 100}}}
	r := newSettingsRig(t, u)
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	if !r.hasText("contr 1.3.0 is available") || !r.has(LabelSettingsNotes) || !r.has(SettingsInstall) {
		t.Fatalf("no update shown: %v", r.texts())
	}
	if !r.hasText("Fixed the sidebar") {
		t.Errorf("notes not shown: %v", r.texts())
	}
	r.tt.Click(SettingsInstall)
	r.tt.Frame()
	if u.installs != 1 || r.relaunch != 1 {
		t.Fatalf("installs %d, relaunches %d", u.installs, r.relaunch)
	}
	if got := r.s.percent.Load(); got != 100 {
		t.Errorf("progress = %d, want 100", got)
	}
	if r.has(SettingsInstall) {
		t.Error("Install and restart still shows after the install")
	}
}

func TestSettingsShowsACheckError(t *testing.T) {
	u := &fakeUpdates{enabled: true, checkErr: errors.New("offline")}
	r := newSettingsRig(t, u)
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	if !r.hasText("update check failed: offline") {
		t.Fatalf("no error: %v", r.texts())
	}
	if r.hasText(TextUpToDate) {
		t.Error("a failed check says up to date")
	}
	u.checkErr = nil
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	if r.hasText("failed") {
		t.Error("the error stays after a good check")
	}
}

func TestSettingsShowsAnInstallErrorAndDoesNotRelaunch(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}, installErr: errors.New("bad signature")}
	r := newSettingsRig(t, u)
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	r.tt.Click(SettingsInstall)
	r.tt.Frame()
	if !r.hasText("install failed: bad signature") || r.relaunch != 0 {
		t.Fatalf("texts %v, relaunches %d", r.texts(), r.relaunch)
	}
	if !r.has(SettingsInstall) {
		t.Error("Install and restart is gone, so the owner cannot try again")
	}
}

func TestSettingsButtonsDoNothingWhileWorkRuns(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	var held []func()
	r := newSettingsRig(t, u)
	r.s.Run = Runner{Go: func(f func()) { held = append(held, f) }}
	r.s.enabled, r.s.enabledKnown = true, true
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Frame()
	if !r.hasText(TextChecking) {
		t.Fatalf("no checking text: %v", r.texts())
	}
	r.tt.Click(SettingsCheck)
	if len(held) != 1 {
		t.Errorf("a second check started: %d jobs", len(held))
	}
}

// scheduleRig runs StartChecks with an update service. Every redraw sends on redrawn, so a test
// waits for the check without sleeping.
func scheduleRig(t *testing.T, u *fakeUpdates, every time.Duration, visible func() bool) (s *Settings, redrawn chan struct{}, stop func()) {
	t.Helper()
	redrawn = make(chan struct{}, 100)
	s = &Settings{Updates: u, Version: "1.2.3", Every: every, Visible: visible,
		Run:        Runner{Go: func(f func()) { f() }},
		Invalidate: func() { redrawn <- struct{}{} }}
	stop = s.StartChecks()
	t.Cleanup(stop)
	return s, redrawn, stop
}

func waitFor(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no redraw")
	}
}

func TestStartChecksInstallsTheUpdateInTheBackgroundWithoutARelaunch(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}}
	s, redrawn, _ := scheduleRig(t, u, time.Hour, nil)
	relaunch := 0
	s.Relaunch = func() { relaunch++ }
	waitFor(t, redrawn)
	if !s.Ready() {
		t.Fatal("a newer version does not make the update ready")
	}
	for range 20 {
		if _, _, ok := s.Installed(); ok {
			break
		}
		waitFor(t, redrawn)
	}
	info, _, ok := s.Installed()
	if !ok || info.Version != "1.3.0" || u.installs != 1 {
		t.Fatalf("installed %v %q after %d installs; want 1.3.0 after 1", ok, info.Version, u.installs)
	}
	if relaunch != 0 {
		t.Error("a background install relaunched contr")
	}
	s.RestartNow()
	if relaunch != 1 || u.installs != 1 {
		t.Errorf("Restart now: relaunch %d, installs %d; want 1 and 1", relaunch, u.installs)
	}
}

func TestABackgroundInstallErrorShowsInSettingsOnly(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}, installErr: errors.New("disk full")}
	r := newSettingsRig(t, u)
	status := 0
	r.s.Status = func(string) { status++ }
	r.s.autoCheck()
	if _, _, ok := r.s.Installed(); ok {
		t.Fatal("a failed download is ready")
	}
	if status != 0 {
		t.Error("a failed download showed a toast")
	}
	r.openUpdates()
	if !r.hasText("download failed: disk full") || !r.has(SettingsInstall) {
		t.Errorf("Settings does not show the failed download: %q", r.texts())
	}
}

func TestSettingsOffersRestartNowWhenAnUpdateIsReady(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}}
	r := newSettingsRig(t, u)
	r.s.autoCheck()
	r.openUpdates()
	if !r.has(SettingsRestart) || r.has(SettingsInstall) {
		t.Fatalf("no Restart now in Settings: %q", r.texts())
	}
	if err := r.tt.Click(SettingsRestart); err != nil {
		t.Fatal(err)
	}
	if r.relaunch != 1 {
		t.Errorf("relaunch = %d after Restart now", r.relaunch)
	}
}

func TestStartChecksChecksAgainEveryInterval(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	_, redrawn, _ := scheduleRig(t, u, 5*time.Millisecond, nil)
	waitFor(t, redrawn)
	waitFor(t, redrawn)
	waitFor(t, redrawn)
}

func TestStartChecksSkipsAHiddenWindow(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}}
	var visible atomic.Bool
	_, redrawn, _ := scheduleRig(t, u, 5*time.Millisecond, visible.Load)
	time.Sleep(60 * time.Millisecond) // many intervals pass
	if len(redrawn) != 0 {
		t.Fatal("a hidden window was checked")
	}
	visible.Store(true)
	waitFor(t, redrawn)
}

func TestStartChecksStaysQuietOnAnErrorAndWhenUpdatesAreOff(t *testing.T) {
	u := &fakeUpdates{enabled: true, checkErr: errors.New("offline")}
	s, redrawn, _ := scheduleRig(t, u, time.Hour, nil)
	waitFor(t, redrawn)
	if s.Ready() || s.err != "" {
		t.Errorf("a failed check shows: ready %v, err %q", s.Ready(), s.err)
	}
	off := &fakeUpdates{enabled: false, info: &UpdateInfo{Version: "1.3.0"}}
	s2, redrawn2, _ := scheduleRig(t, off, time.Hour, nil)
	waitFor(t, redrawn2)
	if s2.Ready() || off.checks != 0 {
		t.Error("a build with updates off checked")
	}
}

func TestStartChecksStops(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	_, redrawn, stop := scheduleRig(t, u, 5*time.Millisecond, nil)
	waitFor(t, redrawn)
	stop()
	stop() // twice is safe
	time.Sleep(30 * time.Millisecond)
	for len(redrawn) > 0 {
		<-redrawn
	}
	time.Sleep(30 * time.Millisecond)
	if len(redrawn) != 0 {
		t.Error("checks go on after stop")
	}
}

func TestStartChecksWithoutAServiceDoesNothing(t *testing.T) {
	s := &Settings{}
	s.StartChecks()()
}

func TestSettingsInstallErrorReachesTheStatusMessage(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}, installErr: errors.New("no space")}
	r := newSettingsRig(t, u)
	var msgs []string
	r.s.Status = func(m string) { msgs = append(msgs, m) }
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	r.tt.Click(SettingsInstall)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "no space") {
		t.Errorf("status messages = %v", msgs)
	}
}

func TestSetVisibleChecksAtOnceWhenNoCheckRan(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	s := &Settings{Updates: u, Run: Runner{Go: func(f func()) { f() }}}
	s.SetVisible(false)
	if u.checks != 0 {
		t.Fatal("a hiding window was checked")
	}
	s.SetVisible(true) // launch while hidden: no check ran yet
	if u.checks != 1 {
		t.Fatalf("checks = %d, want 1", u.checks)
	}
}

func TestSetVisibleChecksOnlyWhenTheLastCheckIsOld(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	now := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	s := &Settings{Updates: u, Every: time.Hour, Now: func() time.Time { return now },
		Run: Runner{Go: func(f func()) { f() }}}
	s.autoCheck()
	if u.checks != 1 {
		t.Fatalf("checks = %d after the first check", u.checks)
	}
	now = now.Add(59 * time.Minute)
	s.SetVisible(true)
	if u.checks != 1 {
		t.Errorf("a recent check ran again: %d", u.checks)
	}
	now = now.Add(2 * time.Minute) // 61 minutes: a scheduled check fell while hidden
	s.SetVisible(true)
	if u.checks != 2 {
		t.Errorf("checks = %d, want 2 after the missed interval", u.checks)
	}
	s.SetVisible(true) // shown again at once: the schedule restarted
	if u.checks != 2 {
		t.Errorf("checks = %d, want 2 right after a check", u.checks)
	}
}

func TestSkippedHiddenCheckDoesNotCountAsACheck(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	hidden := false
	s := &Settings{Updates: u, Visible: func() bool { return !hidden },
		Run: Runner{Go: func(f func()) { f() }}}
	hidden = true
	s.autoCheck() // timer fires while hidden
	hidden = false
	s.SetVisible(true)
	if u.checks != 1 {
		t.Fatalf("checks = %d, want 1", u.checks)
	}
}

func TestSettingsAppearanceRowShowsTheThreeChoices(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.open()
	for _, name := range AppearanceNames {
		if !r.has(name) {
			t.Errorf("the Appearance row has no %q segment", name)
		}
	}
	if !r.has(LabelAppearance) {
		t.Error("the Appearance row has no label")
	}
}

func TestSettingsAppearanceRowChangesTheSource(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	cur := mygo.ThemeSystem
	var set []mygo.ThemeSource
	r.s.Appearance = func() mygo.ThemeSource { return cur }
	r.s.SetAppearance = func(s mygo.ThemeSource) { set = append(set, s); cur = s }
	r.open()
	if err := r.tt.Click("Dark"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.Click("Light"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if err := r.tt.Click("System"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	want := []mygo.ThemeSource{mygo.ThemeDark, mygo.ThemeLight, mygo.ThemeSystem}
	if !slices.Equal(set, want) {
		t.Errorf("SetAppearance got %v, want %v", set, want)
	}
}

func TestSettingsAppearanceRowDoesNotSetAgainForTheSameChoice(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	calls := 0
	r.s.Appearance = func() mygo.ThemeSource { return mygo.ThemeDark }
	r.s.SetAppearance = func(mygo.ThemeSource) { calls++ }
	r.open()
	r.tt.Frame()
	r.tt.Click("Dark")
	r.tt.Frame()
	if calls != 0 {
		t.Errorf("SetAppearance ran %d times for the choice already in use", calls)
	}
}

func TestSettingsAppearanceRowNeedsNoHandlers(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.open()
	if err := r.tt.Click("Light"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
}

func TestSettingsShowsTheHCVersion(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.s.CoreVersion = func() (string, bool) { return "0.9.3", true }
	r.openUpdates()
	if !r.hasText("hc 0.9.3") || !r.hasText("contr 1.2.3") {
		t.Fatalf("versions missing: %v", r.texts())
	}
}

func TestSettingsSaysThatHCIsNotConnected(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.openUpdates()
	if !r.hasText(TextCoreOff) {
		t.Fatalf("no %q without a source: %v", TextCoreOff, r.texts())
	}
	r.s.CoreVersion = func() (string, bool) { return "", false }
	r.tt.Frame()
	if !r.hasText(TextCoreOff) || r.hasText("hc 0") {
		t.Fatalf("not connected core reads wrong: %v", r.texts())
	}
}

func TestSettingsReadyFollowsTheFoundUpdate(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}}
	r := newSettingsRig(t, u)
	if r.s.Ready() {
		t.Fatal("ready before a check")
	}
	r.openUpdates()
	r.tt.Click(SettingsCheck)
	if !r.s.Ready() {
		t.Fatal("not ready after a check found 1.3.0")
	}
}

// An open Settings dialog reads the palette of the current theme on every build: after a switch
// to Light its card is the Light ground, not the Dark one it was built with (#400, D155).
func TestOpenSettingsDialogChangesWithTheTheme(t *testing.T) {
	restoreTheme(t)
	panelPixel := func(r *settingsRig) [3]uint8 {
		t.Helper()
		b, ok := r.tt.Find(LabelSettings)
		if !ok {
			t.Fatal("no Settings dialog")
		}
		p := pixelAt(r.tt.Image(), b.X+6, b.Y+6)
		return [3]uint8{p.R, p.G, p.B}
	}
	want := func(p *theme.Palette) [3]uint8 {
		c := p.Ground
		return [3]uint8{c.R, c.G, c.B}
	}
	f := theme.Follow(&fakeTheme{}, &fakeWindow{}, mygo.ThemeDark)
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.open()
	if got := panelPixel(r); got != want(theme.Dark) {
		t.Fatalf("dark card = %v, want %v", got, want(theme.Dark))
	}
	f.SetSource(mygo.ThemeLight)
	r.tt.Frame()
	if got := panelPixel(r); got != want(theme.Light) {
		t.Errorf("light card = %v, want the Light ground %v", got, want(theme.Light))
	}
	f.SetSource(mygo.ThemeDark)
	r.tt.Frame()
	if got := panelPixel(r); got != want(theme.Dark) {
		t.Errorf("dark again = %v, want %v", got, want(theme.Dark))
	}
}

func TestSettingsShowsTheTintSlidersWithValues(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.s.Tint = func() (float64, float64) { return 30, 55 }
	r.open()
	for _, want := range []string{LabelBackground, LabelPanels, "30%", "55%", TintReset} {
		if !r.hasText(want) {
			t.Errorf("Settings has no %q: %v", want, r.texts())
		}
	}
}

func TestSettingsTintSliderCallsSetTint(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	win, pan := 30.0, 55.0
	var calls [][2]float64
	r.s.Tint = func() (float64, float64) { return win, pan }
	r.s.SetTint = func(w, p float64) { win, pan = w, p; calls = append(calls, [2]float64{w, p}) }
	r.open()
	if err := r.tt.Click(LabelBackground + " alpha"); err != nil {
		t.Fatal(err)
	}
	r.tt.Key(0, ui.KeyHome)
	r.tt.Frame()
	r.tt.Frame()
	if len(calls) == 0 || win != 0 || pan != 55 {
		t.Fatalf("SetTint calls %v, win %v pan %v; want Background 0 and Panels kept", calls, win, pan)
	}
	if !r.hasText("0%") {
		t.Errorf("the value did not follow: %v", r.texts())
	}
}

func TestSettingsTintResetCallsResetTint(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	resets := 0
	r.s.Tint = func() (float64, float64) { return 30, 55 }
	r.s.ResetTint = func() { resets++ }
	r.open()
	if err := r.tt.Click(TintReset); err != nil {
		t.Fatal(err)
	}
	if resets != 1 {
		t.Errorf("ResetTint ran %d times, want 1", resets)
	}
}

func TestSettingsWithoutTintHandlersHasNoSliders(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	r.open()
	if r.hasText(LabelBackground) {
		t.Error("sliders show without a Tint source")
	}
}

func TestSettingsTintSliderFollowsAClickOnTheTrack(t *testing.T) {
	r := newSettingsRig(t, &fakeUpdates{enabled: true})
	win, pan := 30.0, 55.0
	r.s.Tint = func() (float64, float64) { return win, pan }
	r.s.SetTint = func(w, p float64) { win, pan = w, p }
	r.open()
	if err := r.tt.Click(LabelPanels + " alpha"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	r.tt.Frame()
	if pan < 45 || pan > 55 || pan == 55 || win != 30 {
		t.Errorf("after a click at the middle: Background %v Panels %v; want Panels near 50", win, pan)
	}
}

func fakeClockSettings(u *fakeUpdates, now *time.Time) *Settings {
	return &Settings{Updates: u, Now: func() time.Time { return *now },
		Run: Runner{Go: func(f func()) { f() }}}
}

func TestCheckEveryIsFifteenMinutes(t *testing.T) {
	if CheckEvery != 15*time.Minute || ActiveAfter != 10*time.Minute {
		t.Errorf("CheckEvery %v, ActiveAfter %v; want 15m and 10m", CheckEvery, ActiveAfter)
	}
}

func TestATickAfterASleepChecksOnTheWallClock(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	now := time.Now()
	s := fakeClockSettings(u, &now)
	s.dueCheck()
	if u.checks != 1 {
		t.Fatalf("checks = %d at the first tick", u.checks)
	}
	now = now.Add(14 * time.Minute)
	s.dueCheck()
	if u.checks != 1 {
		t.Fatalf("a tick inside 15 minutes checked: %d", u.checks)
	}
	now = now.Add(5 * time.Hour) // the Mac slept: wall time jumps
	s.dueCheck()
	if u.checks != 2 {
		t.Errorf("checks = %d after a sleep, want 2", u.checks)
	}
}

func TestActivatedChecksOnlyAfterTenMinutes(t *testing.T) {
	u := &fakeUpdates{enabled: true}
	now := time.Now()
	s := fakeClockSettings(u, &now)
	s.Activated()
	if u.checks != 1 {
		t.Fatalf("checks = %d on the first activation", u.checks)
	}
	now = now.Add(9 * time.Minute)
	s.Activated()
	if u.checks != 1 {
		t.Errorf("an activation inside 10 minutes checked: %d", u.checks)
	}
	now = now.Add(2 * time.Minute)
	s.Activated()
	if u.checks != 2 {
		t.Errorf("checks = %d after 11 minutes, want 2", u.checks)
	}
	(&Settings{}).Activated() // no service: nothing
}

func TestAFailedCheckDoesNotResetTheAge(t *testing.T) {
	u := &fakeUpdates{enabled: true, checkErr: errors.New("offline")}
	now := time.Now()
	s := fakeClockSettings(u, &now)
	s.dueCheck()
	now = now.Add(time.Minute)
	s.dueCheck() // the next tick tries again
	if u.checks != 2 {
		t.Fatalf("checks = %d, want 2: a failed check reset the age", u.checks)
	}
	u.checkErr = nil
	now = now.Add(time.Minute)
	s.dueCheck()
	now = now.Add(time.Minute)
	s.dueCheck()
	if u.checks != 3 {
		t.Errorf("checks = %d, want 3: a good check did not reset the age", u.checks)
	}
}

func TestADueCheckThatFindsAnUpdateInstallsInTheBackground(t *testing.T) {
	u := &fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}}
	now := time.Now()
	s := fakeClockSettings(u, &now)
	s.Activated()
	if _, _, ok := s.Installed(); !ok || u.installs != 1 {
		t.Errorf("installed %v after %d installs; want a background install", ok, u.installs)
	}
}

// recorded is one RecordInstall call, or the relaunch ("relaunch").
type recorded struct {
	what, version, from string
	at                  time.Time
}

// recordRig is a Settings with a pending update of 1.3.0 whose RecordInstall and Relaunch log
// their calls in order (D209).
func recordRig(u *fakeUpdates) (*Settings, *[]recorded) {
	var log []recorded
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	s := &Settings{Updates: u, Version: "1.2.3", Run: Runner{Go: func(f func()) { f() }},
		Now:      func() time.Time { return at },
		Relaunch: func() { log = append(log, recorded{what: "relaunch"}) },
		RecordInstall: func(info UpdateInfo, from string, at time.Time) error {
			log = append(log, recorded{what: "record", version: info.Version, from: from, at: at})
			return nil
		}}
	return s, &log
}

func TestAnInstallRecordsTheUpdateBeforeTheRelaunch(t *testing.T) {
	s, log := recordRig(&fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}})
	s.enabled = true
	s.Check()
	s.Install()
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := []recorded{{"record", "1.3.0", "1.2.3", at}, {what: "relaunch"}}
	if !slices.Equal(*log, want) {
		t.Errorf("calls %+v, want %+v", *log, want)
	}
}

func TestABackgroundInstallRecordsTheUpdate(t *testing.T) {
	s, log := recordRig(&fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}})
	s.autoCheck()
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := []recorded{{"record", "1.3.0", "1.2.3", at}}
	if !slices.Equal(*log, want) {
		t.Errorf("calls %+v, want %+v", *log, want)
	}
	if _, readyAt, ok := s.Installed(); !ok || !readyAt.Equal(at) {
		t.Errorf("Installed: at %v, ok %v", readyAt, ok)
	}
}

func TestAFailedInstallRecordsNothing(t *testing.T) {
	s, log := recordRig(&fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}, installErr: errors.New("offline")})
	s.autoCheck()
	s.Install()
	if len(*log) != 0 {
		t.Errorf("calls %+v after failed installs", *log)
	}
}

func TestAFailedRecordStillRelaunches(t *testing.T) {
	s, log := recordRig(&fakeUpdates{enabled: true, info: &UpdateInfo{Version: "1.3.0"}})
	s.RecordInstall = func(UpdateInfo, string, time.Time) error { return errors.New("disk full") }
	s.enabled = true
	s.Check()
	s.Install()
	if !slices.Equal(*log, []recorded{{what: "relaunch"}}) {
		t.Errorf("calls %+v, want the relaunch", *log)
	}
}
