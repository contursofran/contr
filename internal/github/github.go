// Package github reads GitHub pull requests through the gh command line.
// gh holds the owner's login, so contr keeps no token (D58).
package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// ErrNotFound is the error of Find when gh is on no searched path.
var ErrNotFound = errors.New("gh: program not found on PATH or in ~/.local/bin, /opt/homebrew/bin, /usr/local/bin")

// waitDelay is how long Run waits for the pipes of gh after the kill. A child
// of gh (git, a pager) can keep them open.
const waitDelay = 2 * time.Second

// Find returns gh from PATH, else the first gh in the usual places. An app
// opened from Finder has only the short launchd PATH, which lacks them. home
// is the home folder; dirs, when not nil, replaces the usual places.
func Find(home string, dirs []string) (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	if dirs == nil {
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}
		if home != "" {
			dirs = append([]string{filepath.Join(home, ".local", "bin")}, dirs...)
		}
	}
	for _, d := range dirs {
		p := filepath.Join(d, "gh")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// ExecRunner runs the gh program found with Find.
type ExecRunner struct {
	// Path, when not empty, is the program to run instead of gh.
	Path string
	// Dirs, when not nil, replaces the usual places that Find searches.
	Dirs []string
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	path := r.Path
	if path == "" {
		home, _ := os.UserHomeDir()
		var err error
		if path, err = Find(home, r.Dirs); err != nil {
			return nil, err
		}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
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
