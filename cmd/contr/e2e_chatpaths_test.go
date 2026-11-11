//go:build e2e

package main

import (
	"testing"
	"time"
)

// TestChatPathPopup is #708 (D279): "@" in the composer lists the paths of lead's repo, Tab on a
// folder puts it in and keeps the popup open on its content, Tab on a file closes it.
func TestChatPathPopup(t *testing.T) {
	h := newHarness(t)
	seeChatPaths(t, h)
	c := h.launch(t)
	c.waitLabel(t, "lead, ")
	c.press(t, "Chat")
	c.text(t, "Look at @app")
	c.waitLabel(t, "@internal/app/")
	c.waitLabel(t, "@wrapper.go")
	if c.hasPrefix(t, "@internal/hc/") || c.hasPrefix(t, "@HANDOFF.md") {
		t.Fatal("the popup shows a path without app, or a deleted file")
	}
	c.key(t, "Tab")
	gone(t, c, "@wrapper.go", "the popup does not show the folder's content after Tab on it")
	c.waitLabel(t, "@internal/app/tabs.go")
	c.text(t, "tab")
	gone(t, c, "@internal/app/panel.go", "the popup does not filter in the folder")
	c.key(t, "Tab")
	gone(t, c, "suggestions", "the popup stays open after Tab on a file")
}

// gone waits until no element has label, and fails with msg after 10 s.
func gone(t *testing.T, c *client, label, msg string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for c.labels(t)[label] {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
