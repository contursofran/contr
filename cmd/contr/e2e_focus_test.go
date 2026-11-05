//go:build e2e

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/contursofran/contr/internal/app"
)

// frontmostApp returns the name of the frontmost app; ok is false when
// osascript fails.
func frontmostApp() (name string, ok bool) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first process whose frontmost is true`).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// guardFocus fails the test when the app under test becomes the frontmost app
// (D148): an e2e run must not take the focus from the owner. The app that was
// in front before may change, since the owner may switch apps; only contr
// taking the front is an error. CONTR_E2E_FOREGROUND=1 turns the guard off.
//
// Two checks run every 200 ms until the test ends. osascript names the
// frontmost app; it needs the Automation permission, and when it fails the
// guard logs it once and relies on the second check. The second check asks the
// app itself through automation (a connection of its own, opened by
// guardActive once the socket is up): no window of the app may be focused.
func guardFocus(t *testing.T) {
	t.Helper()
	if foreground() {
		return
	}
	before, ok := frontmostApp()
	if !ok {
		t.Log("focus guard: osascript cannot read the frontmost app (no Automation permission?); " +
			"only the check through the app's own windows is on")
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	check := func() {
		if now, ok := frontmostApp(); ok && now == "contr-e2e" {
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
			case <-time.After(200 * time.Millisecond):
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

// guardActive fails the test when a window of the app is focused. It reads the
// "windows" list of the automation plugin on its own connection every 50 ms,
// so a short activation shows even when osascript is slow or not allowed.
func guardActive(t *testing.T, sock string) {
	t.Helper()
	if foreground() {
		return
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Logf("focus guard: no second automation connection: %v", err)
		return
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r := bufio.NewReaderSize(conn, 1<<20)
		for id := 1; ; id++ {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
			req := fmt.Sprintf(`{"id": %d, "method": "windows", "params": {}}`+"\n", id)
			if _, err := conn.Write([]byte(req)); err != nil {
				return
			}
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var resp struct {
				Result struct{ Windows []struct{ Focused bool } }
			}
			if json.Unmarshal(line, &resp) != nil {
				continue
			}
			for _, w := range resp.Result.Windows {
				if w.Focused {
					t.Errorf("the app took the focus: one of its windows is focused")
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = conn.Close()
		wg.Wait()
	})
}

// skipWhileViewersHidden skips a test of the board, the worker detail or the
// PR viewer while the hidden-viewers switch (D147, `viewersEnabled` in
// internal/app/hidden.go) is off.
func skipWhileViewersHidden(t *testing.T) {
	t.Helper()
	if !app.ViewersEnabled() {
		t.Skip("the board, worker detail and PR viewer are hidden (D147: viewersEnabled is false)")
	}
}
