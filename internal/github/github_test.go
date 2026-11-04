package github

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	out  []byte
	err  error
	args []string
	dir  string
	fn   func(ctx context.Context) ([]byte, error)
}

func (f *fakeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	f.dir, f.args = dir, args
	if f.fn != nil {
		return f.fn(ctx)
	}
	return f.out, f.err
}

func TestPRParsesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/pr.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{out: data}
	pr, err := Client{R: f, Dir: "/work"}.PR(context.Background(), 640)
	if err != nil {
		t.Fatal(err)
	}
	if f.dir != "/work" || strings.Join(f.args[:3], " ") != "pr view 640" {
		t.Errorf("dir %q args %v", f.dir, f.args)
	}
	if pr.Number != 640 || pr.Title != "Add the PR viewer client" || pr.State != "OPEN" ||
		pr.Body != "Part of #151" || pr.Author != "alice" || pr.HeadRef != "agent/151-client" ||
		pr.BaseRef != "main" || pr.HeadSHA != "abc123" || pr.Additions != 120 ||
		pr.Deletions != 7 || pr.ChangedFiles != 4 || !pr.IsDraft || pr.Mergeable != "MERGEABLE" || pr.ReviewDecision != "APPROVED" ||
		pr.URL != "https://example.test/o/r/pull/640" {
		t.Errorf("header: %+v", pr)
	}
	if want := (CheckSummary{Pass: 2, Fail: 1, Pending: 1, Skip: 1}); pr.ChecksSummary != want {
		t.Errorf("summary %+v, want %+v", pr.ChecksSummary, want)
	}
	var kinds []string
	for _, e := range pr.Timeline {
		kinds = append(kinds, e.Kind+":"+e.Who+":"+e.Text)
	}
	want := "committed:alice:Add client,commented:bob:Looks fine,reviewed:bob:APPROVED"
	if got := strings.Join(kinds, ","); got != want {
		t.Errorf("timeline %q, want %q", got, want)
	}
}

func TestPRMergedEvent(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"number":1,"state":"MERGED","mergedAt":"2026-11-03T11:00:00Z","closedAt":"2026-11-03T11:00:00Z"}`)}
	pr, err := Client{R: f}.PR(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.Timeline) != 1 || pr.Timeline[0].Kind != "merged" {
		t.Errorf("timeline %+v", pr.Timeline)
	}
}

func TestPRGhMissing(t *testing.T) {
	f := &fakeRunner{err: exec.ErrNotFound}
	_, err := Client{R: f}.PR(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "gh") || !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err %v", err)
	}
}

func TestPRNotFound(t *testing.T) {
	f := &fakeRunner{err: errors.New("GraphQL: Could not resolve to a PullRequest with the number of 9.")}
	_, err := Client{R: f}.PR(context.Background(), 9)
	if err == nil || !strings.Contains(err.Error(), "Could not resolve to a PullRequest") ||
		!strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("err %v", err)
	}
}

func TestPRTimeout(t *testing.T) {
	f := &fakeRunner{fn: func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() {
		_, err := Client{R: f, Timeout: 20 * time.Millisecond}.PR(context.Background(), 1)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("PR did not return after the timeout")
	}
}

func TestPRBadJSON(t *testing.T) {
	f := &fakeRunner{out: []byte("not json")}
	_, err := Client{R: f}.PR(context.Background(), 3)
	if err == nil || !strings.Contains(err.Error(), "read pr 3") {
		t.Errorf("err %v", err)
	}
}

func TestPRNoRunner(t *testing.T) {
	if _, err := (Client{}).PR(context.Background(), 1); err == nil {
		t.Error("want an error")
	}
}

// A fake gh script on PATH checks that ExecRunner passes the folder, the
// arguments and the stderr text.
func TestExecRunnerFakeGh(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = fail ]; then echo 'not logged in' >&2; exit 4; fi\npwd; echo \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, err := ExecRunner{}.Run(context.Background(), dir, "pr", "view")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != dir+"\npr view" {
		t.Errorf("out %q", got)
	}
	_, err = ExecRunner{}.Run(context.Background(), dir, "fail")
	if err == nil || err.Error() != "not logged in" {
		t.Errorf("err %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	_, err = New(dir).PR(context.Background(), 1)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: ") {
		t.Errorf("missing gh: %v", err)
	}
}

func writeGh(t *testing.T, dir, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// With a short PATH, gh is found in the usual places.
func TestExecRunnerFindsGhOffPath(t *testing.T) {
	bin := t.TempDir()
	writeGh(t, bin, "#!/bin/sh\necho found\n")
	t.Setenv("PATH", t.TempDir())
	out, err := ExecRunner{Dirs: []string{"/no/such/dir", bin}}.Run(context.Background(), t.TempDir(), "x")
	if err != nil || strings.TrimSpace(string(out)) != "found" {
		t.Errorf("out %q err %v", out, err)
	}
	_, err = ExecRunner{Dirs: []string{t.TempDir()}}.Run(context.Background(), t.TempDir(), "x")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v", err)
	}
	// Through the client the text has the "gh: " prefix once (D58).
	c := Client{R: ExecRunner{Dirs: []string{t.TempDir()}}, Dir: t.TempDir()}
	_, err = c.PR(context.Background(), 1)
	if err == nil || !strings.HasPrefix(err.Error(), "gh: program not found") {
		t.Errorf("client err %v, want the prefix %q once", err, "gh: program not found")
	}
}

func TestFindSkipsDirectoryAndPlainFile(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, "gh"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := t.TempDir()
	if err := os.WriteFile(filepath.Join(f, "gh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Find("", []string{d, f}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err %v", err)
	}
}

// A child of gh that holds the output pipe must not keep Run from returning
// after the kill.
func TestExecRunnerKilledGhDoesNotHang(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ExecRunner{Path: "/bin/sh"}.Run(ctx, t.TempDir(), "-c", "sleep 20 & sleep 20")
	if err == nil {
		t.Error("want an error")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("Run took %v", d)
	}
}

func TestBranchPRReadsNumberInDir(t *testing.T) {
	f := &fakeRunner{out: []byte(`{"number":307}`)}
	n, err := Client{R: f, Dir: "/w"}.BranchPR(context.Background())
	if err != nil || n != 307 {
		t.Fatalf("got %d, %v", n, err)
	}
	if f.dir != "/w" || strings.Join(f.args, " ") != "pr view --json number" {
		t.Errorf("dir %q args %v", f.dir, f.args)
	}
}

func TestBranchPRNoPullRequestIsZero(t *testing.T) {
	f := &fakeRunner{err: errors.New(`no pull requests found for branch "x"`)}
	n, err := Client{R: f}.BranchPR(context.Background())
	if err != nil || n != 0 {
		t.Errorf("got %d, %v", n, err)
	}
}

func TestBranchPRErrors(t *testing.T) {
	if _, err := (Client{R: &fakeRunner{err: errors.New("boom")}}).BranchPR(context.Background()); err == nil {
		t.Error("other gh error was hidden")
	}
	if _, err := (Client{R: &fakeRunner{out: []byte("x")}}).BranchPR(context.Background()); err == nil {
		t.Error("bad json was accepted")
	}
}

func TestOpenIssueRunsWeb(t *testing.T) {
	f := &fakeRunner{}
	if err := (Client{R: f, Dir: "/w"}).OpenIssue(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.args, " ") != "issue view 12 --web" || f.dir != "/w" {
		t.Errorf("dir %q args %v", f.dir, f.args)
	}
}
