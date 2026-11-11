//go:build e2e

package main

import (
	"testing"
	"time"
)

// TestChatCallDetail: a click on a call line in the open work group shows its detail, and a
// second click hides it (#709, D274).
//
//	go test -tags e2e -run TestChatCallDetail ./cmd/contr
func TestChatCallDetail(t *testing.T) {
	h := newHarness(t)
	seeChatTools(t, h)
	c := h.launch(t)
	c.waitLabel(t, "lead, ")
	c.settle(t)
	c.press(t, "Chat")
	c.waitLabel(t, "work group 12 tool calls")
	c.press(t, "work group 12 tool calls")
	c.waitLabel(t, "tool call Bash")
	c.settle(t)
	c.press(t, "tool call Bash")
	c.waitLabel(t, "call detail Bash")
	c.press(t, "tool call Bash")
	deadline := time.Now().Add(5 * time.Second)
	for c.labels(t)["call detail Bash"] {
		if time.Now().After(deadline) {
			t.Fatal("the call detail shows after a second click")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
