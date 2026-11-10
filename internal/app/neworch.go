package app

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/contursofran/contr/internal/app/projectsview"
	"github.com/contursofran/contr/internal/app/view"
	"github.com/contursofran/contr/internal/git"
)

// Labels of the dialog that asks to make a git repository (D233).
const (
	LabelMakeRepo       = "Make git repository"
	LabelMakeRepoCancel = "Cancel"
)

// gitInitTimeout limits `git init` and the first commit together (D221).
const gitInitTimeout = 30 * time.Second

// NewOrchestrator starts an orchestrator in a folder that the owner picks
// (the `+ new` button next to Spaces, D57). Its methods run on the UI thread;
// the picker, the git commands and the spawn run off it. A folder outside git
// asks first to make it a git repository (D233).
type NewOrchestrator struct {
	// Pick asks the owner for a folder. It blocks until the owner answers and
	// returns ErrPickCancelled for no choice. Nil makes Start do nothing.
	Pick func() (string, error)
	// Spawn starts the orchestrator profile in a folder and returns the
	// address of its terminal.
	Spawn func(dir string) (string, error)
	// IsRepo tells if dir is in a git repository. It reads files. Nil uses
	// git.Discover.
	IsRepo func(dir string) bool
	// Git runs `git init` and the first commit. Nil uses git.ExecRunner.
	Git git.Runner
	// Spawned gets the address of a new orchestrator on the UI thread. Nil
	// does nothing.
	Spawned func(addr string)
	// Env runs the picker and the spawn off the UI thread and shows errors.
	Env view.Env

	busy bool   // a pick, the question or a spawn is running
	ask  string // the folder outside git that the dialog asks about, or ""
}

// Start picks a folder, then spawns an orchestrator there. A second click
// while the first runs does nothing.
func (n *NewOrchestrator) Start() {
	if n.Pick == nil || n.Spawn == nil || n.busy {
		return
	}
	n.busy = true
	n.Env.Do(func() func() {
		dir, err := n.Pick()
		if err != nil {
			return func() { n.done("", err) }
		}
		return n.spawnIfRepo(dir)
	})
}

// StartIn spawns an orchestrator in dir with no picker: the `+ new` of an
// empty project group (D116).
func (n *NewOrchestrator) StartIn(dir string) {
	if n.Spawn == nil || n.busy {
		return
	}
	n.busy = true
	n.Env.Do(func() func() { return n.spawnIfRepo(dir) })
}

// spawnIfRepo spawns in dir when dir is in git, else it gives the step that
// asks. It runs off the UI thread.
func (n *NewOrchestrator) spawnIfRepo(dir string) func() {
	if !n.isRepo(dir) {
		return func() { n.ask = dir; n.Env.Redraw() }
	}
	addr, err := n.Spawn(dir)
	return func() { n.done(addr, err) }
}

// makeRepo runs `git init` and an empty first commit in dir, then spawns.
func (n *NewOrchestrator) makeRepo(dir string) {
	n.Env.Do(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), gitInitTimeout)
		defer cancel()
		var addr string
		err := git.Init(ctx, n.runner(), dir)
		if err == nil {
			addr, err = n.Spawn(dir)
		}
		return func() { n.done(addr, err) }
	})
}

// done ends a start on the UI thread: it shows an error, or tells Spawned.
func (n *NewOrchestrator) done(addr string, err error) {
	n.busy = false
	switch {
	case errors.Is(err, projectsview.ErrPickCancelled):
	case err != nil:
		n.Env.Show("New orchestrator: " + err.Error())
	case n.Spawned != nil && addr != "":
		n.Spawned(addr)
	}
}

// View builds the dialog while it asks to make a git repository.
func (n *NewOrchestrator) View(c *ui.Context) {
	if n.ask == "" {
		return
	}
	open := true
	choice := ui.AlertDialog(c, &open, n.ask+" is not a git repository.",
		"Workers need one. Make it a git repository?", LabelMakeRepoCancel, LabelMakeRepo)
	switch {
	case choice == 1:
		dir := n.ask
		n.ask = ""
		n.makeRepo(dir)
	case choice == 0 || choice == ui.AlertDismissed || !open:
		n.ask = ""
		n.busy = false
		n.Env.Redraw()
	}
}

func (n *NewOrchestrator) isRepo(dir string) bool {
	if n.IsRepo != nil {
		return n.IsRepo(dir)
	}
	_, ok := git.Discover(dir)
	return ok
}

func (n *NewOrchestrator) runner() git.Runner {
	if n.Git != nil {
		return n.Git
	}
	return git.ExecRunner{}
}
