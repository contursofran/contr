package app

// Sends the title bar's commands to herdr. Each command is one request on its own connection,
// made on a goroutine so the UI thread never waits on the socket. A failed command shows in the
// status message; the next herdr tree shows what herdr did. Nothing is shown optimistically.
// Ported from the Rust contr module tab_commands.

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/contursofran/contr/internal/herdr"
)

// commandTimeout is how long one command may take before it is given up.
const commandTimeout = 2 * time.Second

// TabKind tells what a [TabCommand] asks herdr to do.
type TabKind uint8

// The tab commands.
const (
	// TabFocus focuses the tab with the ID.
	TabFocus TabKind = iota
	// TabCreate makes a focused tab in the workspace with the ID.
	TabCreate
	// TabSplitRight splits the pane with the ID to the right.
	TabSplitRight
)

// TabCommand is what a click asks herdr to do. The ID is herdr's: a tab, a workspace or a pane.
type TabCommand struct {
	Kind TabKind
	ID   string
}

func (c TabCommand) String() string {
	switch c.Kind {
	case TabFocus:
		return "tab.focus " + c.ID
	case TabCreate:
		return "tab.create " + c.ID
	}
	return "pane.split " + c.ID
}

// run sends the one request that c stands for: tab.focus, tab.create or pane.split to the right.
func (c TabCommand) run(path string) error {
	switch c.Kind {
	case TabFocus:
		return herdr.FocusTab(path, c.ID, commandTimeout)
	case TabCreate:
		return herdr.CreateTab(path, c.ID, commandTimeout)
	}
	return herdr.SplitRight(path, c.ID, commandTimeout)
}

// commander sends [TabCommand]s. report shows a failure to the user; it runs on the sending
// goroutine, so it should only set the status message.
type commander struct {
	socket func() (string, bool)
	run    func(path string, cmd TabCommand) error
	report func(text string)
	// done, when set, runs after each command ends. Tests wait on it.
	done func()
}

func newCommander(report func(string)) *commander {
	return &commander{socket: herdr.APISocketPath, run: func(path string, cmd TabCommand) error { return cmd.run(path) }, report: report}
}

// Send sends cmd to the local herdr and returns at once.
func (c *commander) Send(cmd TabCommand) {
	go func() {
		c.do(cmd)
		if c.done != nil {
			c.done()
		}
	}()
}

// do sends cmd and reports a failure.
func (c *commander) do(cmd TabCommand) {
	path, ok := socketOrWarn(c.socket, cmd)
	if !ok {
		c.report("herdr is not running")
		return
	}
	err := c.run(path, cmd)
	if err == nil {
		return
	}
	slog.Warn("herdr command failed", "cmd", cmd.String(), "err", err)
	c.report(failureLine(err))
}

// failureLine is the status line for a failed herdr command.
func failureLine(err error) string {
	var wire *herdr.WireError
	if errors.As(err, &wire) && wire.IsNotRunning() {
		return "herdr is not running"
	}
	return fmt.Sprintf("herdr: %v", err)
}

// socketOrWarn returns the socket path, or false with a warning that cmd is not sent.
func socketOrWarn(socket func() (string, bool), cmd TabCommand) (string, bool) {
	path, ok := socket()
	if !ok {
		slog.Warn("no herdr socket path; command not sent", "cmd", cmd.String())
	}
	return path, ok
}
