package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/chatview"
	"github.com/contursofran/contr/internal/app/ghsource"
	"github.com/contursofran/contr/internal/app/ghsource/ghsourcetest"
	"github.com/contursofran/contr/internal/app/state"
	"github.com/contursofran/contr/internal/app/view"
)

// prRunner answers each gh read of a number with out, and counts the reads.
type prRunner struct {
	out   string
	calls int
}

func (r *prRunner) Run(context.Context, string, ...string) ([]byte, error) {
	r.calls++
	return []byte(r.out), nil
}

// chipsRig is a frame whose session "lead" works in dir, with a worker b-649 and the gh reads
// and file checks queued in queue.
type chipsRig struct {
	f       *Frame
	s       *chatChips
	dir     string
	queue   []func()
	runner  *prRunner
	redraws int
}

func newChipsRig(t *testing.T) *chipsRig {
	t.Helper()
	r := &chipsRig{dir: t.TempDir(), runner: &prRunner{out: `{"number":667,"state":"MERGED","statusCheckRollup":[]}`}}
	env := view.Env{Run: view.Runner{Go: func(fn func()) { r.queue = append(r.queue, fn) }}, Invalidate: func() { r.redraws++ }}
	r.f = NewFrame(Deps{})
	r.f.WorkerPRs = &ghsource.WorkerPRs{Env: env, GH: &ghsource.GHQueue{}}
	r.f.Selection = stateSelection()
	r.f.snap = agentstate.Snapshot{
		Orchestrators: []agents.Worker{{Name: "lead", Address: "o1", Role: agents.RoleOrchestrator, Worktree: r.dir, Live: true}},
		Workers: []agents.Worker{
			{Name: "b-649", Address: "t0", Worktree: "/wt/old"},
			{Name: "b-649", Address: "t1", Worktree: "/wt/b", Live: true, Status: "blocked"},
		},
	}
	r.f.byAddr = map[string]agents.Worker{"o1": r.f.snap.Orchestrators[0]}
	r.s = newChatChips(r.f, env)
	r.s.refs.Client = apptest.ClientOf(r.runner)
	return r
}

func (r *chipsRig) flush() {
	for len(r.queue) > 0 {
		fn := r.queue[0]
		r.queue = r.queue[1:]
		fn()
	}
}

func (r *chipsRig) look(kind chatview.TokenKind, name string) (chatview.Chip, bool) {
	return r.s.Look("o1", chatview.Token{Kind: kind, Name: name})
}

func TestChatChipsKnowWorkersByName(t *testing.T) {
	r := newChipsRig(t)
	if c, ok := r.look(chatview.TokenWord, "b-649"); !ok || c != (chatview.Chip{Kind: chatview.ChipWorker, State: chatview.StateNeedsYou}) {
		t.Errorf("b-649: %v %v, want the live one, which needs you", c, ok)
	}
	if _, ok := r.look(chatview.TokenWord, "b-650"); ok {
		t.Error("an unknown name is a chip")
	}
	if r.f.WorkerPRs.GH != r.s.refs.GH {
		t.Error("the number reads do not share the gh queue")
	}
}

func TestChatChipsKnowNumbersByTheirRead(t *testing.T) {
	r := newChipsRig(t)
	if _, ok := r.look(chatview.TokenNumber, "667"); ok || len(r.queue) != 1 {
		t.Fatalf("known before its read, or %d reads queued", len(r.queue))
	}
	r.flush()
	if c, ok := r.look(chatview.TokenNumber, "667"); !ok || c != (chatview.Chip{Kind: chatview.ChipPR, State: chatview.StateMerged}) {
		t.Errorf("667: %v %v", c, ok)
	}
	if _, ok := r.s.Look("nobody", chatview.Token{Kind: chatview.TokenNumber, Name: "667"}); ok {
		t.Error("a session without a folder knows a number")
	}
	if r.runner.calls != 1 {
		t.Errorf("gh ran %d times, want 1", r.runner.calls)
	}
}

func TestChatChipsKnowAWorkersPRBeforeItsRead(t *testing.T) {
	r := newChipsRig(t)
	prs := ghsourcetest.NewPRSourceRig(map[string]string{"/wt/b": `{"number":656,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`})
	prs.W.Sync(r.f.snap.Workers)
	prs.Flush()
	r.f.WorkerPRs = prs.W
	c, ok := r.look(chatview.TokenNumber, "656")
	if !ok || c != (chatview.Chip{Kind: chatview.ChipPR, State: chatview.StateRunning}) {
		t.Errorf("656: %v %v, want a PR whose checks run", c, ok)
	}
}

