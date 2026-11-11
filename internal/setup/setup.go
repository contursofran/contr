// Package setup checks what contr needs on this Mac (D278): hc, the hc core, the contr plugin of
// Claude Code, the Claude meter, gh, and the optional codex and contr command line. The checks
// only read: they run commands with a time limit and read contr's own state folder. They do not
// read files in Claude Code's or Codex's folders; the plugin check asks `claude plugin list`.
package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/contursofran/contr/internal/usage"
)

// State is the result of one check.
type State int

const (
	// Ready: nothing to do.
	Ready State = iota
	// Missing: a program or file is not there.
	Missing
	// Action: it is there, but the owner must do a step.
	Action
	// Optional: an optional part is not there.
	Optional
)

// IDs of the checks, in the order of the checklist.
const (
	IDHC     = "hc"
	IDCore   = "core"
	IDPlugin = "plugin"
	IDMeter  = "meter"
	IDGH     = "gh"
	IDCodex  = "codex"
	IDContr  = "contr"
)

// Commands the owner can copy. contr does not run them (D278) and does not edit Claude Code's
// settings: the meter line is for the owner to put in settings.json.
const (
	CopyHC       = "Install herdr-core and put hc on PATH or in ~/.local/bin"
	CopyPlugin   = "claude plugin install contr@contr"
	CopyEnable   = "claude plugin enable contr@contr"
	CopyMeter    = `"statusLine": {"type": "command", "command": "contr statusline"}`
	CopyGHLogin  = "gh auth login"
	CopyGHApp    = "brew install gh"
	CopyCodex    = "brew install codex"
	CopyContrCLI = "scripts/install.sh"
)

// MeterFresh is the age under which the meter's data counts as live.
const MeterFresh = 24 * time.Hour

// Timeout is the time limit of each command.
const Timeout = 5 * time.Second

// Result is the result of one check.
type Result struct {
	ID       string
	Name     string
	Required bool
	State    State
	// Detail is line 2 of the row: a version, or what is wrong.
	Detail string
	// Copy is the command or line to copy; empty when there is none.
	Copy string
}

// OK tells whether the check needs nothing from the owner.
func (r Result) OK() bool { return r.State == Ready || (!r.Required && r.State == Optional) }

// Runner runs a program and returns its standard output.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Env is what the checks read. Tests give fakes.
type Env struct {
	// Run runs a program; nil runs it with os/exec.
	Run Runner
	// Find returns the path of a program; nil searches PATH and the usual places under Home.
	Find func(name string) (string, error)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Home is the home folder, for Find.
	Home string
	// StateDir is contr's state folder, where `contr statusline` writes the meter's data.
	StateDir string
	// Timeout is the limit of each command; zero means Timeout.
	Timeout time.Duration
}

// Required is the number of required checks that are not ready.
func Required(rs []Result) int {
	n := 0
	for _, r := range rs {
		if r.Required && !r.OK() {
			n++
		}
	}
	return n
}

// Core is the core check from the frame's live connection: v is the core's version, ok is false
// while contr does not follow a core.
func Core(v string, ok bool) Result {
	r := Result{ID: IDCore, Name: "hc core", Required: true}
	if !ok {
		r.State, r.Detail = Missing, "not connected"
		return r
	}
	r.Detail = "running · hc " + v
	return r
}

// Run runs the checks other than Core, in order. It blocks: call it off the UI thread.
func Run(ctx context.Context, e Env) []Result {
	return []Result{e.hc(ctx), e.plugin(ctx), e.meter(), e.gh(ctx), e.codex(), e.contr()}
}

