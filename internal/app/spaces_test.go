package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/host"
)

// ---- Fake git layouts on disk (plain files, no `git` binary) ----

func writeFakeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mainRepo makes a main checkout at root with `.git/HEAD` = head and returns
// the git dir.
func mainRepo(t *testing.T, root, head string) string {
	t.Helper()
	gitDir := filepath.Join(root, ".git")
	writeFakeFile(t, filepath.Join(gitDir, "HEAD"), head)
	return gitDir
}

// linkedWorktree makes a linked worktree at root, laid out as git writes it,
// and returns its admin dir.
func linkedWorktree(t *testing.T, common, name, root, head string) string {
	t.Helper()
	admin := filepath.Join(common, "worktrees", name)
	writeFakeFile(t, filepath.Join(admin, "HEAD"), head)
	writeFakeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(root, ".git")+"\n")
	writeFakeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	writeFakeFile(t, filepath.Join(root, ".git"), "gitdir: "+admin+"\n")
	return admin
}

// canonicalTmp is a temporary directory in the canonical form that the scan's
// rows use.
func canonicalTmp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// oneWorktree makes a repo at base/repo with a linked worktree at base/wt1 on
// branch feature. It returns the worktree's root and its admin dir.
func oneWorktree(t *testing.T, base string) (string, string) {
	t.Helper()
	gitDir := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	root := filepath.Join(base, "wt1")
	return root, linkedWorktree(t, gitDir, "w1", root, "ref: refs/heads/feature\n")
}

// twoWorktrees makes base/wt1 on feature and base/wt2 on other.
func twoWorktrees(t *testing.T, base string) (string, string) {
	t.Helper()
	gitDir := mainRepo(t, filepath.Join(base, "repo"), "ref: refs/heads/main\n")
	wt1, wt2 := filepath.Join(base, "wt1"), filepath.Join(base, "wt2")
	linkedWorktree(t, gitDir, "w1", wt1, "ref: refs/heads/feature\n")
	linkedWorktree(t, gitDir, "w2", wt2, "ref: refs/heads/other\n")
	return wt1, wt2
}

// ---- Trees ----

// spPane is a live worker named id in the folder cwd, with an hc status
// when status is not nil.
func spPane(id, cwd string, status *string) agents.Worker {
	w := agents.Worker{Name: id, Address: id, Role: "builder", Worktree: cwd, Live: true}
	if status != nil {
		w.Status = *status
	}
	return w
}

func hs(s string) *string { return &s }

// spTree is the agent state of a followed core with the workers ws.
func spTree(ws ...agents.Worker) agentstate.Snapshot {
	return agentstate.Snapshot{Workers: ws, CoreVersion: "0.1.18"}
}

// hcDown is the agent state of a core that contr does not follow:
// no core version, and the hc problem.
func hcDown() agentstate.Snapshot {
	return agentstate.Snapshot{Problem: "hc: connection refused"}
}

// spSnap is the agent state the list reads.
type spSnap struct {
	mu sync.Mutex
	s  agentstate.Snapshot
}

func (f *spSnap) get() agentstate.Snapshot  { f.mu.Lock(); defer f.mu.Unlock(); return f.s }
func (f *spSnap) set(s agentstate.Snapshot) { f.mu.Lock(); f.s = s; f.mu.Unlock() }

// ---- Harness ----

// spacesHarness shows the sidebar head part alone in a window as wide as the
// sidebar. The scan runs at once on this goroutine, so a test sees its result.
type spacesHarness struct {
	t    *testing.T
	live *spSnap
	sel  *state.Selection
	list *SpacesList
	host *fakeHost
	tt   *ui.Tester
	// heads are the HEAD watches that the list started, by file path.
	heads map[string]*fakeHead
}

// fakeHead is a HEAD watch that the test drives: change calls the list's
// callback, as the file watch does on a goroutine.
type fakeHead struct {
	changed func()
	ctx     context.Context
}

func newSpaces(t *testing.T, live agentstate.Snapshot) *spacesHarness {
	t.Helper()
	h := &spacesHarness{t: t, live: &spSnap{s: live}, sel: state.NewSelection(nil),
		host: &fakeHost{}, heads: map[string]*fakeHead{}}
	h.list = NewSpacesList(h.live.get, h.sel, nil)
	h.list.start = func(f func()) { f() }
	h.list.Host = h.host
	h.list.WatchHead = func(ctx context.Context, path string, changed func()) error {
		h.heads[path] = &fakeHead{changed: changed, ctx: ctx}
		return nil
	}
	h.list.Sync()
	h.tt = ui.NewTester(h.list.View, theme.SidebarWidth, theme.WindowHeight)
	return h
}

// send replaces the live state, as the reader would, and draws a frame.
func (h *spacesHarness) send(l agentstate.Snapshot) {
	h.t.Helper()
	h.live.set(l)
	h.list.Sync()
	h.tt.Frame()
}

func (h *spacesHarness) group(g int) spaces.RepoGroup {
	h.t.Helper()
	groups := h.list.Groups()
	if g >= len(groups) {
		h.t.Fatalf("no group %d in %d groups", g, len(groups))
	}
	return groups[g]
}

func (h *spacesHarness) rowOf(g, r int) spaces.WorktreeRow { return h.group(g).Rows[r] }

func (h *spacesHarness) has(label string) bool { _, ok := h.tt.Find(label); return ok }

// ---- The scan and the sync ----

