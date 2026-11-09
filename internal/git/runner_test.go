package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is missing")
	}
}

// RunInput gives git its standard input.
func TestExecRunnerRunInputGivesStdin(t *testing.T) {
	needGit(t)
	out, err := ExecRunner{}.RunInput(context.Background(), t.TempDir(), strings.NewReader("hello\n"), "hash-object", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("hash %q", got)
	}
}

// A failed command gives an *Error with the args, the exit error and stderr.
func TestExecRunnerErrorHasArgsAndStderr(t *testing.T) {
	needGit(t)
	_, err := ExecRunner{}.Run(context.Background(), t.TempDir(), "no-such-command")
	var ge *Error
	if !errors.As(err, &ge) {
		t.Fatalf("error %T %v, want *Error", err, err)
	}
	if strings.Join(ge.Args, " ") != "no-such-command" || ge.Stderr == "" || ge.Err == nil {
		t.Fatalf("error %+v", ge)
	}
	if !strings.HasPrefix(err.Error(), "git no-such-command: ") || !strings.Contains(err.Error(), ge.Stderr) {
		t.Fatalf("message %q", err.Error())
	}
}

// A command that runs past the end of ctx stops, and the error is the
// error of ctx.
func TestExecRunnerStopsWhenContextEnds(t *testing.T) {
	needGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	// `git hash-object --stdin` waits for its input, which never ends.
	r, w := ioPipe(t)
	defer w.Close()
	_, err := ExecRunner{}.RunInput(ctx, t.TempDir(), r, "hash-object", "--stdin")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error %v, want context.DeadlineExceeded", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("git did not stop")
	}
}

func ioPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, w
}