func (e Env) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	d := e.Timeout
	if d <= 0 {
		d = Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	run := e.Run
	if run == nil {
		run = execRun
	}
	out, err := run(ctx, name, args...)
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (e Env) find(name string) (string, error) {
	if e.Find != nil {
		return e.Find(name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if e.Home != "" {
		dirs = append([]string{filepath.Join(e.Home, ".local", "bin")}, dirs...)
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errors.New(name + " not found")
}

// failed is the detail of a command that did not end well.
func failed(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "failed: " + err.Error()
}

func (e Env) hc(ctx context.Context) Result {
	r := Result{ID: IDHC, Name: "hc", Required: true}
	p, err := e.find("hc")
	if err != nil {
		r.State, r.Detail, r.Copy = Missing, "not installed", CopyHC
		return r
	}
	out, err := e.run(ctx, p, "--version")
	if err != nil {
		r.State, r.Detail = Action, failed(err)
		return r
	}
	// The output is one line: "hc 0.1.18".
	f := strings.Fields(string(out))
	if len(f) != 2 || f[0] != "hc" {
		r.State, r.Detail = Action, "unexpected version output"
		return r
	}
	r.Detail = "hc " + f[1]
	return r
}

// plugin is one item of `claude plugin list --json`.
type plugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

func (e Env) plugin(ctx context.Context) Result {
	r := Result{ID: IDPlugin, Name: "contr plugin", Required: true}
	p, err := e.find("claude")
	if err != nil {
		r.State, r.Detail = Missing, "Claude Code not installed"
		return r
	}
	out, err := e.run(ctx, p, "plugin", "list", "--json")
	if err != nil {
		r.State, r.Detail = Action, failed(err)
		return r
	}
	var list []plugin
	if err := json.Unmarshal(out, &list); err != nil {
		r.State, r.Detail = Action, "unexpected plugin list output"
		return r
	}
	for _, pl := range list {
		if pl.ID != "contr" && !strings.HasPrefix(pl.ID, "contr@") {
			continue
		}
		if !pl.Enabled {
			r.State, r.Detail, r.Copy = Action, "disabled", CopyEnable
			return r
		}
		r.Detail = "contr " + pl.Version
		return r
	}
	r.State, r.Detail, r.Copy = Missing, "not installed", CopyPlugin
	return r
}

func (e Env) meter() Result {
	r := Result{ID: IDMeter, Name: "Claude meter", Required: true}
	u := usage.ReadClaude(e.StateDir)
	if u == nil {
		r.State, r.Detail, r.Copy = Action, "no data from the status line", CopyMeter
		return r
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	age := now().Sub(time.Unix(int64(u.ReadAt), 0))
	if age > MeterFresh {
		r.State, r.Detail, r.Copy = Action, "last data "+ago(age), CopyMeter
		return r
	}
	r.Detail = "last data " + ago(age)
	return r
}

// ago is an age in words: "now", "5 min ago", "3 h ago", "2 days ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " days ago"
}

func (e Env) gh(ctx context.Context) Result {
	r := Result{ID: IDGH, Name: "gh", Required: true}
	p, err := e.find("gh")
	if err != nil {
		r.State, r.Detail, r.Copy = Missing, "not installed", CopyGHApp
		return r
	}
	if _, err := e.run(ctx, p, "auth", "status"); err != nil {
		r.State, r.Copy = Action, CopyGHLogin
		r.Detail = "not signed in"
		if errors.Is(err, context.DeadlineExceeded) {
			r.Detail = "timed out"
		}
		return r
	}
	r.Detail = "signed in"
	return r
}

func (e Env) codex() Result {
	r := Result{ID: IDCodex, Name: "codex"}
	if _, err := e.find("codex"); err != nil {
		r.State, r.Detail, r.Copy = Optional, "not installed", CopyCodex
		return r
	}
	r.Detail = "installed"
	return r
}

func (e Env) contr() Result {
	r := Result{ID: IDContr, Name: "contr command"}
	if _, err := e.find("contr"); err != nil {
		r.State, r.Detail, r.Copy = Optional, "not linked: the meter needs it", CopyContrCLI
		return r
	}
	r.Detail = "linked"
	return r
}