func TestChatChipsKnowFilesOfTheSessionFolder(t *testing.T) {
	r := newChipsRig(t)
	if err := os.MkdirAll(filepath.Join(r.dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "docs", "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"docs/a.md", filepath.Join(r.dir, "docs", "a.md"), "docs/b.md", "docs", "../x.md", "/etc/hosts"} {
		r.look(chatview.TokenPath, p)
	}
	r.flush()
	for p, want := range map[string]bool{
		"docs/a.md": true, filepath.Join(r.dir, "docs", "a.md"): true,
		"docs/b.md": false, "docs": false, "../x.md": false, "/etc/hosts": false,
	} {
		if _, ok := r.look(chatview.TokenPath, p); ok != want {
			t.Errorf("%s: a chip %v, want %v", p, ok, want)
		}
	}
	if len(r.queue) != 0 {
		t.Error("a path is read again")
	}
	if r.redraws != 1 {
		t.Errorf("%d redraws, want 1: the two spellings of one file are one read, and a path that is no file changes nothing", r.redraws)
	}
}

func TestChatChipsOpen(t *testing.T) {
	r := newChipsRig(t)
	var opened []string
	r.f.OpenPRWeb = func(d string, n int) error { opened = append(opened, "pr "+d); return nil }
	r.f.OpenIssueWeb = func(d string, n int) error { opened = append(opened, "issue "+d); return errors.New("x") }
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipPR, State: chatview.StateOpen}, "667")
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipIssue}, "649")
	r.flush()
	if want := []string{"pr " + r.dir, "issue " + r.dir}; len(opened) != 2 || opened[0] != want[0] || opened[1] != want[1] {
		t.Errorf("opened %q, want %q", opened, want)
	}
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipWorker}, "b-649")
	if sel, ok := r.f.Selection.Current(); !ok || sel != (state.Selected{Kind: state.KindWorker, ID: "t1"}) {
		t.Errorf("selection %v", sel)
	}
	rv, _ := newTestReview()
	r.f.Review = rv
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipFile}, "docs/a.md")
	if !r.f.ReviewOpen() || rv.Dir() != r.dir {
		t.Errorf("review open %v on %q", r.f.ReviewOpen(), rv.Dir())
	}
	// A name that is gone does nothing.
	r.f.reviewOpen = false
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipWorker}, "b-1")
	r.s.Open("nobody", chatview.Chip{Kind: chatview.ChipFile}, "docs/a.md")
	r.s.Open("o1", chatview.Chip{Kind: chatview.ChipPR}, "x")
	if r.f.ReviewOpen() || len(opened) != 2 {
		t.Error("a name that is gone opened something")
	}
}

// A board reads a worker with the PR of its branch, and a number with the counts of its checks
// (D267), from the data of the chips.
func TestChatChipsGiveBoardData(t *testing.T) {
	r := newChipsRig(t)
	if _, ok := r.s.Worker("o1", "b-650"); ok {
		t.Error("an unknown worker is known")
	}
	w, ok := r.s.Worker("o1", "b-649")
	if !ok || w != (chatview.WorkerInfo{Chip: chatview.Chip{Kind: chatview.ChipWorker, State: chatview.StateNeedsYou}}) {
		t.Errorf("b-649 before its PR is known: %+v %v", w, ok)
	}
	prs := ghsourcetest.NewPRSourceRig(map[string]string{"/wt/b": `{"number":656,"state":"OPEN","statusCheckRollup":[{"status":"IN_PROGRESS"}]}`})
	prs.W.Sync(r.f.snap.Workers)
	prs.Flush()
	r.f.WorkerPRs = prs.W
	if w, _ := r.s.Worker("o1", "b-649"); w.PR != 656 {
		t.Errorf("b-649's PR is %d, want 656", w.PR)
	}
	// Before its read by number, the worker's PR is known without counts.
	ref, ok := r.s.Ref("o1", 656)
	if !ok || ref != (chatview.RefInfo{Chip: chatview.Chip{Kind: chatview.ChipPR, State: chatview.StateRunning}}) {
		t.Errorf("656 before its read: %+v %v", ref, ok)
	}
	r.runner.out = `{"number":656,"state":"OPEN","statusCheckRollup":[{"status":"COMPLETED","conclusion":"SUCCESS"},{"status":"COMPLETED","conclusion":"FAILURE"}]}`
	r.flush()
	ref, ok = r.s.Ref("o1", 656)
	if !ok || ref.Chip.State != chatview.StateFailed || ref.Checks.Pass != 1 || ref.Checks.Fail != 1 || ref.Done {
		t.Errorf("656 after its read: %+v %v", ref, ok)
	}
	if _, ok := r.s.Ref("nobody", 656); ok {
		t.Error("a session without a folder knows a number")
	}
}
