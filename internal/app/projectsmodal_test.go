package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/spaces"
	"github.com/contursofran/contr/internal/projects"
)

type modalRig struct {
	t      *testing.T
	m      *ProjectsModal
	store  *projects.Store
	dirs   []string // all dirs, tree order
	saved  []*projects.Store
	saveEr error
	status []string
	tt     *ui.Tester
	// held queues work instead of running it, to test saves that overlap.
	held []func()
}

func newModalRig(t *testing.T) *modalRig {
	t.Helper()
	r := &modalRig{t: t, store: &projects.Store{}, dirs: []string{"/w/a/.git", "/w/b/.git", "/w/c/.git"}}
	r.m = &ProjectsModal{
		Groups: r.groups,
		Store:  func() *projects.Store { return r.store },
		Save: func(s *projects.Store) error {
			r.saved = append(r.saved, s)
			return r.saveEr
		},
		Run:    Runner{Go: func(f func()) { f() }},
		Status: func(s string) { r.status = append(r.status, s) },
	}
	r.tt = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			ui.Box(c).Grow(1).Label("window")
			r.m.View(c)
		})
	}, 1000, 700)
	r.tt.Frame()
	return r
}

// groups are the groups the sidebar shows: stored order, hidden left out.
func (r *modalRig) groups() []spaces.RepoGroup {
	var out []spaces.RepoGroup
	for _, d := range r.store.Order(r.dirs) {
		if r.store.Hidden(d) {
			continue
		}
		name := strings.Split(d, "/")[2]
		out = append(out, spaces.RepoGroup{CommonDir: d, Name: name, Rows: []spaces.WorktreeRow{{HoldYou: name == "a"}, {}}})
	}
	return out
}

func (r *modalRig) names() []string {
	var n []string
	for _, g := range r.groups() {
		n = append(n, g.Name)
	}
	return n
}

func (r *modalRig) has(label string) bool { _, ok := r.tt.Find(label); return ok }

func (r *modalRig) open() {
	r.m.Toggle()
	r.tt.Frame()
}

func TestProjectsModalOpensAndListsProjects(t *testing.T) {
	r := newModalRig(t)
	if r.has(LabelProjects) {
		t.Fatal("modal shows before it opens")
	}
	r.open()
	for _, l := range []string{LabelProjects, ProjectsRow("a"), ProjectsRow("b"), ProjectsRow("c"), ProjectsDone} {
		if !r.has(l) {
			t.Errorf("no %q", l)
		}
	}
	texts := r.tt.Texts()
	for _, want := range []string{"3 open", "w · 2 worktrees · 1 need you"} {
		if !slices.Contains(texts, want) {
			t.Errorf("no text %q in %v", want, texts)
		}
	}
	r.m.Toggle()
	r.tt.Frame()
	if r.has(LabelProjects) {
		t.Error("Toggle did not close the modal")
	}
}

func TestProjectsModalClosesWithEscAndDone(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.tt.Key(0, ui.KeyEscape)
	if r.has(LabelProjects) || r.m.Opened() {
		t.Error("Esc did not close the modal")
	}
	r.open()
	r.tt.Click(ProjectsDone)
	if r.m.Opened() {
		t.Error("Done did not close the modal")
	}
	r.open()
	r.tt.Click(ProjectsEsc)
	if r.m.Opened() {
		t.Error("esc button did not close the modal")
	}
}

func TestProjectsModalCloseHidesAndSaves(t *testing.T) {
	r := newModalRig(t)
	apply := 0
	r.m.Apply = func() { apply++ }
	r.open()
	r.tt.Click(ProjectsCloseOf("b"))
	if !r.store.Hidden("/w/b/.git") {
		t.Fatal("project b is not hidden")
	}
	if apply != 1 {
		t.Errorf("Apply ran %d times", apply)
	}
	if len(r.saved) != 1 || !r.saved[0].Hidden("/w/b/.git") {
		t.Fatalf("saved %v", r.saved)
	}
	r.tt.Frame()
	if r.has(ProjectsRow("b")) || !r.has(ProjectsRow("a")) {
		t.Error("the list still shows b, or lost a")
	}
}

func TestProjectsModalReorderStoresOrder(t *testing.T) {
	r := newModalRig(t)
	r.open()
	if r.m.list.Reorder == nil {
		t.Fatal("the list has no Reorder")
	}
	r.m.list.Reorder([]int{2}, 0)
	if got := r.names(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Errorf("order %v", got)
	}
	if len(r.saved) != 1 || !slices.Equal(r.saved[0].Order(r.dirs), []string{"/w/c/.git", "/w/a/.git", "/w/b/.git"}) {
		t.Errorf("saved order wrong: %v", r.saved)
	}
}

func TestProjectsModalReorderKeepsHiddenOnesOut(t *testing.T) {
	r := newModalRig(t)
	r.store.Hide("/w/b/.git")
	r.open()
	r.m.list.Reorder([]int{1}, 0) // rows are a, c: c goes first
	if got := r.names(); !slices.Equal(got, []string{"c", "a"}) {
		t.Errorf("order %v", got)
	}
}

func TestProjectsModalFilterTurnsReorderOff(t *testing.T) {
	r := newModalRig(t)
	r.open()
	r.m.filter = "c"
	r.tt.Frame()
	if !r.has(ProjectsRow("c")) || r.has(ProjectsRow("a")) {
		t.Error("filter did not narrow the list")
	}
	if r.m.list.Reorder != nil {
		t.Error("Reorder stays on while the filter hides rows")
	}
	r.m.reorder([]int{0}, 1)
	if len(r.saved) != 0 {
		t.Error("a reorder under a filter saved")
	}
}

