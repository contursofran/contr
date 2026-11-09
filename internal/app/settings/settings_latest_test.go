package settings

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/view"
)

// readyRig is recordRig with 1.3.0 installed in the background and ready (D176).
func readyRig(t *testing.T) (*Settings, *fakeUpdates, *[]recorded) {
	t.Helper()
	u := &fakeUpdates{On: true, Info: &UpdateInfo{Version: "1.3.0", Notes: "- one"}}
	s, log := recordRig(u)
	s.AutoCheck()
	if info, _, ok := s.Installed(); !ok || info.Version != "1.3.0" || u.Installs != 1 {
		t.Fatalf("no ready 1.3.0: %+v %v, %d installs", info, ok, u.Installs)
	}
	*log = nil
	return s, u, log
}

// readyVersion is the version that waits for the restart, or "".
func readyVersion(s *Settings) string {
	info, _, ok := s.Installed()
	if !ok {
		return ""
	}
	return info.Version
}

// queue is a Runner whose work waits until the test runs it.
type queue struct{ work []func() }

func (q *queue) env() view.Env {
	return view.Env{Run: view.Runner{Go: func(fn func()) { q.work = append(q.work, fn) }}}
}

// runAll runs the waiting work, and the work that it starts, in order.
func (q *queue) runAll() {
	for len(q.work) > 0 {
		fn := q.work[0]
		q.work = q.work[1:]
		fn()
	}
}

func TestACheckWhileReadyInstallsANewerVersion(t *testing.T) {
	s, u, log := readyRig(t)
	var ready []string
	s.OnReady = func(info UpdateInfo) { ready = append(ready, info.Version) }
	u.Info = &UpdateInfo{Version: "1.4.0", Notes: "- two"}
	s.AutoCheck()
	if u.Checks != 2 || u.Installs != 2 || readyVersion(s) != "1.4.0" {
		t.Fatalf("checks %d, installs %d, ready %q; want 1.4.0 installed", u.Checks, u.Installs, readyVersion(s))
	}
	if !slices.Equal(ready, []string{"1.4.0"}) {
		t.Errorf("OnReady %v, want 1.4.0", ready)
	}
	if s.found.Notes != "- two\n\n- one" || s.status() != "contr 1.4.0 is ready: restart to update" {
		t.Errorf("notes %q, status %q", s.found.Notes, s.status())
	}
	if len(*log) != 1 || (*log)[0].version != "1.4.0" || (*log)[0].from != "1.2.3" {
		t.Errorf("recorded %+v, want 1.4.0 from 1.2.3", *log)
	}
}

func TestACheckWhileReadyThatFindsTheReadyVersionInstallsNothing(t *testing.T) {
	s, u, log := readyRig(t)
	s.AutoCheck()
	if u.Checks != 2 || u.Installs != 1 || readyVersion(s) != "1.3.0" || len(*log) != 0 {
		t.Errorf("checks %d, installs %d, ready %q, recorded %+v", u.Checks, u.Installs, readyVersion(s), *log)
	}
}

func TestACheckWhileReadyThatFindsAnOlderVersionInstallsNothing(t *testing.T) {
	s, u, _ := readyRig(t)
	u.Info = &UpdateInfo{Version: "1.2.9"}
	s.AutoCheck()
	if u.Installs != 1 || readyVersion(s) != "1.3.0" {
		t.Errorf("installs %d, ready %q", u.Installs, readyVersion(s))
	}
}

func TestAFailedInstallOfANewerVersionKeepsTheReadyOne(t *testing.T) {
	s, u, log := readyRig(t)
	u.Info, u.InstallErr = &UpdateInfo{Version: "1.4.0"}, errors.New("offline")
	s.AutoCheck()
	if u.Installs != 2 || readyVersion(s) != "1.3.0" || s.err != "" || len(*log) != 0 {
		t.Errorf("installs %d, ready %q, err %q, recorded %+v", u.Installs, readyVersion(s), s.err, *log)
	}
	if s.status() != "contr 1.3.0 is ready: restart to update" {
		t.Errorf("status %q", s.status())
	}
}

func TestAFailedCheckWhileReadyKeepsTheReadyOne(t *testing.T) {
	s, u, _ := readyRig(t)
	u.CheckErr = errors.New("offline")
	s.AutoCheck()
	if u.Checks != 2 || u.Installs != 1 || readyVersion(s) != "1.3.0" {
		t.Errorf("checks %d, installs %d, ready %q", u.Checks, u.Installs, readyVersion(s))
	}
}

