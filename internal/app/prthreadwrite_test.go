package app

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/github"
)

const oneThreadJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
{"id":"T1","isResolved":false,"path":"a.go","line":3,"originalLine":3,"diffSide":"RIGHT",
 "comments":{"nodes":[{"id":"C1","author":{"login":"bob"},"body":"why three?","createdAt":"2026-11-03T10:00:00Z"}]}}
]}}}}}`

const resolvedThreadJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[
{"id":"T1","isResolved":true,"path":"a.go","line":3,"originalLine":3,"diffSide":"RIGHT",
 "comments":{"nodes":[{"id":"C1","author":{"login":"bob"},"body":"why three?","createdAt":"2026-11-03T10:00:00Z"}]}}
]}}}}}`

// writeRig is a Files tab with one open thread. Calls to gh are recorded; a
// mutation fails while failMutation is set; after a mutation the threads read
// returns after.
type writeRig struct {
	*prRig
	mu           sync.Mutex
	calls        []string
	failMutation bool
	after        string
	mutated      bool
}

func newWriteRig(t *testing.T) *writeRig {
	t.Helper()
	w := &writeRig{after: resolvedThreadJSON}
	w.prRig = filesRig(t, prDiffText, oneThreadJSON, nil)
	w.flush()
	w.pane.client = github.Client{Dir: "/w", R: runnerFunc(func(args ...string) ([]byte, error) {
		j := strings.Join(args, " ")
		w.calls = append(w.calls, j)
		switch {
		case strings.Contains(j, "mutation"):
			if w.failMutation {
				return nil, errors.New("boom write")
			}
			w.mutated = true
			return []byte(`{"data":{}}`), nil
		case strings.HasPrefix(j, "repo view"):
			return []byte(`{"owner":{"login":"o"},"name":"n"}`), nil
		case strings.HasPrefix(j, "api graphql"):
			if w.mutated {
				return []byte(w.after), nil
			}
			return []byte(oneThreadJSON), nil
		}
		return []byte(prJSON), nil
	})}
	return w
}

func (w *writeRig) mutations() []string {
	var out []string
	for _, c := range w.calls {
		if strings.Contains(c, "mutation") {
			out = append(out, c)
		}
	}
	return out
}

func TestPRThreadReplySends(t *testing.T) {
	w := newWriteRig(t)
	if err := w.tt.Click(LabelPRReply); err != nil {
		t.Fatal(err)
	}
	w.tt.Frame()
	if err := w.tt.Click(LabelPRReplyBox); err != nil {
		t.Fatal(err)
	}
	w.tt.Type("because two was one short")
	w.tt.Key(0, ui.KeyEnter)
	w.tt.Frame()
	if len(w.queue) == 0 {
		t.Fatal("the write must be queued off the UI thread")
	}
	if len(w.mutations()) != 0 {
		t.Fatal("no call before the runner runs")
	}
	w.flush()
	m := w.mutations()
	if len(m) != 1 || !strings.Contains(m[0], "addPullRequestReviewThreadReply") ||
		!strings.Contains(m[0], "id=T1") || !strings.Contains(m[0], "body=because two was one short") {
		t.Fatalf("mutations %q", m)
	}
	// the threads are read again, and the thread shows as resolved now
	if !w.tt.HasText("resolved · bob") {
		t.Errorf("threads not re-read: %q", w.tt.Texts())
	}
	if w.pane.filesState.replying["T1"] || *w.pane.filesState.draft("T1") != "" {
		t.Error("field should close and clear after a send")
	}
}

func TestPRThreadReplyButtonSends(t *testing.T) {
	w := newWriteRig(t)
	_ = w.tt.Click(LabelPRReply)
	w.tt.Frame()
	_ = w.tt.Click(LabelPRReplyBox)
	w.tt.Type("hi")
	if err := w.tt.Click(LabelPRReplySend); err != nil {
		t.Fatal(err)
	}
	w.flush()
	if len(w.mutations()) != 1 {
		t.Fatalf("mutations %q", w.mutations())
	}
}

func TestPRThreadEmptyReplyDoesNotSend(t *testing.T) {
	w := newWriteRig(t)
	_ = w.tt.Click(LabelPRReply)
	w.tt.Frame()
	_ = w.tt.Click(LabelPRReplySend)
	w.flush()
	if len(w.queue)+len(w.mutations()) != 0 {
		t.Error("an empty reply must not send")
	}
}

func TestPRThreadResolveSends(t *testing.T) {
	w := newWriteRig(t)
	if err := w.tt.Click(LabelPRResolve); err != nil {
		t.Fatal(err)
	}
	w.flush()
	m := w.mutations()
	if len(m) != 1 || !strings.Contains(m[0], "resolveReviewThread") || !strings.Contains(m[0], "id=T1") {
		t.Fatalf("mutations %q", m)
	}
	if !w.tt.HasText("resolved · bob") || hasExact(w.tt, LabelPRResolve) {
		t.Errorf("thread should collapse after the re-read: %q", w.tt.Texts())
	}
}

func TestPRThreadWriteError(t *testing.T) {
	w := newWriteRig(t)
	w.failMutation = true
	_ = w.tt.Click(LabelPRReply)
	w.tt.Frame()
	_ = w.tt.Click(LabelPRReplyBox)
	w.tt.Type("keep me")
	w.tt.Key(0, ui.KeyEnter)
	w.flush()
	if !w.tt.HasText("boom write") {
		t.Errorf("error should show in the pane: %q", w.tt.Texts())
	}
	if got := *w.pane.filesState.draft("T1"); got != "keep me" || !w.pane.filesState.replying["T1"] {
		t.Errorf("typed text and field must stay, got %q", got)
	}
	// a retry works
	w.failMutation = false
	_ = w.tt.Click(LabelPRReplySend)
	w.flush()
	if len(w.mutations()) != 2 || w.tt.HasText("boom write") {
		t.Errorf("retry: mutations %q, texts %q", w.mutations(), w.tt.Texts())
	}
}

func TestPRThreadButtonsStopWhileWriting(t *testing.T) {
	w := newWriteRig(t)
	_ = w.tt.Click(LabelPRResolve)
	w.tt.Frame()
	_ = w.tt.Click(LabelPRResolve) // second click while the first runs
	w.flush()
	if len(w.mutations()) != 1 {
		t.Errorf("one write at a time, got %q", w.mutations())
	}
}

func TestPRThreadResolvedHasNoButtons(t *testing.T) {
	r := filesRig(t, prDiffText, resolvedThreadJSON, nil)
	r.flush()
	if hasExact(r.tt, LabelPRReply) || hasExact(r.tt, LabelPRResolve) {
		t.Errorf("resolved thread has no buttons: %q", r.tt.Texts())
	}
}

// hasExact tells whether a text equals s (HasText matches a part).
func hasExact(tt *ui.Tester, s string) bool {
	for _, x := range tt.Texts() {
		if x == s {
			return true
		}
	}
	return false
}
