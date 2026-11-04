package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"github.com/contursofran/contr/internal/github"
)

const prFilesJSON = `{"files":[{"path":"a.go","additions":2,"deletions":1},{"path":"img.png","additions":0,"deletions":0}]}`

const prDiffText = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,4 @@
 package a
-var x = 1
+var x = 2
+var y = 3
 func f() {}
diff --git a/img.png b/img.png
index 333..444 100644
Binary files a/img.png and b/img.png differ
`

const prThreadsJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
{"id":"T1","isResolved":false,"path":"a.go","line":3,"originalLine":3,"diffSide":"RIGHT",
 "comments":{"nodes":[{"id":"C1","author":{"login":"bob"},"body":"why three?","createdAt":"2026-11-03T10:00:00Z"}]}},
{"id":"T2","isResolved":false,"path":"a.go","line":2,"originalLine":2,"diffSide":"LEFT",
 "comments":{"nodes":[{"id":"C2","author":{"login":"carol"},"body":"old one was fine","createdAt":"2026-11-03T10:00:00Z"}]}},
{"id":"T3","isResolved":true,"path":"a.go","line":1,"originalLine":1,"diffSide":"RIGHT",
 "comments":{"nodes":[{"id":"C3","author":{"login":"dave"},"body":"fixed already","createdAt":"2026-11-03T10:00:00Z"}]}},
{"id":"T4","isResolved":false,"path":"gone.go","line":1,"originalLine":1,"diffSide":"RIGHT",
 "comments":{"nodes":[{"id":"C4","author":{"login":"erin"},"body":"other file","createdAt":"2026-11-03T10:00:00Z"}]}}
]}}}}}`

// filesRig opens the pane on a fake gh that also answers the Files reads.
func filesRig(t *testing.T, diff, threads string, fail map[string]bool) *prRig {
	t.Helper()
	r := newPRRig(t)
	r.pane.Client = func(dir string) github.Client {
		return github.Client{Dir: dir, R: runnerFunc(func(args ...string) ([]byte, error) {
			j := strings.Join(args, " ")
			for k := range fail {
				if strings.Contains(j, k) {
					return nil, errors.New("boom " + k)
				}
			}
			switch {
			case strings.Contains(j, "--json files"):
				return []byte(prFilesJSON), nil
			case strings.HasPrefix(j, "pr diff"):
				return []byte(diff), nil
			case strings.HasPrefix(j, "repo view"):
				return []byte(`{"owner":{"login":"o"},"name":"n"}`), nil
			case strings.HasPrefix(j, "api graphql"):
				return []byte(threads), nil
			}
			return []byte(prJSON), nil
		})}
	}
	r.pane.Open("/w", 642)
	r.flush()
	if err := r.tt.Click("Files 3"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	return r
}

type runnerFunc func(args ...string) ([]byte, error)

func (f runnerFunc) Run(_ context.Context, _ string, args ...string) ([]byte, error) { return f(args...) }

func TestPRFilesListCounts(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, nil)
	r.flush()
	for _, w := range []string{"a.go", "img.png", "+2 −1"} {
		if !r.tt.HasText(w) {
			t.Errorf("missing %q in %q", w, r.tt.Texts())
		}
	}
}

func TestPRFilesSelectShowsHunk(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, nil)
	r.flush()
	d := r.pane.filesState.diff
	if d == nil || !strings.Contains(d.New.String(), "var y = 3") || !strings.Contains(d.Old.String(), "var x = 1") {
		t.Fatalf("first file diff missing: %+v", d)
	}
	if err := r.tt.Click("file img.png"); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame()
	if r.pane.filesState.diff != nil || !r.tt.HasText(LabelPRBinary) {
		t.Errorf("binary file should replace the diff: %q", r.tt.Texts())
	}
}

func TestPRFilesThreadUnderLine(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, nil)
	r.flush()
	for _, w := range []string{"bob", "why three?", "on line 3", "carol", "old one was fine", "on line 2"} {
		if !r.tt.HasText(w) {
			t.Errorf("missing %q in %q", w, r.tt.Texts())
		}
	}
	var sides []editor.DiffSide
	for _, a := range r.pane.filesState.diff.Annotations {
		if a.Key == "T2" {
			sides = append(sides, a.Side)
		}
	}
	if len(sides) != 1 || sides[0] != editor.DiffOld {
		t.Errorf("a LEFT thread annotates the old text: %v", sides)
	}
	if r.tt.HasText("other file") {
		t.Error("a thread on a file outside the list is ignored")
	}
}

func TestPRFilesResolvedThreadIsOneLine(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, nil)
	r.flush()
	if !r.tt.HasText("resolved · dave") {
		t.Errorf("texts %q", r.tt.Texts())
	}
	if r.tt.HasText("fixed already") {
		t.Error("a resolved thread hides its text")
	}
}

func TestPRFilesBadPatchShowsError(t *testing.T) {
	bad := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -x,y +z @@\n+oops\n"
	r := filesRig(t, bad, `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`, nil)
	r.flush()
	if !r.tt.HasText("a.go") {
		t.Errorf("the list stays: %q", r.tt.Texts())
	}
	found := false
	for _, s := range r.tt.Texts() {
		if strings.HasPrefix(s, "Cannot read the diff of a.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRFilesReadErrorShown(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, map[string]bool{"pr diff": true})
	r.flush()
	if !r.tt.HasText("gh: boom pr diff") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRFilesThreadErrorKeepsFiles(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, map[string]bool{"api graphql": true})
	r.flush()
	if !r.tt.HasText("a.go") || !r.tt.HasText("gh: boom api graphql") {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRFilesStaleDropped(t *testing.T) {
	r := filesRig(t, prDiffText, prThreadsJSON, nil)
	r.pane.Open("/w", 7) // a newer PR before the files land
	r.flush()
	if r.pane.filesState.loaded || r.pane.filesState.files != nil {
		t.Error("an older files read must not land")
	}
}

func TestPRFilesNotReadBeforeTheTab(t *testing.T) {
	r := newPRRig(t)
	r.pane.Open("/w", 642)
	r.flush()
	if r.pane.filesState.started {
		t.Error("no files read before the Files tab is chosen")
	}
}

func TestPatchTextLineMapsBothSides(t *testing.T) {
	fps, err := editor.ParsePatch(prDiffText)
	if err != nil {
		t.Fatal(err)
	}
	// New: 1 package, 2 var x = 2, 3 var y = 3, 4 func. Old: 1, 2 var x = 1, 3 func.
	if i, ok := patchTextLine(fps[0], editor.DiffNew, 3); !ok || i != 2 {
		t.Errorf("new 3 -> %d %v", i, ok)
	}
	if i, ok := patchTextLine(fps[0], editor.DiffOld, 2); !ok || i != 1 {
		t.Errorf("old 2 -> %d %v", i, ok)
	}
	if _, ok := patchTextLine(fps[0], editor.DiffNew, 99); ok {
		t.Error("a line outside the hunks is not mapped")
	}
}