func TestTheScanFillsTheGroupsOnceTheTreeIsKnown(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spTree(spPane("w1:p1", wt, nil)))
	if len(h.list.Groups()) != 1 || h.group(0).Name != "repo" {
		t.Fatalf("groups %+v", h.list.Groups())
	}
	// The main checkout is not held, so only the worktree has a row.
	if rows := h.group(0).Rows; len(rows) != 1 || rows[0].ID() != wt {
		t.Errorf("rows %+v", rows)
	}
	if h.list.scans != 1 {
		t.Errorf("scans %d, want 1", h.list.scans)
	}
	if got := h.group(0).Branch; got != "main" {
		t.Errorf("branch %q, want main", got)
	}
}

func TestAnAgentsChangeWithTheSameCwdsRebuildsWithoutAScan(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spTree(spPane("w1:p1", wt, nil)))
	h.send(spTree(spPane("w1:p1", wt, hs("working"))))
	if h.list.scans != 1 {
		t.Errorf("scans %d: same cwds, want no scan", h.list.scans)
	}
	if got := h.rowOf(0, 0).Status; got != icons.Working {
		t.Errorf("status %v, want working", got)
	}
}

func TestANewCwdStartsOneMoreScan(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spTree(spPane("w1:p1", wt, nil)))
	h.send(spTree(spPane("w1:p1", wt, nil), spPane("w1:p2", filepath.Join(base, "repo"), nil)))
	if h.list.scans != 2 {
		t.Errorf("scans %d, want 2", h.list.scans)
	}
}

func TestLosingTheTreeEmptiesTheGroupsAndStopsTheScans(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	h := newSpaces(t, spTree(spPane("w1:p1", wt, nil)))
	if len(h.list.Groups()) != 1 {
		t.Fatal("want one group")
	}
	h.send(hcDown())
	if len(h.list.Groups()) != 0 {
		t.Errorf("groups %+v", h.list.Groups())
	}
	h.send(hcDown())
	if h.list.scans != 1 {
		t.Errorf("scans %d: no tree, so no scan", h.list.scans)
	}
}

func TestACwdOutsideGitGivesNoGroup(t *testing.T) {
	base := canonicalTmp(t)
	// TMPDIR may lie inside a git worktree; do not look above the temp folder.
	t.Setenv("GIT_CEILING_DIRECTORIES", base)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	h := newSpaces(t, spTree(spPane("w1:p1", plain, nil)))
	if len(h.list.Groups()) != 0 {
		t.Errorf("groups %+v", h.list.Groups())
	}
}

func TestAPaneWithoutACwdStartsNoScan(t *testing.T) {
	h := newSpaces(t, spTree(spPane("w1:p1", "", nil)))
	if h.list.scans != 0 {
		t.Errorf("scans %d, want 0", h.list.scans)
	}
}

func TestAScanLandsThroughThePostFunc(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &spSnap{s: spTree(spPane("w1:p1", wt, nil))}
	var started, posted []func()
	l := NewSpacesList(live.get, state.NewSelection(nil), func(f func()) { posted = append(posted, f) })
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	if len(l.Groups()) != 0 || len(started) != 1 {
		t.Fatalf("groups %+v, started %d: the scan has not run", l.Groups(), len(started))
	}
	started[0]()
	if len(l.Groups()) != 0 || len(posted) != 1 {
		t.Fatalf("groups %+v, posted %d: the scan waits for the UI thread", l.Groups(), len(posted))
	}
	posted[0]()
	if len(l.Groups()) != 1 {
		t.Errorf("groups %+v, want one", l.Groups())
	}
}

func TestAScanThatLostTheTreeIsDropped(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	live := &spSnap{s: spTree(spPane("w1:p1", wt, nil))}
	var started []func()
	l := NewSpacesList(live.get, state.NewSelection(nil), nil)
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	live.set(hcDown())
	l.Sync()
	started[0]() // the old scan ends now; post is nil, so it lands at once
	live.set(spTree(spPane("w1:p1", wt, nil)))
	if got := len(l.Groups()); got != 0 {
		t.Errorf("groups %d: a dropped scan came back", got)
	}
}

func TestOnlyTheNewestScanLands(t *testing.T) {
	base := canonicalTmp(t)
	wt, _ := oneWorktree(t, base)
	repo := filepath.Join(base, "repo")
	live := &spSnap{s: spTree(spPane("w1:p1", wt, nil))}
	var started []func()
	l := NewSpacesList(live.get, state.NewSelection(nil), nil)
	l.start = func(f func()) { started = append(started, f) }
	l.Sync()
	live.set(spTree(spPane("w1:p1", wt, nil), spPane("w1:p2", repo, nil)))
	l.Sync()
	started[1]()
	started[0]() // the older scan ends last and is dropped
	if rows := l.Groups()[0].Rows; len(rows) != 2 {
		t.Errorf("rows %+v, want the newest scan's two rows", rows)
	}
}

func TestTheScanFuncGetsTheCwdsInTreeOrder(t *testing.T) {
	live := &spSnap{s: spTree(spPane("w1:p1", "/x/b", nil), spPane("w1:p2", "/x/a", nil), spPane("w1:p3", "/x/b", nil))}
	var got []string
	l := NewSpacesList(live.get, nil, nil)
	l.scanFn = func(cwds []string) git.Scan { got = cwds; return git.Scan{} }
	l.start = func(f func()) { f() }
	l.Sync()
	if want := []string{"/x/b", "/x/a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cwds %q, want %q", got, want)
	}
}

// fakeHost is a host reading that a test sets.
type fakeHost struct {
	mu    sync.Mutex
	stats host.Stats
	has   bool
}

func (f *fakeHost) Stats() (host.Stats, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats, f.has
}

func (f *fakeHost) set(s host.Stats) {
	f.mu.Lock()
	f.stats, f.has = s, true
	f.mu.Unlock()
}

func stats(cpu float32, used, total uint64) host.Stats {
	return host.Stats{CPUPercent: cpu, MemUsed: used, MemTotal: total}
}
