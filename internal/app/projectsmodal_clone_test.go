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

func TestCloneHiddenWithoutParent(t *testing.T) {
	r := newModalRig(t)
	r.open()
	if r.has(LabelCloneRemote) || r.has(ProjectsClone) {
		t.Error("clone part shows without a parent folder")
	}
}

func TestCloneShowsDestinationPreview(t *testing.T) {
	r, parent := cloneRig(t, nil)
	r.m.remote = "acme/widget"
	r.open()
	if want := "→ " + filepath.Join(parent, "widget"); !r.shows(want) {
		t.Errorf("no preview %q in %v", want, r.tt.Texts())
	}
	r.m.remote = "bad input"
	if !r.shows("not a repository yet") {
		t.Error("no hint for a bad remote")
	}
}

func TestCloneAddsFolderToStoreAndRecent(t *testing.T) {
	var calls [][]string
	r, parent := cloneRig(t, okRunner(&calls))
	r.m.remote = "acme/widget"
	r.open()
	r.tt.Click(ProjectsClone)
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
	if len(r.saved) != 1 || r.m.remote != "" || r.m.cloning || r.m.cloneErr != "" {
		t.Errorf("saved %d, remote %q, cloning %v, err %q", len(r.saved), r.m.remote, r.m.cloning, r.m.cloneErr)
	}
}

func TestCloneDestinationExistsShowsError(t *testing.T) {
	var calls [][]string
	r, parent := cloneRig(t, okRunner(&calls))
	if err := os.Mkdir(filepath.Join(parent, "widget"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.m.remote = "acme/widget"
	r.open()
	r.tt.Click(ProjectsClone)
	if len(calls) != 0 || len(r.store.Folders()) != 0 || len(r.saved) != 0 {
		t.Errorf("git ran or store changed: %v %v", calls, r.store.Folders())
	}
	if !r.shows("folder exists: " + filepath.Join(parent, "widget")) {
		t.Errorf("no error shown: %v", r.tt.Texts())
	}
}

func TestCloneAuthFailureShowsErrorAndKeepsField(t *testing.T) {
	r, _ := cloneRig(t, func(context.Context, string, ...string) (string, error) {
		return "Cloning...\nfatal: could not read Username for 'https://github.com'\n", errors.New("exit 128")
	})
	r.m.remote = "acme/private"
	r.open()
	r.tt.Click(ProjectsClone)
	if len(r.store.Folders()) != 0 || r.m.cloning || r.m.remote != "acme/private" {
		t.Errorf("state after failure: %v %v %q", r.store.Folders(), r.m.cloning, r.m.remote)
	}
	if !r.shows("could not read Username") || !r.shows("GitHub access") {
		t.Errorf("no auth error: %v", r.tt.Texts())
	}
}

func TestCloneBadRemoteDoesNotRunGit(t *testing.T) {
	var calls [][]string
	r, _ := cloneRig(t, okRunner(&calls))
	r.open()
	r.tt.Click(ProjectsClone) // empty field
	if !r.shows("enter owner/name or a URL") {
		t.Errorf("no hint: %v", r.tt.Texts())
	}
	r.m.remote = "a b"
	r.tt.Click(ProjectsClone)
	if !r.shows("not a repository: a b") || len(calls) != 0 {
		t.Errorf("texts %v, calls %v", r.tt.Texts(), calls)
	}
}

func TestCloneBusyBlocksSecondRun(t *testing.T) {
	var calls [][]string
	r, _ := cloneRig(t, okRunner(&calls))
	r.m.Run = Runner{Go: func(f func()) { r.held = append(r.held, f) }}
	r.m.remote = "acme/widget"
	r.open()
	r.tt.Click(ProjectsClone)
	if !r.m.cloning || !r.shows("Cloning…") {
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

func TestCloneChooseFolderChangesDestination(t *testing.T) {
	r, _ := cloneRig(t, nil)
	other := t.TempDir()
	var def string
	r.m.PickParent = func(d string) (string, error) { def = d; return other, nil }
	r.m.remote = "acme/widget"
	r.open()
	r.tt.Click(ProjectsCloneFolder)
	if def != r.m.CloneParent {
		t.Errorf("dialog started at %q", def)
	}
	if want := "→ " + filepath.Join(other, "widget"); !r.shows(want) {
		t.Errorf("no preview %q in %v", want, r.tt.Texts())
	}
}

func TestCloneChooseFolderCancelAndError(t *testing.T) {
	r, parent := cloneRig(t, nil)
	r.m.PickParent = func(string) (string, error) { return "", ErrPickCancelled }
	r.open()
	r.tt.Click(ProjectsCloneFolder)
	if r.m.parent != "" || len(r.status) != 0 {
		t.Errorf("cancel changed state: %q %v", r.m.parent, r.status)
	}
	r.m.PickParent = func(string) (string, error) { return "", errors.New("dialog broke") }
	r.tt.Click(ProjectsCloneFolder)
	if len(r.status) != 1 || !strings.Contains(r.status[0], "dialog broke") || r.m.parent != "" {
		t.Errorf("status = %v", r.status)
	}
	_ = parent
}

func TestCloneNoPickerHidesChooseFolder(t *testing.T) {
	r, _ := cloneRig(t, nil)
	r.open()
	if r.has(ProjectsCloneFolder) {
		t.Error("button shows without a picker")
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