// A found update that did not install stops the scheduled checks, as before D226.
func TestAFoundUpdateThatFailedToInstallStopsTheChecks(t *testing.T) {
	u := &fakeUpdates{On: true, Info: &UpdateInfo{Version: "1.3.0"}, InstallErr: errors.New("offline")}
	s, _ := recordRig(u)
	s.AutoCheck()
	s.AutoCheck()
	if u.Checks != 1 {
		t.Errorf("checks %d, want 1", u.Checks)
	}
}

func TestInstallsOverAReadyUpdateKeepTheFirstFromVersion(t *testing.T) {
	s, u, log := readyRig(t)
	u.Info = &UpdateInfo{Version: "1.4.0"}
	s.AutoCheck()
	u.Info = &UpdateInfo{Version: "1.5.0"}
	s.AutoCheck()
	var from []string
	for _, r := range *log {
		from = append(from, r.version+" from "+r.from)
	}
	if !slices.Equal(from, []string{"1.4.0 from 1.2.3", "1.5.0 from 1.2.3"}) {
		t.Errorf("recorded %v", from)
	}
}

func TestRestartNowInstallsANewerVersionThenRelaunchesOnce(t *testing.T) {
	s, u, log := readyRig(t)
	u.Info = &UpdateInfo{Version: "1.4.0"}
	s.RestartNow()
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := []recorded{{"record", "1.4.0", "1.2.3", at}, {what: "relaunch"}}
	if !slices.Equal(*log, want) || u.Checks != 2 || u.Installs != 2 {
		t.Errorf("calls %+v, checks %d, installs %d; want %+v", *log, u.Checks, u.Installs, want)
	}
	if u.CheckLimit <= 0 || u.CheckLimit > RestartCheckTimeout {
		t.Errorf("the restart check has %v left, want at most %v", u.CheckLimit, RestartCheckTimeout)
	}
}

func TestRestartNowWithTheSameVersionRelaunchesIntoIt(t *testing.T) {
	s, u, log := readyRig(t)
	s.RestartNow()
	if !slices.Equal(*log, []recorded{{what: "relaunch"}}) || u.Checks != 2 || u.Installs != 1 {
		t.Errorf("calls %+v, checks %d, installs %d", *log, u.Checks, u.Installs)
	}
}

func TestRestartNowWithAFailedCheckRelaunchesIntoTheReadyOne(t *testing.T) {
	s, u, log := readyRig(t)
	u.CheckErr = errors.New("timeout")
	s.RestartNow()
	if !slices.Equal(*log, []recorded{{what: "relaunch"}}) || u.Installs != 1 || readyVersion(s) != "1.3.0" {
		t.Errorf("calls %+v, installs %d, ready %q", *log, u.Installs, readyVersion(s))
	}
}

func TestRestartNowWithAFailedInstallRelaunchesIntoTheReadyOne(t *testing.T) {
	s, u, log := readyRig(t)
	u.Info, u.InstallErr = &UpdateInfo{Version: "1.4.0"}, errors.New("offline")
	s.RestartNow()
	if !slices.Equal(*log, []recorded{{what: "relaunch"}}) || u.Installs != 2 || readyVersion(s) != "1.3.0" {
		t.Errorf("calls %+v, installs %d, ready %q", *log, u.Installs, readyVersion(s))
	}
}

func TestRestartNowWhileAnInstallRunsRelaunchesWhenItEnds(t *testing.T) {
	s, u, log := readyRig(t)
	q := &queue{}
	s.Env = q.env()
	u.Info = &UpdateInfo{Version: "1.4.0"}
	s.AutoCheck()
	q.work[0]() // the check ends; the install of 1.4.0 waits
	q.work = q.work[1:]
	if !s.installing {
		t.Fatal("no install runs")
	}
	s.RestartNow()
	s.RestartNow()
	if len(*log) != 0 {
		t.Fatalf("relaunched before the install ended: %+v", *log)
	}
	q.runAll()
	if u.Checks != 2 || len(*log) != 2 || (*log)[1].what != "relaunch" || readyVersion(s) != "1.4.0" {
		t.Errorf("checks %d, calls %+v, ready %q; want one relaunch into 1.4.0", u.Checks, *log, readyVersion(s))
	}
}

