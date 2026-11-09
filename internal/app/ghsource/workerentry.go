package ghsource

// The entry points from a worker to its pull request (D106). A worker with a pull request opens
// it in the PR pane; a worker with only an issue opens the issue in the browser (D74).

import (
	"strconv"
	"strings"

	"github.com/contursofran/contr/internal/agents"
)

// WorkerOpenPR and WorkerOpenIssue are the labels of the buttons that open a worker's pull
// request or issue, followed by the number.
const (
	WorkerOpenPR    = "Open PR"
	WorkerOpenIssue = "Open issue"
)

// WorkerEntry opens the pull request or the issue of a worker. The zero value opens nothing.
type WorkerEntry struct {
	// PRs gives each worker's pull request number (D104). May be nil.
	PRs *WorkerPRs
	// OpenPR shows a pull request in the PR pane, with the worktree and the number.
	OpenPR func(dir string, n int)
	// OpenIssue opens an issue in the browser, with the worktree and the number.
	OpenIssue func(dir string, n int)
	// OpenPRWeb opens a pull request in the browser, with the worktree and the number. The agent
	// rows use it: they never open the PR pane (D127). It must not block the UI thread.
	OpenPRWeb func(dir string, n int)
}

// For returns the label and the action for worker w: its pull request when it has one, else its
// issue. The label is empty and the action nil when the worker has neither.
func (e WorkerEntry) For(w agents.Worker) (string, func()) {
	if w.Worktree == "" {
		return "", nil
	}
	if n := e.PRs.PR(w.Worktree); n > 0 && e.OpenPR != nil {
		return WorkerOpenPR + " #" + strconv.Itoa(n), func() { e.OpenPR(w.Worktree, n) }
	}
	if n, ok := issueNumber(w.Issue); ok && e.OpenIssue != nil {
		return WorkerOpenIssue + " #" + strconv.Itoa(n), func() { e.OpenIssue(w.Worktree, n) }
	}
	return "", nil
}

// issueNumber reads a worker's issue field ("268" or "#268").
func issueNumber(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	return n, err == nil && n > 0
}

// ItemOf is the item a pull request is for: the issue of the worker in dir, as "#N". It is ""
// when no worker works in dir or the worker has no issue.
func ItemOf(workers []agents.Worker, dir string) string {
	for _, w := range workers {
		if w.Worktree == dir && w.Worktree != "" {
			if n, ok := issueNumber(w.Issue); ok {
				return "#" + strconv.Itoa(n)
			}
		}
	}
	return ""
}
