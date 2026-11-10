package app

import (
	"fmt"
	"os"
	"testing"
)

// TestMain gives the tests a state folder of their own, so that no view of an attached frame
// reads or watches the owner's session files (the chat source makes and watches its folder).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "contr-app-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Setenv("CONTR_STATE_DIR", dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