func TestRestartNowWhileACheckRunsDoesNotCheckAgain(t *testing.T) {
	s, u, log := readyRig(t)
	q := &queue{}
	s.Env = q.env()
	s.AutoCheck()
	s.RestartNow()
	s.RestartNow()
	q.runAll()
	if u.Checks != 2 || !slices.Equal(*log, []recorded{{what: "relaunch"}}) {
		t.Errorf("checks %d, calls %+v; want one check and one relaunch", u.Checks, *log)
	}
}

func TestNoScheduledCheckWhileRestartNowRuns(t *testing.T) {
	s, _, _ := readyRig(t)
	q := &queue{}
	s.Env = q.env()
	s.RestartNow()
	s.checking = false // the restart check runs; a tick comes
	s.AutoCheck()
	if len(q.work) != 1 {
		t.Errorf("%d checks wait, want the restart check only", len(q.work))
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"1.4.0", "1.3.0", true},
		{"1.3.0", "1.4.0", false},
		{"1.3.0", "1.3.0", false},
		{"1.10.0", "1.9.0", true},
		{"v1.3.1", "1.3.0", true},
		{"1.3", "1.3.0", false},
		{"1.3.0.1", "1.3.0", true},
		{"1.3.0", "1.3.0-beta.1", true},
		{"1.3.0-beta.1", "1.3.0", false},
		{"1.3.0-beta.2", "1.3.0-beta.1", true},
		{"nightly", "1.3.0", true},
		{"nightly", "nightly", false},
		{"", "1.3.0", true},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestAnInstallOverAReadyUpdateKeepsTheEarlierNotes(t *testing.T) {
	u := &fakeUpdates{On: true, Info: &UpdateInfo{Version: "1.3", Notes: "## 1.3\n- Third"}}
	s, log := recordRig(u)
	s.Version = "1.2"
	var notes []string
	s.RecordInstall = func(info UpdateInfo, from string, _ time.Time) error {
		*log = append(*log, recorded{what: "record", version: info.Version, from: from})
		notes = append(notes, info.Notes)
		return nil
	}
	s.AutoCheck()
	u.Info = &UpdateInfo{Version: "1.4", Notes: "## 1.4\n- Fourth"}
	s.AutoCheck()
	want := "## 1.4\n- Fourth\n\n## 1.3\n- Third"
	if len(notes) != 2 || notes[1] != want {
		t.Fatalf("recorded notes %q, want the last %q", notes, want)
	}
	if (*log)[1].version != "1.4" || (*log)[1].from != "1.2" {
		t.Errorf("recorded %+v, want 1.4 from 1.2", (*log)[1])
	}
	if info, _, _ := s.Installed(); info.Notes != want {
		t.Errorf("ready notes %q, want %q", info.Notes, want)
	}
}

func TestAFailedInstallOverAReadyUpdateKeepsItsNotes(t *testing.T) {
	s, u, _ := readyRig(t)
	u.Info, u.InstallErr = &UpdateInfo{Version: "1.4.0", Notes: "- two"}, errors.New("offline")
	s.AutoCheck()
	if info, _, _ := s.Installed(); info.Notes != "- one" {
		t.Errorf("ready notes %q, want %q", info.Notes, "- one")
	}
}

func TestJoinNotes(t *testing.T) {
	for _, c := range []struct{ newer, earlier, want string }{
		{"## 1.4\n- a", "## 1.3\n- b", "## 1.4\n- a\n\n## 1.3\n- b"},
		{"## 1.5\n- c\n", "## 1.4\n- a\n\n## 1.3\n- b\n", "## 1.5\n- c\n\n## 1.4\n- a\n\n## 1.3\n- b"},
		{"## 1.4\n- a", "## 1.4\n- old\n\n## 1.3\n- b", "## 1.4\n- a\n\n## 1.3\n- b"},
		{"## [v1.4] - 2026-11-09\n- a", "## 1.4\n- old", "## [v1.4] - 2026-11-09\n- a"},
		{"## 1.4\n- a", "", "## 1.4\n- a"},
		{"", "## 1.3\n- b", "## 1.3\n- b"},
		{"- a", "- b", "- a\n\n- b"},
	} {
		if got := JoinNotes(c.newer, c.earlier); got != c.want {
			t.Errorf("JoinNotes(%q, %q) = %q, want %q", c.newer, c.earlier, got, c.want)
		}
	}
}
