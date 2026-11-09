package reviewview

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/view"
)

// newTestReview returns a view that runs reads at once, and the number of
// reads started.
func newTestReview() (*Review, *int) {
	reads := 0
	r := NewReview(view.Env{})
	r.Env.Run.Go = func(f func()) { reads++; f() }
	return r, &reads
}

func reviewTester(r *Review) *ui.Tester {
	return ui.NewTester(r.View, 1000, 500)
}

func TestReviewOpensFirstBranchAndFile(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	if r.Branch() != "b" || r.File() != "a.txt" {
		t.Fatalf("branch %q, file %q", r.Branch(), r.File())
	}
	if r.Code() == nil || r.Code().New.String() != "a1\n" {
		t.Fatalf("code %+v", r.Code())
	}
	tt := reviewTester(r)
	for _, want := range []string{"b", "2 commits", "a.txt", "b.txt", "Branches", "Files"} {
		if !tt.HasText(want) {
			t.Errorf("no %q; texts %q", want, tt.Texts())
		}
	}
}

func TestReviewClickFile(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	tt := reviewTester(r)
	if err := tt.Click("file b.txt"); err != nil {
		t.Fatal(err)
	}
	if r.File() != "b.txt" || r.Code().New.String() != "b1\nb2\n" {
		t.Errorf("file %q, code %q", r.File(), r.Code().New.String())
	}
}

func TestReviewByCommit(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	tt := reviewTester(r)
	if err := tt.Click("By commit"); err != nil {
		t.Fatal(err)
	}
	cs := r.Commits()
	if len(cs) != 2 || cs[0].Subject != "one" || cs[1].Subject != "two" {
		t.Fatalf("commits %+v", cs)
	}
	if r.Commit() != cs[0].SHA {
		t.Errorf("commit %q, want the first", r.Commit())
	}
	if len(r.Files()) != 1 || r.Files()[0].Path != "a.txt" {
		t.Errorf("files %+v, want only a.txt", r.Files())
	}
	tt.Frame()
	for _, label := range []string{"commit " + cs[0].Short + " text", "commit " + cs[1].Short + " text"} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("commit row %q missing; texts %q", label, tt.Texts())
		}
	}
	if err := tt.Click("commit " + cs[1].Short); err != nil {
		t.Fatal(err)
	}
	if len(r.Files()) != 1 || r.Files()[0].Path != "b.txt" || r.File() != "b.txt" {
		t.Errorf("files %+v, file %q", r.Files(), r.File())
	}
}

func TestReviewBackToWhole(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	tt := reviewTester(r)
	if err := tt.Click("By commit"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Whole branch"); err != nil {
		t.Fatal(err)
	}
	if r.ByCommit() || len(r.Files()) != 2 || r.File() != "a.txt" {
		t.Errorf("by commit %v, files %+v, file %q", r.ByCommit(), r.Files(), r.File())
	}
}

func TestReviewStaleReadDropped(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	s.Git("checkout", "main")
	s.Git("checkout", "-b", "c")
	s.Commit("three", map[string]string{"c.txt": "c\n"})
	r, _ := newTestReview()
	var held []func()
	r.Env.Run.Go = func(f func()) { held = append(held, f) }
	r.SetRepo(s.Dir)
	held[0]() // branches b, c arrive; the read of b's files (held[1]) is held
	if len(held) != 2 || r.Branch() != "b" {
		t.Fatalf("held %d reads, branch %q", len(held), r.Branch())
	}
	r.SelectBranch("c") // read of c's files: held[2]
	held[2]()           // c's files arrive; the read of c.txt is held[3]
	held[1]()           // the stale read of b arrives late
	held[3]()
	if r.Branch() != "c" || len(r.Files()) != 1 || r.Files()[0].Path != "c.txt" || r.Code() == nil {
		t.Errorf("branch %q, files %+v: the stale read changed the state", r.Branch(), r.Files())
	}
}

func TestReviewGitErrorShown(t *testing.T) {
	r, _ := newTestReview()
	r.SetRepo(filepath.Join(t.TempDir(), "missing"))
	if r.Err() == "" {
		t.Fatal("no error")
	}
	tt := reviewTester(r)
	found := false
	for _, txt := range tt.Texts() {
		if strings.Contains(txt, "git") {
			found = true
		}
	}
	if !found {
		t.Errorf("error not shown; texts %q", tt.Texts())
	}
}

func TestReviewNoRepo(t *testing.T) {
	r, reads := newTestReview()
	r.SetRepo("")
	if *reads != 0 {
		t.Errorf("%d reads, want 0", *reads)
	}
	if tt := reviewTester(r); !tt.HasText("Select a project") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestReviewNoBranches(t *testing.T) {
	s := apptest.NewRepo(t)
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	if tt := reviewTester(r); !tt.HasText("No branches ahead of main") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestReviewBinary(t *testing.T) {
	s := apptest.NewRepo(t)
	s.Git("checkout", "-b", "bin")
	s.Commit("bin", map[string]string{"x.bin": "a\x00b\n"})
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	if r.File() != "x.bin" || r.Code() != nil {
		t.Fatalf("file %q, code %v", r.File(), r.Code())
	}
	if tt := reviewTester(r); !tt.HasText("Binary file") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestReviewSetRepoSwitches(t *testing.T) {
	a, b := apptest.NewRepo(t), apptest.NewRepo(t)
	a.Git("checkout", "-b", "from-a")
	a.Commit("x", map[string]string{"x.txt": "x\n"})
	b.Git("checkout", "-b", "from-b")
	b.Commit("y", map[string]string{"y.txt": "y\n"})
	r, _ := newTestReview()
	r.SetRepo(a.Dir)
	r.SetRepo(b.Dir)
	if bs := r.Branches(); len(bs) != 1 || bs[0].Name != "from-b" {
		t.Errorf("branches %+v", bs)
	}
	if r.File() != "y.txt" {
		t.Errorf("file %q", r.File())
	}
}

func TestReviewArrowKeysMoveFile(t *testing.T) {
	s := apptest.NewRepo(t)
	s.BranchWithTwoCommits()
	r, _ := newTestReview()
	r.SetRepo(s.Dir)
	tt := reviewTester(r)
	if err := tt.Click("file a.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	if r.File() != "b.txt" || r.Code() == nil || r.Code().New.String() != "b1\nb2\n" {
		t.Fatalf("after Down: file %q", r.File())
	}
	tt.Key(0, ui.KeyUp)
	if r.File() != "a.txt" || r.Code() == nil || r.Code().New.String() != "a1\n" {
		t.Fatalf("after Up: file %q", r.File())
	}
}
