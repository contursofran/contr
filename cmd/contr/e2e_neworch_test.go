//go:build e2e

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
	"github.com/contursofran/contr/internal/app/sidebar"
)

// TestPlusNewOutsideGit presses `+ new` with a folder outside git (D233): the
// app asks, makes the folder a git repository with a first commit, starts the
// orchestrator there and selects it, so its tab shows.
//
//	go test -tags e2e -run TestPlusNewOutsideGit ./cmd/contr
func TestPlusNewOutsideGit(t *testing.T) {
	h := newHarness(t)
	dir := seeNonGit(t, h)
	// The scratch home has no git identity; the commit needs one.
	if err := os.WriteFile(filepath.Join(h.home, ".gitconfig"), []byte("[user]\n\tname = demo\n\temail = demo@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.theme(t, "dark")
	c := h.launch(t)
	c.waitLabel(t, "lead, ")
	c.press(t, sidebar.LabelProjectsAdd)
	c.press(t, sidebar.LabelProjectsNew)
	c.waitLabel(t, app.LabelMakeRepo)
	c.capture(t, filepath.Join(h.out, "neworch-ask.png"))
	c.press(t, app.LabelMakeRepo)
	row := c.waitLabel(t, "orch-notes-")
	name, _, _ := strings.Cut(row, ",")
	deadline := time.Now().Add(20 * time.Second)
	for !c.labels(t)[name] {
		if time.Now().After(deadline) {
			t.Fatalf("no tab %q: the new orchestrator is not selected; labels %v", name, keys(c.labels(t)))
		}
		time.Sleep(100 * time.Millisecond)
	}
	out, err := exec.Command("git", "-C", dir, "log", "--format=%s").Output()
	if err != nil || strings.TrimSpace(string(out)) != "Initial commit" {
		t.Errorf("git log %q, %v; want one commit \"Initial commit\"", out, err)
	}
}
