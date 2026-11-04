package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/github"
)

// ghFake answers gh calls: pr view, api user and the writes. It records argv.
type ghFake struct {
	mu     sync.Mutex
	calls  [][]string
	pr     string // JSON of pr view
	user   string
	failOn string // a write verb that fails: comment, review or merge
	prs    int    // count of pr view reads
}

func (g *ghFake) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, slices.Clone(args))
	switch {
	case args[0] == "api" && args[1] == "user":
		return []byte(g.user + "\n"), nil
	case args[0] == "pr" && args[1] == "view":
		g.prs++
		return []byte(g.pr), nil
	case args[0] == "pr" && args[1] == g.failOn:
		return nil, errors.New("gh: refused")
	}
	return nil, nil
}

func (g *ghFake) writes() [][]string {
	var w [][]string
	for _, c := range g.calls {
		if c[0] == "pr" && c[1] != "view" {
			w = append(w, c)
		}
	}
	return w
}

func footerRig(t *testing.T, mutate func(string) string) (*prRig, *ghFake) {
	t.Helper()
	r := newPRRig(t)
	pr := prJSON
	if mutate != nil {
		pr = mutate(pr)
	}
	g := &ghFake{pr: pr, user: "owner"}
	r.pane.Client = func(dir string) github.Client { return github.Client{R: g, Dir: dir} }
	r.pane.Open("/w", 642)
	r.tt.Frame()
	r.flush()
	return r, g
}

const runningCheck = `{"status":"IN_PROGRESS","conclusion":""}`

func pending(s string) string {
	return strings.Replace(s, `{"status":"COMPLETED","conclusion":"SUCCESS"}`, runningCheck, 1)
}

func failed(s string) string {
	return strings.Replace(s, `{"status":"COMPLETED","conclusion":"SUCCESS"}`, `{"status":"COMPLETED","conclusion":"FAILURE"}`, 1)
}

