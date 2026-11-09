package app

import (
	"context"

	"github.com/contursofran/contr/internal/app/ghostty"
	"github.com/contursofran/contr/internal/app/theme"
	"github.com/contursofran/contr/internal/git"
	"github.com/contursofran/contr/internal/prefs"
	"github.com/contursofran/contr/internal/projects"
	"github.com/contursofran/contr/internal/updated"
	"github.com/contursofran/contr/internal/widths"
)

// Store loads and saves one value. The frame calls Load and Save on a goroutine, except
// where a field of [Deps] says otherwise. A nil Load reads nothing and a nil Save saves
// nothing.
type Store[T any] struct {
	Load func() (T, error)
	Save func(T) error
}

// StoreAt returns the store of the value in the file that path finds. path runs on each
// Load and Save; its error is the error of that call.
func StoreAt[T any](path func() (string, error), load func(path string) (T, error),
	save func(path string, v T) error) Store[T] {
	return Store[T]{
		Load: func() (T, error) {
			p, err := path()
			if err != nil {
				var zero T
				return zero, err
			}
			return load(p)
		},
		Save: func(v T) error {
			p, err := path()
			if err != nil {
				return err
			}
			return save(p, v)
		},
	}
}

// Deps is what the frame gets from outside: the stores, the pickers, the agents and the
// update service. cmd/contr fills it; tests fill it with fakes. Each field is optional: the
// zero Deps gives a frame with no stores, no agents and no terminal.
type Deps struct {
	// Projects is the projects store (D109, D110). Nil Load: no store, the frame reads no
	// file. Nil Save: nothing is saved.
	Projects Store[*projects.Store]
	// Widths keeps the widths of the sidebar and the panel (D111). Nil Load: the defaults.
	// Save runs after a drag.
	Widths Store[widths.Widths]
	// Prefs keeps the settings (D119). [Frame.StartTheme] calls Load before the window
	// shows, so it must be quick. Nil Load: the defaults.
	Prefs Store[prefs.Prefs]
	// Updated keeps the note of the last install (D209).
	Updated Store[updated.Note]

	// StartAgents follows the agents. [Frame.Attach] calls it once, with a notify function
	// that is safe from any goroutine, and uses the source and the stop function it returns.
	// Nil means no agents.
	StartAgents func(notify func()) (src AgentSource, stop func())
	// AgentsNotice is a note about agents.json that LoadConfig made at start (the old
	// format, D206). [Frame.Attach] shows it once in the toasts.
	AgentsNotice string
	// AgentsConfig is the path of agents.json that the Agents tab of Settings edits (D186).
	AgentsConfig string
	// ModelOf names the model that an agent profile starts, for line 2 of the agent rows.
	// Nil shows no model.
	ModelOf func(profile string) string
	// EffortOf names the effort that an agent profile starts, for line 2 of the agent rows
	// (D189). Nil: none.
	EffortOf func(profile string) string
	// WatchHead watches a repo's HEAD file for the sidebar's branch (see
	// SpacesList.WatchHead). Nil: the branch is read when the terminals change.
	WatchHead func(ctx context.Context, path string, changed func()) error
	// TerminalSource gives the source of the hc terminal with an address. The terminal view
	// attaches to the selected agent's terminal through it. Nil shows no terminal.
	TerminalSource func(address string) ghostty.Source
	// OpenTerminal serves "+" and Cmd-T (D82): it starts a shell in an hc terminal for the
	// open project, in dir, and returns its address. It blocks, so the frame runs it off the
	// UI thread. Nil makes both do nothing.
	OpenTerminal func(project, dir string) (string, error)
	// PickFolder and SpawnOrchestrator serve `+ new` (D57): the first asks the owner for a
	// folder (ErrPickCancelled for none), the second starts the orchestrator profile there.
	// Both block, so the frame runs them off the UI thread. Nil makes `+ new` do nothing.
	PickFolder        func() (string, error)
	SpawnOrchestrator func(dir string) error

	// OpenFolder asks the owner for a folder to add to the projects (D116). It blocks, so
	// the modal calls it off the UI thread.
	OpenFolder func() (string, error)
	// CloneParent, PickCloneParent and CloneRun serve the clone part of the projects modal
	// (D117); see ProjectsModal. An empty CloneParent hides it.
	CloneParent     string
	PickCloneParent func(def string) (string, error)
	CloneRun        git.CloneRunner
	// ProjectKey returns the project key of a folder (D116); nil uses the folder.
	ProjectKey func(path string) string

	// Updates is the update service of the Settings modal (D115). Nil shows "updates are off".
	Updates Updates
	// Relaunch restarts contr after an update installs.
	Relaunch func()
	// ThemePlatform is MyGo's theme (mygo.Theme). Nil leaves the palette as it is.
	ThemePlatform theme.Platform
}
