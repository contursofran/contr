package apptest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/contursofran/contr/internal/agents"
	"github.com/contursofran/contr/internal/app/agentstate"
)

// WriteFakeFile writes text to path, and makes its folders.
func WriteFakeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// MainRepo makes a main checkout at root with `.git/HEAD` = head and returns
// the git dir.
func MainRepo(t *testing.T, root, head string) string {
	t.Helper()
	gitDir := filepath.Join(root, ".git")
	WriteFakeFile(t, filepath.Join(gitDir, "HEAD"), head)
	return gitDir
}

// CanonicalTmp is a temporary directory in the canonical form that the scan's
// rows use.
func CanonicalTmp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// SpPane is a live worker named id in the folder cwd, with an hc status
// when status is not nil.
func SpPane(id, cwd string, status *string) agents.Worker {
	w := agents.Worker{Name: id, Address: id, Role: "builder", Worktree: cwd, Live: true}
	if status != nil {
		w.Status = *status
	}
	return w
}

// SpTree is the agent state of a followed core with the workers ws.
func SpTree(ws ...agents.Worker) agentstate.Snapshot {
	return agentstate.Snapshot{Workers: ws, CoreVersion: "0.1.18"}
}

// ForeignTerm is a terminal that contr did not start, in dir.
func ForeignTerm(addr, role, project, dir string) agents.Worker {
	return agents.Worker{Name: addr, Address: addr, Role: role, Project: project, Worktree: dir, Live: true, Foreign: true}
}
