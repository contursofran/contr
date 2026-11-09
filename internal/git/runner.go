package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Runner runs git in dir and gives its output. Tests use a fake.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// InputRunner is a Runner that can also give git a standard input.
type InputRunner interface {
	Runner
	RunInput(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error)
}

// Error is a git command that failed.
type Error struct {
	Args   []string
	Err    error  // the exit error, or the error of ctx when ctx ended
	Stderr string // trimmed
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, e.Stderr)
}

func (e *Error) Unwrap() error { return e.Err }

// ExecRunner runs the `git` command. An empty dir runs it in the current
// folder.
type ExecRunner struct{}

// Run runs `git args...` in dir. It stops when ctx ends.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.RunInput(ctx, dir, nil, args...)
}

// RunInput runs `git args...` in dir with stdin as its standard input. It
// stops when ctx ends; the error then wraps the error of ctx. On an error, the
// output is what git wrote before it ended.
func (ExecRunner) RunInput(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	// A child that keeps the pipes open does not hold Wait after a stop.
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return out, &Error{Args: args, Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return out, nil
}