func TestProjectsModalWithoutStoreChangesNothing(t *testing.T) {
	r := newModalRig(t)
	r.m.Store = func() *projects.Store { return nil }
	r.open()
	r.m.closeProject("/w/a/.git")
	r.m.reorder([]int{0}, 2)
	if len(r.saved) != 0 {
		t.Error("saved without a store")
	}
}

func TestProjectsModalSaveErrorShowsInStatus(t *testing.T) {
	r := newModalRig(t)
	r.saveEr = errors.New("disk full")
	r.open()
	r.tt.Click(ProjectsCloseOf("a"))
	if len(r.status) != 1 || !strings.Contains(r.status[0], "disk full") {
		t.Errorf("status %v", r.status)
	}
}

func TestProjectsModalSavesInOrderOffTheUIThread(t *testing.T) {
	r := newModalRig(t)
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.open()
	r.tt.Click(ProjectsCloseOf("a"))
	r.tt.Frame()
	r.tt.Click(ProjectsCloseOf("b"))
	if len(r.saved) != 0 {
		t.Fatal("a save ran on the UI thread")
	}
	if len(r.held) != 1 {
		t.Fatalf("%d saves started, want 1 (the second waits)", len(r.held))
	}
	r.held[0]() // first save done: the newest copy goes next
	if len(r.held) != 2 {
		t.Fatalf("%d saves started, want 2", len(r.held))
	}
	r.held[1]()
	if len(r.saved) != 2 || r.saved[0].Hidden("/w/b/.git") || !r.saved[1].Hidden("/w/b/.git") {
		t.Errorf("saves out of order: %v", r.saved)
	}
}

func TestProjectInitials(t *testing.T) {
	for in, want := range map[string]string{"contr": "C", "herdr-agents": "HA", "a_b_c": "AB", "": ""} {
		if got := projectInitials(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func (r *modalRig) pick(path string, err error) {
	r.m.PickFolder = func() (string, error) { return path, err }
	r.m.Resolve = func(p string) string { return p + "/.git" }
	r.m.Now = func() time.Time { return time.Unix(50, 0) }
}

func TestProjectsModalOpenFolderAddsToStoreAndRecent(t *testing.T) {
	r := newModalRig(t)
	r.pick("/w/d", nil)
	r.open()
	if !r.has(ProjectsOpenFolder) {
		t.Fatal("no open folder button")
	}
	r.tt.Click(ProjectsOpenFolder)
	if got := r.store.Folders(); !slices.Equal(got, []string{"/w/d/.git"}) || r.store.Path("/w/d/.git") != "/w/d" {
		t.Fatalf("folders = %v", got)
	}
	if rec := r.store.Recent(); len(rec) != 1 || rec[0].Path != "/w/d" {
		t.Fatalf("recent = %v", rec)
	}
	if len(r.saved) != 1 {
		t.Errorf("saved %d times", len(r.saved))
	}
	if got := r.store.Order([]string{"/w/d/.git", "/w/a/.git", "/w/b/.git", "/w/c/.git"}); got[3] != "/w/d/.git" {
		t.Errorf("new project is not last: %v", got)
	}
}

func TestProjectsModalOpenFolderCancelDoesNothing(t *testing.T) {
	r := newModalRig(t)
	r.pick("", ErrPickCancelled)
	r.open()
	r.tt.Click(ProjectsOpenFolder)
	if len(r.store.Folders()) != 0 || len(r.store.Recent()) != 0 || len(r.saved) != 0 || len(r.status) != 0 {
		t.Errorf("cancel changed state: %v %v %v", r.store.Recent(), r.saved, r.status)
	}
}

func TestProjectsModalOpenFolderErrorShowsStatus(t *testing.T) {
	r := newModalRig(t)
	r.pick("", errors.New("dialog broke"))
	r.open()
	r.tt.Click(ProjectsOpenFolder)
	if len(r.status) != 1 || !strings.Contains(r.status[0], "dialog broke") || len(r.saved) != 0 {
		t.Errorf("status = %v", r.status)
	}
}

func TestProjectsModalNoPickerHidesOpenFolder(t *testing.T) {
	r := newModalRig(t)
	r.open()
	if r.has(ProjectsOpenFolder) {
		t.Error("button shows without a picker")
	}
}

func TestProjectsModalRecentOpensAProjectAgain(t *testing.T) {
	r := newModalRig(t)
	r.store.Touch("/w/z/.git", "/w/z", time.Unix(1, 0))
	r.store.Touch("/w/a/.git", "/w/a", time.Unix(2, 0)) // open already: not listed
	r.open()
	if r.has(ProjectsOpenOf("a")) {
		t.Error("an open project is in the recent list")
	}
	if !r.has(ProjectsOpenOf("z")) {
		t.Fatal("no open button for z")
	}
	r.tt.Click(ProjectsOpenOf("z"))
	if !slices.Equal(r.store.Folders(), []string{"/w/z/.git"}) || r.store.Recent()[0].Dir != "/w/z/.git" || len(r.saved) != 1 {
		t.Errorf("folders = %v, recent = %v, saved = %d", r.store.Folders(), r.store.Recent(), len(r.saved))
	}
}
