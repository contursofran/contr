package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/github"
)

const checksJSON = `[
{"name":"build","workflow":"CI","bucket":"pass","link":"https://example.test/run/1","startedAt":"2026-11-03T11:00:00Z","completedAt":"2026-11-03T11:02:05Z"},
{"name":"lint","workflow":"CI","bucket":"fail","link":"https://example.test/run/2","startedAt":"2026-11-03T11:00:00Z","completedAt":"2026-11-03T11:00:42Z"},
{"name":"e2e","workflow":"CI","bucket":"pending","link":"https://example.test/run/3","startedAt":"2026-11-03T11:00:00Z","completedAt":"0001-01-01T00:00:00Z"},
{"name":"docs","workflow":"Docs","bucket":"skipping","link":"https://example.test/run/4"}]`

// checksRunner answers pr view with prJSON and pr checks with a canned reply.
type checksRunner struct {
	checks func() ([]byte, error)
	calls  int
}

func (r *checksRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[1] == "checks" {
		r.calls++
		return r.checks()
	}
	return []byte(prJSON), nil
}

func newChecksRig(t *testing.T) (*prRig, *checksRunner) {
	t.Helper()
	r := newPRRig(t)
	cr := &checksRunner{checks: func() ([]byte, error) { return []byte(checksJSON), nil }}
	r.pane.Client = func(dir string) github.Client { return github.Client{R: cr, Dir: dir} }
	r.pane.Open("/w", 642)
	r.flush()
	return r, cr
}

func openChecks(t *testing.T, r *prRig) {
	t.Helper()
	if err := r.tt.Click("Checks 3"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
}

func TestPRChecksReadOnlyWhenTabOpened(t *testing.T) {
	r, cr := newChecksRig(t)
	if cr.calls != 0 || len(r.queue) != 0 {
		t.Fatalf("read before the tab: calls %d queued %d", cr.calls, len(r.queue))
	}
	openChecks(t, r)
	if !r.tt.HasText(LabelPRLoad) {
		t.Errorf("no loading text: %q", r.tt.Texts())
	}
	r.tt.Frame() // more frames do not read again
	r.flush()
	if cr.calls != 1 {
		t.Errorf("calls %d, want 1", cr.calls)
	}
}

func TestPRChecksRowsPerState(t *testing.T) {
	r, _ := newChecksRig(t)
	openChecks(t, r)
	r.flush()
	for _, want := range []string{"build", "2m 05s", "lint", "42s", "e2e", "running", "docs", "skipped"} {
		if !r.tt.HasText(want) {
			t.Errorf("missing %q in %q", want, r.tt.Texts())
		}
	}
	for _, s := range []icons.Status{icons.Done, icons.Blocked, icons.Working, icons.Idle} {
		if _, ok := r.tt.Find(s.Label()); !ok {
			t.Errorf("no %s icon", s.Label())
		}
	}
}

func TestPRChecksLogOpensLink(t *testing.T) {
	r, _ := newChecksRig(t)
	openChecks(t, r)
	r.flush()
	if err := r.tt.Click(LabelPRCheckLog); err != nil {
		t.Fatal(err)
	}
	if len(r.opened) != 1 || !strings.HasPrefix(r.opened[0], "https://example.test/run/") {
		t.Errorf("opened %v", r.opened)
	}
}

func TestPRChecksError(t *testing.T) {
	r, cr := newChecksRig(t)
	cr.checks = func() ([]byte, error) { return nil, errors.New("gh: boom") }
	openChecks(t, r)
	r.flush()
	if !r.tt.HasText("gh: boom") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRChecksEmpty(t *testing.T) {
	r, cr := newChecksRig(t)
	cr.checks = func() ([]byte, error) { return []byte("[]"), nil }
	openChecks(t, r)
	r.flush()
	if !r.tt.HasText(LabelPRNoChecks) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRChecksStaleDropped(t *testing.T) {
	r, _ := newChecksRig(t)
	openChecks(t, r)
	stale := r.queue
	r.queue = nil
	r.pane.Open("/w", 7) // another PR replaces the pane
	for _, f := range stale {
		f()
	}
	r.queue = nil
	r.tt.Frame()
	if r.pane.checks.loaded || r.pane.checks.rows != nil {
		t.Errorf("stale rows landed: %+v", r.pane.checks)
	}
}

func TestPRChecksAgentsSyncRereads(t *testing.T) {
	r, cr := newChecksRig(t)
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	r.pane.Now = func() time.Time { return now }
	r.pane.OnAgentsSync() // Overview tab: no read
	if cr.calls != 0 || len(r.queue) != 0 {
		t.Fatal("read on the Overview tab")
	}
	openChecks(t, r)
	r.flush()
	r.pane.OnAgentsSync() // too soon
	if len(r.queue) != 0 {
		t.Error("re-read inside the minimum gap")
	}
	now = now.Add(checksMinGap)
	r.pane.OnAgentsSync()
	r.pane.OnAgentsSync() // one is in flight
	if len(r.queue) != 1 {
		t.Fatalf("queued %d, want 1", len(r.queue))
	}
	r.flush()
	if cr.calls != 2 {
		t.Errorf("calls %d, want 2", cr.calls)
	}
}

func TestChecksDue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		s    prChecks
		t    checksTrigger
		want bool
	}{
		{"first open", prChecks{}, checksTabOpened, true},
		{"open again", prChecks{started: true}, checksTabOpened, false},
		{"in flight", prChecks{inflight: true, started: true, last: now.Add(-time.Minute)}, checksAgentsSync, false},
		{"sync before first read", prChecks{}, checksAgentsSync, false},
		{"sync too soon", prChecks{started: true, last: now}, checksAgentsSync, false},
		{"sync later", prChecks{started: true, last: now.Add(-time.Minute)}, checksAgentsSync, true},
	}
	for _, c := range cases {
		if got := checksDue(c.s, c.t, now); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
