//go:build e2e

package main

import (
	"testing"
	"time"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/questions"
)

// TestQuestionsCardFromTheRow runs the built app against a
// scratch hc core with an orchestrator and a worker in a scratch repository.
// It opens the questions card from the notch of the orchestrator, since the
// needs-you row is gone (D146) (tmp/e2e/answer-card.png):
//
//	go test -tags e2e -run TestQuestionsCardFromTheRow ./cmd/contr
//
// It never touches the owner's hc core.
func TestQuestionsCardFromTheRow(t *testing.T) {
	h := newHarness(t)
	repo := h.repo(t, "demo", "main")
	h.spawn(t,
		agents.Spec{Name: "lead", Role: agents.RoleOrchestrator, Project: "demo", Worktree: repo},
		agents.Spec{Name: "b162-s5-card", Role: "builder", Project: "demo", Issue: "162", Worktree: repo},
	)
	h.owner(t, `{"questions":[
{"id":"D1","project":"demo","title":"Which database for the cache?","question":"The cache needs a store. Redis is fast but one more service; sqlite is already in the app. Which one should it use?","options":["sqlite","redis","files"],"recommended":0,"asked":"2026-11-03T10:00:00Z"}],
"todos":[{"id":"T1","project":"demo","kind":"do","text":"Test the bag drag on the Mac","pinned":"2026-11-03T10:02:00Z"}],
"next_question":2,"next_todo":2}`)
	c := h.launch(t)
	c.press(t, c.waitLabel(t, "lead, "))
	time.Sleep(1500 * time.Millisecond) // the terminal attaches
	// One question and one to-do of "demo".
	const notch = "2 items. Open"
	c.waitLabel(t, notch)
	c.press(t, notch)
	if !c.labels(t)[questions.LabelQuestionsField] {
		t.Fatalf("the questions card did not open; labels %v", keys(c.labels(t)))
	}
	h.capture(t, c, "answer-card.png")
}
