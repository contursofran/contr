package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/git"
)

// cloneRig sets up the clone part with a fake git runner and a temp parent.
func cloneRig(t *testing.T, run git.CloneRunner) (*modalRig, string) {
	t.Helper()
	r := newModalRig(t)
	parent := t.TempDir()
	r.m.CloneParent = parent
	r.m.CloneRun = run
	r.m.Resolve = func(p string) string { return p + "/.git" }
	r.m.Now = func() time.Time { return time.Unix(60, 0) }
	return r, parent
}

func (r *modalRig) shows(sub string) bool {
	r.tt.Frame()
	for _, s := range r.tt.Texts() {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func okRunner(calls *[][]string) git.CloneRunner {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		*calls = append(*calls, args)
		return "", os.MkdirAll(args[len(args)-1], 0o755)
	}
}

// typeQuery opens the modal and types q into the field.
func (r *modalRig) typeQuery(q string) {
	r.open()
	r.tt.Type(q)
	r.tt.Frame()
	r.tt.Frame()
}

func TestCloneHiddenWithoutParent(t *testing.T) {
	r := newModalRig(t)
	r.typeQuery("acme/widget")
	if r.has(LabelCloneRow) {
		t.Error("clone row shows without a parent folder")
	}
}

func TestCloneRowShowsForRepo(t *testing.T) {
	for _, q := range []string{"acme/widget", "https://github.com/acme/widget", "https://www.github.com/acme/widget.git/", "git@github.com:acme/widget.git"} {
		r, parent := cloneRig(t, nil)
		r.typeQuery(q)
		if !r.has(LabelCloneRow) || !r.shows("Clone acme/widget") {
			t.Errorf("%q: no clone row in %v", q, r.tt.Texts())
		}
		if want := "into " + filepath.Join(parent, "widget"); !r.shows(want) {
			t.Errorf("%q: no meta %q in %v", q, want, r.tt.Texts())
		}
		if !r.shows("Add") || r.has(ProjectsRow("a")) || r.shows("Open · ") || r.shows("⏎ clone") == false {
			t.Errorf("%q: wrong sections: %v", q, r.tt.Texts())
		}
	}
}

func TestCloneEnterClonesAndAdds(t *testing.T) {
	var calls [][]string
	r, parent := cloneRig(t, okRunner(&calls))
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	dest := filepath.Join(parent, "widget")
	if len(calls) != 1 || calls[0][1] != "--" || calls[0][2] != "https://github.com/acme/widget.git" || calls[0][3] != dest {
		t.Fatalf("git args = %v", calls)
	}
	if got := r.store.Folders(); !slices.Equal(got, []string{dest + "/.git"}) || r.store.Path(dest+"/.git") != dest {
		t.Fatalf("folders = %v", got)
	}
	if rec := r.store.Recent(); len(rec) != 1 || rec[0].Path != dest {
		t.Fatalf("recent = %v", rec)
	}
	if len(r.saved) != 1 || r.m.query != "" || r.m.cloning || r.m.cloneErr != "" {
		t.Errorf("saved %d, query %q, cloning %v, err %q", len(r.saved), r.m.query, r.m.cloning, r.m.cloneErr)
	}
}

func TestCloneShowsProgressAndCancelStopsGit(t *testing.T) {
	var ctx context.Context
	r, _ := cloneRig(t, func(c context.Context, _ string, _ ...string) (string, error) {
		ctx = c
		return "", c.Err()
	})
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	if !r.m.cloning || !r.shows("Cloning acme/widget") || !r.has(LabelCloneProgress) || !r.has(ProjectsCancel) || !r.shows("⏎ wait") {
		t.Fatalf("no busy state: %v", r.tt.Texts())
	}
	r.tt.Key(0, ui.KeyEnter) // wait: Enter does nothing
	if len(r.held) != 1 {
		t.Fatalf("%d clones started", len(r.held))
	}
	r.tt.Click(ProjectsCancel)
	r.held[0]()
	if ctx == nil || ctx.Err() == nil {
		t.Error("git context not cancelled")
	}
	if r.m.cloning || r.m.cloneErr != "" || len(r.store.Folders()) != 0 || len(r.store.Recent()) != 0 {
		t.Errorf("after cancel: cloning %v err %q folders %v", r.m.cloning, r.m.cloneErr, r.store.Folders())
	}
}

func TestCloneErrorShowsTryAgain(t *testing.T) {
	n := 0
	r, _ := cloneRig(t, func(context.Context, string, ...string) (string, error) {
		n++
		return "fatal: could not read Username for 'https://github.com'\n", errors.New("exit 128")
	})
	r.typeQuery("acme/private")
	r.tt.Key(0, ui.KeyEnter)
	if n != 1 || len(r.store.Folders()) != 0 || r.m.cloning || r.m.query != "acme/private" {
		t.Errorf("state after failure: n %d %v %v %q", n, r.store.Folders(), r.m.cloning, r.m.query)
	}
	if !r.shows("Could not clone acme/private") || !r.shows("could not read Username") || !r.shows("GitHub access") ||
		!r.has(ProjectsTryAgain) || !r.shows("⏎ try again") || r.has(ProjectsChangeDir) {
		t.Fatalf("no error row: %v", r.tt.Texts())
	}
	r.tt.Click(ProjectsTryAgain)
	r.tt.Click(LabelProjectsSearch)
	if n != 2 {
		t.Errorf("git ran %d times, want 2", n)
	}
	r.tt.Type("x") // editing the field clears the error
	r.tt.Frame()
	if r.m.cloneErr != "" || r.has(ProjectsTryAgain) {
		t.Error("an edit did not clear the error")
	}
}

func TestCloneDestinationExistsShowsError(t *testing.T) {
	var calls [][]string
	r, parent := cloneRig(t, okRunner(&calls))
	if err := os.Mkdir(filepath.Join(parent, "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	if len(calls) != 0 || len(r.store.Folders()) != 0 || len(r.saved) != 0 {
		t.Errorf("git ran or store changed: %v %v", calls, r.store.Folders())
	}
	if !r.shows("folder exists: " + filepath.Join(parent, "widget")) {
		t.Errorf("no error shown: %v", r.tt.Texts())
	}
}

func TestCloneBusyBlocksSecondRun(t *testing.T) {
	var calls [][]string
	r, _ := cloneRig(t, okRunner(&calls))
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	if !r.m.cloning {
		t.Fatal("no busy state")
	}
	r.m.clone()
	if len(r.held) != 1 {
		t.Fatalf("%d clones started, want 1", len(r.held))
	}
	r.held[0]()
	if r.m.cloning || len(calls) != 1 || len(r.store.Folders()) != 1 {
		t.Errorf("after run: cloning %v calls %d folders %v", r.m.cloning, len(calls), r.store.Folders())
	}
}

func TestCloneChangeFolderChangesDestination(t *testing.T) {
	r, _ := cloneRig(t, nil)
	other := t.TempDir()
	var def string
	r.m.PickParent = func(d string) (string, error) { def = d; return other, nil }
	r.typeQuery("acme/widget")
	r.tt.Click(ProjectsChangeDir)
	if def != r.m.CloneParent {
		t.Errorf("dialog started at %q", def)
	}
	if want := "into " + filepath.Join(other, "widget"); !r.shows(want) {
		t.Errorf("no meta %q in %v", want, r.tt.Texts())
	}
}

func TestCloneChangeFolderCancelAndError(t *testing.T) {
	r, _ := cloneRig(t, nil)
	r.m.PickParent = func(string) (string, error) { return "", ErrPickCancelled }
	r.typeQuery("acme/widget")
	r.tt.Click(ProjectsChangeDir)
	if r.m.parent != "" || len(r.status) != 0 {
		t.Errorf("cancel changed state: %q %v", r.m.parent, r.status)
	}
	r.m.PickParent = func(string) (string, error) { return "", errors.New("dialog broke") }
	r.tt.Click(ProjectsChangeDir)
	if len(r.status) != 1 || !strings.Contains(r.status[0], "dialog broke") || r.m.parent != "" {
		t.Errorf("status = %v", r.status)
	}
}

func TestCloneNoPickerHidesChangeFolder(t *testing.T) {
	r, _ := cloneRig(t, nil)
	r.typeQuery("acme/widget")
	if r.has(ProjectsChangeDir) {
		t.Error("button shows without a picker")
	}
}

func TestPathRowOpensFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(home, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, _ := cloneRig(t, nil)
	r.typeQuery("~/x")
	if !r.has(LabelPathRow) || !r.shows("Open x") || !r.shows("~/x") || !r.shows("Add") || !r.shows("⏎ open") || r.has(ProjectsRow("a")) {
		t.Fatalf("no path row: %v", r.tt.Texts())
	}
	r.tt.Key(0, ui.KeyEnter)
	dir := filepath.Join(home, "x")
	if got := r.store.Folders(); !slices.Equal(got, []string{dir + "/.git"}) || r.store.Path(dir+"/.git") != dir {
		t.Fatalf("folders = %v", got)
	}
	if len(r.saved) != 1 || r.m.query != "" {
		t.Errorf("saved %d, query %q", len(r.saved), r.m.query)
	}
}

func TestPathRowMissingFolderShowsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	r, _ := cloneRig(t, nil)
	r.typeQuery("~/nope")
	r.tt.Key(0, ui.KeyEnter)
	if !r.shows("not a folder: ~/nope") || len(r.store.Folders()) != 0 || len(r.saved) != 0 || r.has(ProjectsTryAgain) {
		t.Errorf("no error: %v %v", r.tt.Texts(), r.store.Folders())
	}
	r.tt.Type("x") // an edit clears the line
	r.tt.Frame()
	if r.shows("not a folder") {
		t.Error("an edit did not clear the error")
	}
}

func TestPathRowFileIsNotAFolder(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := cloneRig(t, nil)
	r.typeQuery(f)
	r.tt.Key(0, ui.KeyEnter)
	if !r.shows("not a folder: "+f) || len(r.store.Folders()) != 0 {
		t.Errorf("no error: %v", r.tt.Texts())
	}
}

func TestCloneMessage(t *testing.T) {
	if got := cloneMessage(git.ErrGitNotFound, "/x"); got != "git is not installed" {
		t.Errorf("git missing: %q", got)
	}
	if got := cloneMessage(errors.New("fatal: Repository not found."), "/x"); !strings.Contains(got, "GitHub access") {
		t.Errorf("not found: %q", got)
	}
	if got := cloneMessage(errors.New("disk full"), "/x"); got != "disk full" {
		t.Errorf("other: %q", got)
	}
}

func TestCloneDefaultsToParentOfLastOpenedProject(t *testing.T) {
	r, fallback := cloneRig(t, nil)
	older, newest := t.TempDir(), t.TempDir()
	r.store.Touch("/w/a/.git", filepath.Join(older, "a"), time.Unix(10, 0))
	r.store.Touch("/w/b/.git", filepath.Join(newest, "b"), time.Unix(20, 0))
	r.m.query = "acme/widget"
	r.m.refreshDefault()
	_, dest, err := r.m.cloneDest()
	if err != nil || dest != filepath.Join(newest, "widget") {
		t.Errorf("dest %q err %v, want under %q (not %q)", dest, err, newest, fallback)
	}
}

func TestCloneDefaultsToFallbackWithoutRecent(t *testing.T) {
	r, fallback := cloneRig(t, nil)
	r.m.query = "acme/widget"
	if _, dest, _ := r.m.cloneDest(); dest != filepath.Join(fallback, "widget") {
		t.Errorf("no recent: dest %q", dest)
	}
	// A recent project whose parent folder is gone also falls back.
	r.store.Touch("/w/a/.git", filepath.Join(fallback, "gone", "a"), time.Unix(10, 0))
	r.m.refreshDefault()
	if _, dest, _ := r.m.cloneDest(); dest != filepath.Join(fallback, "widget") {
		t.Errorf("missing parent: dest %q", dest)
	}
}

func TestCloseModalCancelsCloneAndAddsNothing(t *testing.T) {
	r, _ := cloneRig(t, func(ctx context.Context, _ string, _ ...string) (string, error) {
		return "", ctx.Err()
	})
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	r.tt.Key(0, ui.KeyEnter)
	if len(r.held) != 1 {
		t.Fatal("clone did not start")
	}
	r.m.Close()
	r.held[0]()
	if r.m.cloning || r.m.cloneErr != "" || len(r.store.Folders()) != 0 || len(r.store.Recent()) != 0 {
		t.Errorf("after close: cloning %v err %q folders %v", r.m.cloning, r.m.cloneErr, r.store.Folders())
	}
}

func TestCloneDefaultIsKeptAndNotCheckedInView(t *testing.T) {
	r, fallback := cloneRig(t, nil)
	dir := t.TempDir()
	r.store.Touch("/w/a/.git", filepath.Join(dir, "a"), time.Unix(10, 0))
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.typeQuery("acme/widget")
	// The file check waits on the goroutine: the view still uses the fallback.
	if _, dest, _ := r.m.cloneDest(); dest != filepath.Join(fallback, "widget") {
		t.Errorf("before the check: dest %q", dest)
	}
	for _, f := range r.held {
		f()
	}
	r.held = nil
	if _, dest, _ := r.m.cloneDest(); dest != filepath.Join(dir, "widget") {
		t.Errorf("after the check: dest %q", dest)
	}
	// Removing the folder changes nothing until the next refresh.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, dest, _ := r.m.cloneDest(); dest != filepath.Join(dir, "widget") {
		t.Errorf("kept value lost: dest %q", dest)
	}
}
