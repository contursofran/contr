package hctest

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHolderPIDs(t *testing.T) {
	ps := `  10 /u/.local/bin/hc hold --socket /tmp/scr1/ru/a.sock
  11 /u/.local/bin/hc hold --socket /tmp/scr10/ru/a.sock
  12 /u/.local/bin/hc hold --socket /tmp/other/a.sock
  13 /u/.local/bin/hc server run
  14 vim hc hold --socket /tmp/scr1/x
  15 hc hold --socket /tmp/scr1/ru/b.sock
`
	got := holderPIDs(ps, []string{"/tmp/scr1/"})
	if want := []int{10, 15}; !reflect.DeepEqual(got, want) {
		t.Errorf("pids = %v, want %v", got, want)
	}
}

// fakeHold starts a process whose command line is `<dir>/hc hold --socket sock`.
func fakeHold(t *testing.T, bin, sock string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, "hold", "--socket", sock)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

func TestKillHoldersEndsOnlyScratchHolders(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "hc")
	// No exec: the shell keeps the command line "<root>/hc hold --socket ...".
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(root, "scratch")
	mine := fakeHold(t, bin, filepath.Join(scratch, "a.sock"))
	theirs := fakeHold(t, bin, filepath.Join(root, "other", "a.sock"))

	var n int
	var err error
	// The process table may lag the start by a moment.
	for i := 0; i < 50 && n == 0; i++ {
		n, err = KillHolders(scratch)
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || n != 1 {
		t.Fatalf("KillHolders = %d, %v; want 1", n, err)
	}
	done := make(chan error, 1)
	go func() { done <- mine.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("scratch holder still running")
	}
	if theirs.ProcessState != nil {
		t.Error("holder outside the scratch folder ended")
	}
}
