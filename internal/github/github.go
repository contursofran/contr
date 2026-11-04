// Package github reads GitHub pull requests through the gh command line.
// gh holds the owner's login, so contr keeps no token (D58).
package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout is the time limit of one gh call.
const DefaultTimeout = 15 * time.Second

// Runner runs gh with args in dir and returns its standard output. On
// failure the error text is the stderr text of gh.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// Client reads from the repository of the folder Dir.
type Client struct {
	R   Runner
	Dir string
	// Timeout limits one call. Zero means DefaultTimeout.
	Timeout time.Duration
}

// New returns a Client that runs the real gh in dir.
func New(dir string) Client { return Client{R: ExecRunner{}, Dir: dir} }

// run calls gh with a time limit. Errors start with "gh: ".
func (c Client) run(ctx context.Context, args ...string) ([]byte, error) {
	if c.R == nil {
		return nil, errors.New("gh: no runner")
	}
	d := c.Timeout
	if d <= 0 {
		d = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	out, err := c.R.Run(ctx, c.Dir, args...)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gh: %s: %w", strings.Join(args[:min(2, len(args))], " "), ctx.Err())
		}
		return nil, fmt.Errorf("gh: %w", err)
	}
	return out, nil
}

// ExecRunner runs the gh program found on PATH.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
