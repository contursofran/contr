//go:build e2e

package main

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// frontmostApp returns the name of the frontmost app, "" when it cannot tell.
func frontmostApp() string {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first process whose frontmost is true`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// guardFocus fails the test when the app under test becomes the frontmost app
// (D148): an e2e run must not take the focus from the owner. It checks at
// once and then every second until the test ends. The app that was in front
// before may change, since the owner may switch apps; only contr taking the
// front is an error. CONTR_E2E_FOREGROUND=1 turns the guard off.
func guardFocus(t *testing.T) {
	t.Helper()
	if foreground() {
		return
	}
	before := frontmostApp()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	check := func() {
		if now := frontmostApp(); now == "contr-e2e" {
			t.Errorf("the app took the focus: %q was in front, now %q", before, now)
		}
	}
	check()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
				check()
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
		check()
	})
}
