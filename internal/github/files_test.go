package github

import (
	"context"
	"strings"
	"testing"
)

// seqRunner answers each call from the first rule whose key is in the args.
type seqRunner struct {
	rules []rule
	calls [][]string
}

type rule struct {
	key string
	out string
	err error
}

func (s *seqRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, args)
	j := strings.Join(args, " ")
	for _, r := range s.rules {
		if strings.Contains(j, r.key) {
			return []byte(r.out), r.err
		}
	}
	return nil, nil
}

const filesJSON = `{"files":[
{"path":"a.go","additions":2,"deletions":1},
{"path":"new/b.go","additions":0,"deletions":0},
{"path":"logo.png","additions":0,"deletions":0}]}`

const diffText = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,2 +1,3 @@
 keep
-old
+new
+more
diff --git a/old/b.go b/new/b.go
similarity index 90%
rename from old/b.go
rename to new/b.go
index 333..444 100644
--- a/old/b.go
+++ b/new/b.go
@@ -1 +1 @@
-x
+y
diff --git a/logo.png b/logo.png
index 555..666 100644
Binary files a/logo.png and b/logo.png differ
`

func TestFilesJoinsCountsAndPatches(t *testing.T) {
	r := &seqRunner{rules: []rule{{"--json files", filesJSON, nil}, {"pr diff", diffText, nil}}}
	files, err := Client{R: r}.Files(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files %+v", files)
	}
	a := files[0]
	if a.Path != "a.go" || a.OldPath != "" || a.Additions != 2 || a.Deletions != 1 ||
		!strings.HasPrefix(a.Patch, "diff --git a/a.go b/a.go\n") || !strings.Contains(a.Patch, "+more\n") ||
		strings.Contains(a.Patch, "old/b.go") {
		t.Errorf("a: %+v", a)
	}
	b := files[1]
	if b.Path != "new/b.go" || b.OldPath != "old/b.go" || !strings.Contains(b.Patch, "+y\n") {
		t.Errorf("b: %+v", b)
	}
	if p := files[2]; p.Path != "logo.png" || p.Patch != "" {
		t.Errorf("binary: %+v", p)
	}
}

func TestFilesDiffFails(t *testing.T) {
	r := &seqRunner{rules: []rule{{"--json files", filesJSON, nil}, {"pr diff", "", errString("diff too large")}}}
	_, err := Client{R: r}.Files(context.Background(), 7)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") || !strings.Contains(err.Error(), "diff too large") {
		t.Errorf("err %v", err)
	}
}

func TestFilesBadJSON(t *testing.T) {
	r := &seqRunner{rules: []rule{{"--json files", "nope", nil}}}
	_, err := Client{R: r}.Files(context.Background(), 7)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("err %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