func TestPRFooterMergeWaitsWhileCIRuns(t *testing.T) {
	r, g := footerRig(t, pending)
	if r.tt.HasText(LabelPRMerge) || !r.tt.HasText(LabelPRMergeWait) {
		t.Errorf("texts %q", r.tt.Texts())
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	r.flush()
	if len(g.writes()) != 0 {
		t.Errorf("merge ran while CI runs: %v", g.writes())
	}
}

func TestPRFooterMergeShownWhenGreen(t *testing.T) {
	r, _ := footerRig(t, nil)
	if !r.tt.HasText(LabelPRMerge) || r.tt.HasText(LabelPRMergeWait) {
		t.Errorf("texts %q", r.tt.Texts())
	}
}

func TestPRFooterMergeWhenFailed(t *testing.T) {
	r, g := footerRig(t, failed)
	if !r.tt.HasText(LabelPRMerge) || !r.tt.HasText("CI failing · 1 of 3") {
		t.Fatalf("texts %q", r.tt.Texts())
	}
	if err := r.tt.Click(LabelPRMerge); err != nil {
		t.Fatal(err)
	}
	r.flush()
	if len(g.writes()) != 1 {
		t.Errorf("writes %v", g.writes())
	}
	if wait, red := ciGate(github.CheckSummary{Fail: 1}); wait || !red {
		t.Error("failed CI must not wait and must show red")
	}
	if ciTextColor(github.CheckSummary{Fail: 1}) != theme.DiffDeleted || ciTextColor(github.CheckSummary{Pass: 1}) != theme.Text2 {
		t.Error("CI text colour")
	}
}

func TestPRFooterApproveHiddenOnOwnPR(t *testing.T) {
	r, _ := footerRig(t, func(s string) string { return strings.Replace(s, `"login":"b-1140"}`, `"login":"owner"}`, 1) })
	if r.tt.HasText(LabelPRApprove) {
		t.Errorf("Approve shown on the owner's PR: %q", r.tt.Texts())
	}
	r2, _ := footerRig(t, nil)
	if !r2.tt.HasText(LabelPRApprove) {
		t.Errorf("Approve missing: %q", r2.tt.Texts())
	}
}

func TestPRFooterApproveHiddenWhenApproved(t *testing.T) {
	r, _ := footerRig(t, func(s string) string {
		return strings.Replace(s, `"isDraft"`, `"reviewDecision":"APPROVED","isDraft"`, 1)
	})
	if r.tt.HasText(LabelPRApprove) {
		t.Errorf("Approve shown on an approved PR: %q", r.tt.Texts())
	}
}

func TestPRFooterNoFooterWhenMerged(t *testing.T) {
	r, _ := footerRig(t, func(s string) string { return strings.Replace(s, `"state":"OPEN"`, `"state":"MERGED"`, 1) })
	if r.tt.HasText(LabelPRMerge) || r.tt.HasText(LabelPRComment) {
		t.Errorf("footer on a merged PR: %q", r.tt.Texts())
	}
}

func TestPRFooterApproveWritesAndRereads(t *testing.T) {
	r, g := footerRig(t, nil)
	before := g.prs
	if err := r.tt.Click(LabelPRApprove); err != nil {
		t.Fatal(err)
	}
	g.pr = strings.Replace(g.pr, "Fix bag drag target", "Fixed title", 1)
	r.flush()
	if !r.tt.HasText("Fixed title") {
		t.Errorf("pane did not show the re-read PR: %q", r.tt.Texts())
	}
	if w := g.writes(); len(w) != 1 || !slices.Equal(w[0], []string{"pr", "review", "642", "--approve"}) {
		t.Errorf("writes %v", w)
	}
	if g.prs != before+1 {
		t.Errorf("PR reads %d, want %d", g.prs, before+1)
	}
}

func TestPRFooterCommentSendsAndClears(t *testing.T) {
	r, g := footerRig(t, nil)
	if err := r.tt.Click(LabelPRComment); err != nil {
		t.Fatal(err)
	}
	r.flush()
	if len(g.writes()) != 0 {
		t.Fatalf("empty comment was sent: %v", g.writes())
	}
	r.pane.ft.text = "looks good"
	r.tt.Frame()
	if err := r.tt.Click(LabelPRComment); err != nil {
		t.Fatal(err)
	}
	r.flush()
	if w := g.writes(); len(w) != 1 || !slices.Equal(w[0], []string{"pr", "comment", "642", "--body", "looks good"}) {
		t.Errorf("writes %v", w)
	}
	if r.pane.ft.text != "" {
		t.Errorf("field not cleared: %q", r.pane.ft.text)
	}
}

func TestPRFooterWriteErrorShown(t *testing.T) {
	r, g := footerRig(t, nil)
	g.failOn = "review"
	before := g.prs
	if err := r.tt.Click(LabelPRApprove); err != nil {
		t.Fatal(err)
	}
	r.flush()
	if !r.tt.HasText("gh: refused") {
		t.Errorf("error not shown: %q", r.tt.Texts())
	}
	if g.prs != before {
		t.Error("PR re-read after a failed write")
	}
	if r.pane.Loaded() == nil {
		t.Error("pane lost the PR")
	}
}

func TestPRFooterBusyDisablesButtons(t *testing.T) {
	r, g := footerRig(t, nil)
	if err := r.tt.Click(LabelPRApprove); err != nil {
		t.Fatal(err)
	}
	r.tt.Frame() // the write is queued, not run
	_ = r.tt.Click(LabelPRMerge)
	r.flush()
	if w := g.writes(); len(w) != 1 {
		t.Errorf("a second write started while busy: %v", w)
	}
}

func TestPRFooterMergeUsesHeadSHA(t *testing.T) {
	r, g := footerRig(t, nil)
	if err := r.tt.Click(LabelPRMerge); err != nil {
		t.Fatal(err)
	}
	r.flush()
	want := []string{"pr", "merge", "642", "--squash", "--match-head-commit", "abc"}
	if w := g.writes(); len(w) != 1 || !slices.Equal(w[0], want) {
		t.Errorf("writes %v, want %v", w, want)
	}
}

func TestPRFooterKeys(t *testing.T) {
	r, g := footerRig(t, nil)
	r.tt.Key(0, ui.KeyC)
	r.tt.Frame()
	r.tt.Type("hi")
	if r.pane.ft.text != "hi" {
		t.Errorf("c did not focus the field: text %q", r.pane.ft.text)
	}
	r.tt.TypeKey(0, ui.KeyC, "c") // a c typed in the field is text
	if r.pane.ft.text != "hic" {
		t.Errorf("c typed in the field: text %q", r.pane.ft.text)
	}
	r.tt.Key(ui.Cmd|ui.Shift, ui.KeyM)
	r.flush()
	if w := g.writes(); len(w) != 1 || w[0][1] != "merge" {
		t.Errorf("writes %v", w)
	}
}

func TestPRFooterStaleWriteDropped(t *testing.T) {
	r, _ := footerRig(t, nil)
	if err := r.tt.Click(LabelPRApprove); err != nil {
		t.Fatal(err)
	}
	r.pane.Close()
	r.flush()
	if r.pane.Loaded() != nil || r.pane.ft.err != "" {
		t.Error("a write result landed after the pane closed")
	}
}
