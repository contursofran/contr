package agentstate_test

// The needs tests use apptest.Source, and apptest imports agentstate, so they
// are an outside test package.

import (
	"slices"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
	"github.com/contursofran/contr/internal/app/apptest"
	"github.com/contursofran/contr/internal/app/icons"
	"github.com/contursofran/contr/internal/owner"
)

func sampleSource() *apptest.Source {
	return &apptest.Source{Snap: agentstate.Snapshot{
		Workers: []agents.Worker{
			{Name: "b-1", Project: "a", Status: "working"},
			apptest.BlockedWorker("b-2", "a", "Allow rm?"),
			apptest.BlockedWorker("b-3", "b", "Allow git push?"),
		},
		Questions: []owner.Question{
			{ID: "D1", Project: "a", Title: "Which db?"},
			{ID: "D2", Project: "b", Question: "Ship\nit?"},
		},
		Todos: []owner.Todo{
			{ID: "T1", Project: "a", Kind: "do", Text: "Test the bag"},
			{ID: "T2", Project: "b", Kind: "know", Text: "CI is slow"},
		},
	}}
}

func TestNeedItemsOrderBlockedThenQuestionsThenTodos(t *testing.T) {
	tabs := agentstate.NeedItems(sampleSource().Snapshot, nil)()
	got := apptest.Keys(tabs)
	want := []string{"b-2", "b-3", "D1", "D2", "T1", "T2"}
	if !slices.Equal(got, want) {
		t.Fatalf("tabs %v, want %v", got, want)
	}
	if tabs[0].Icon != icons.Blocked || tabs[2].Icon != icons.Decide || tabs[4].Icon != icons.Do || tabs[5].Icon != icons.Fyi {
		t.Errorf("icons wrong: %+v", tabs)
	}
	if tabs[3].Title != "Ship it?" {
		t.Errorf("title %q, want one line", tabs[3].Title)
	}
}

func TestNeedItemsProjectFilter(t *testing.T) {
	proj := "a"
	items := agentstate.NeedItems(sampleSource().Snapshot, func() string { return proj })
	if got, want := apptest.Keys(items()), []string{"b-2", "D1", "T1"}; !slices.Equal(got, want) {
		t.Fatalf("project a: %v, want %v", got, want)
	}
	proj = ""
	if len(items()) != 6 {
		t.Errorf("no scope should show all, got %v", apptest.Keys(items()))
	}
}

func TestNeedItemsWithoutASourceListNothing(t *testing.T) {
	if got := agentstate.NeedItems(nil, nil)(); len(got) != 0 {
		t.Fatalf("nil source gave %v", apptest.Keys(got))
	}
	if got := agentstate.NeedItems((&apptest.Source{}).Snapshot, nil)(); len(got) != 0 {
		t.Fatalf("empty state gave %v", apptest.Keys(got))
	}
}

func TestNeedItemsMarkResolvedItems(t *testing.T) {
	s := sampleSource()
	s.Snap.Resolved = []string{"D1"}
	for _, tab := range agentstate.NeedItems(s.Snapshot, nil)() {
		if tab.Resolved != (tab.Key == "D1") {
			t.Errorf("%s resolved = %v", tab.Key, tab.Resolved)
		}
	}
}

// Each item carries when it came: Asked, Pinned or the worker's last output (D258).
func TestNeedItemsCarryTheirTime(t *testing.T) {
	asked, pinned, out := time.Unix(100, 0), time.Unix(200, 0), time.Unix(300, 0)
	w := apptest.BlockedWorker("b-2", "a", "Allow rm?")
	w.LastOutput = out
	snap := agentstate.Snapshot{
		Workers:   []agents.Worker{w},
		Questions: []owner.Question{{ID: "D1", Project: "a", Title: "Which db?", Asked: asked}},
		Todos:     []owner.Todo{{ID: "T1", Project: "a", Kind: "do", Text: "Test", Pinned: pinned}},
	}
	got := map[string]time.Time{}
	for _, tab := range agentstate.NeedItemsOf(snap, "a") {
		got[tab.Key] = tab.Since
	}
	if !got["b-2"].Equal(out) || !got["D1"].Equal(asked) || !got["T1"].Equal(pinned) {
		t.Errorf("times %v, want b-2 %v, D1 %v, T1 %v", got, out, asked, pinned)
	}
}
